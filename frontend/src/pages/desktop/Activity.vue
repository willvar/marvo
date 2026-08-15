<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Dialog } from '@ark-ui/vue/dialog'
import {
  BellOutlined,
  CheckOutlined,
  CloseOutlined,
  DeleteOutlined,
  MessageOutlined,
  ReloadOutlined,
} from '@ant-design/icons-vue'
import { useAppBackHandler, workspaceRoute, type ActivityItem } from '../../sdk'
import { useActivityStore } from '../../stores/activity'
import { useAgentStore } from '../../stores/agent'
import { XButton, XMarkdown, XSender, XWelcome } from '../../components/x'
import { useRetainedDialog } from '../../composables/useRetainedDialog'

const activityStore = useActivityStore()
const agent = useAgentStore()
const router = useRouter()
const drafts = reactive<Record<string, string>>({})
const selections = reactive<Record<string, string[]>>({})
const sendingID = ref('')
const actionError = ref('')
const deleteDialog = useRetainedDialog<ActivityItem>()
const { open: deleteDialogOpen, payload: deleteTarget } = deleteDialog
const deletingID = ref('')
const deleteError = ref('')
const unavailableReplySessions = reactive(new Set<string>())
const cardElements = new Map<string, Element>()
const visibilityTimers = new Map<string, ReturnType<typeof setTimeout>>()
const queuedReadIDs = new Set<string>()
let readFlushTimer: ReturnType<typeof setTimeout> | null = null
let observer: IntersectionObserver | null = null

onMounted(async () => {
  observer = new IntersectionObserver(handleVisibility, { threshold: [0, 0.55, 1] })
  for (const element of cardElements.values()) observer.observe(element)
  try {
    await activityStore.load(true)
  } catch {
    // The store exposes the actionable error state.
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
  for (const timer of visibilityTimers.values()) clearTimeout(timer)
  visibilityTimers.clear()
  if (readFlushTimer) clearTimeout(readFlushTimer)
  readFlushTimer = null
  void flushReads()
})

function setCardElement(id: string, element: unknown) {
  const previous = cardElements.get(id)
  if (previous) observer?.unobserve(previous)
  if (!(element instanceof Element)) {
    cardElements.delete(id)
    clearVisibilityTimer(id)
    return
  }
  cardElements.set(id, element)
  observer?.observe(element)
}

function handleVisibility(entries: IntersectionObserverEntry[]) {
  for (const entry of entries) {
    const id = [...cardElements.entries()].find(([, element]) => element === entry.target)?.[0]
    if (!id) continue
    const item = activityStore.activities.find((candidate) => candidate.id === id)
    if (!item || item.read_at || entry.intersectionRatio < 0.55) {
      clearVisibilityTimer(id)
      continue
    }
    if (visibilityTimers.has(id)) continue
    visibilityTimers.set(
      id,
      setTimeout(() => {
        visibilityTimers.delete(id)
        queueRead(id)
      }, 500),
    )
  }
}

function clearVisibilityTimer(id: string) {
  const timer = visibilityTimers.get(id)
  if (timer) clearTimeout(timer)
  visibilityTimers.delete(id)
}

function queueRead(id: string) {
  queuedReadIDs.add(id)
  if (readFlushTimer) return
  readFlushTimer = setTimeout(() => {
    readFlushTimer = null
    void flushReads()
  }, 120)
}

async function flushReads() {
  if (queuedReadIDs.size === 0) return
  const ids = [...queuedReadIDs]
  queuedReadIDs.clear()
  try {
    await activityStore.markRead(ids)
  } catch {
    // Reading remains best-effort and will be retried when the cards re-enter view.
  }
}

function selectedChoices(item: ActivityItem) {
  return selections[item.id] || []
}

function toggleChoice(item: ActivityItem, choice: string) {
  if (item.responded_at || item.replying || sendingID.value) return
  const current = selectedChoices(item)
  if (!item.multiple) {
    selections[item.id] = current.includes(choice) ? [] : [choice]
    return
  }
  selections[item.id] = current.includes(choice)
    ? current.filter((candidate) => candidate !== choice)
    : [...current, choice]
}

function canReply(item: ActivityItem) {
  return (
    !item.responded_at &&
    !item.replying &&
    sendingID.value === '' &&
    (!!drafts[item.id]?.trim() || selectedChoices(item).length > 0)
  )
}

function replyText(item: ActivityItem) {
  const selected = selectedChoices(item)
  const custom = drafts[item.id]?.trim() || ''
  const answer = selected.length > 1 ? `我选择：${selected.join('、')}` : selected[0] || ''
  return [answer, custom].filter(Boolean).join('\n\n')
}

function replySessionIsMissing(sessionID: string) {
  return agent.sessionsLoaded && !agent.sessionsError && !agent.allSessions.some((session) => session.id === sessionID)
}

async function reply(item: ActivityItem) {
  if (!canReply(item)) return
  actionError.value = ''
  sendingID.value = item.id
  try {
    agent.connect()
    await agent.createSession()
    await agent.sendMessage(replyText(item), [], {
      activity: { id: item.id, choices: selectedChoices(item) },
    })
    await router.push(workspaceRoute('/agent'))
  } catch (cause) {
    actionError.value = cause instanceof Error ? cause.message : '回复活动失败'
  } finally {
    sendingID.value = ''
  }
}

async function continueConversation(item: ActivityItem) {
  if (!item.reply_session_id) return
  const sessionID = item.reply_session_id
  actionError.value = ''
  try {
    agent.connect()
    await agent.loadSessions()
    if (replySessionIsMissing(sessionID)) {
      unavailableReplySessions.add(sessionID)
      return
    }
    await agent.selectSession(sessionID)
    await router.push(workspaceRoute('/agent'))
  } catch {
    await agent.loadSessions().catch(() => undefined)
    if (replySessionIsMissing(sessionID)) {
      unavailableReplySessions.add(sessionID)
      return
    }
    actionError.value = '暂时无法打开对应对话，请稍后重试'
  }
}

async function retry() {
  actionError.value = ''
  try {
    await activityStore.load(true)
  } catch {
    // The store exposes the actionable error state.
  }
}

function requestDelete(item: ActivityItem) {
  deleteError.value = ''
  deleteDialog.show(item)
}

function updateDeleteDialogOpen(open: boolean) {
  deleteDialog.updateOpen(open, !deletingID.value)
}

async function confirmDelete() {
  const target = deleteTarget.value
  if (!target || deletingID.value) return
  deletingID.value = target.id
  deleteError.value = ''
  try {
    await activityStore.deleteActivity(target.id)
    deleteDialog.close()
  } catch (cause) {
    deleteError.value = cause instanceof Error ? cause.message : '删除活动失败'
  } finally {
    deletingID.value = ''
  }
}

function formatTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const number = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}.${number(date.getMonth() + 1)}.${number(date.getDate())} ${number(date.getHours())}:${number(date.getMinutes())}:${number(date.getSeconds())}`
}

useAppBackHandler(() => {
  if (!deleteDialogOpen.value) return false
  if (!deletingID.value) deleteDialog.close()
  return true
}, 70)
</script>

<template>
  <main class="activity-page">
    <div class="activity-feed">
      <header class="activity-heading">
        <div>
          <h1>活动</h1>
          <p>智能体主动发来的进展、结果和待确认事项。</p>
        </div>
      </header>

      <p v-if="actionError" class="activity-error" role="alert">{{ actionError }}</p>

      <div v-if="activityStore.loading && !activityStore.loaded" class="activity-loading" role="status">
        <span class="page-loading-spinner" />
        <span>正在加载活动…</span>
      </div>

      <section v-else-if="activityStore.error && activityStore.activities.length === 0" class="activity-load-error">
        <strong>活动暂时无法加载</strong>
        <p>{{ activityStore.error }}</p>
        <XButton variant="primary" @click="retry"><ReloadOutlined />重试</XButton>
      </section>

      <XWelcome
        v-else-if="activityStore.activities.length === 0"
        class="activity-empty"
        :icon="BellOutlined"
        title="暂时没有活动"
        description="有新进展或需要你决定时，智能体会在这里联系你。"
        variant="filled"
      />

      <div v-else class="activity-list">
        <article
          v-for="item in activityStore.activities"
          :key="item.id"
          :ref="(element) => setCardElement(item.id, element)"
          :class="['activity-card', { 'is-unread': !item.read_at, 'is-responded': item.responded_at }]"
        >
          <header class="activity-card-heading">
            <span class="activity-card-icon" aria-hidden="true">
              <MessageOutlined v-if="item.kind === 'choice'" />
              <BellOutlined v-else />
            </span>
            <div class="activity-card-title">
              <div class="activity-title-line">
                <h2>{{ item.title }}</h2>
                <span v-if="!item.read_at" class="activity-unread-dot" title="未读" />
              </div>
              <time :datetime="item.created_at">{{ formatTime(item.created_at) }}</time>
            </div>
            <XButton size="small" variant="ghost" :disabled="deletingID === item.id" @click="requestDelete(item)">
              <DeleteOutlined aria-hidden="true" />删除
            </XButton>
          </header>

          <XMarkdown class="activity-content" :text="item.content" open-links-in-new-tab />

          <div v-if="item.kind === 'choice' && !item.responded_at" class="activity-choices">
            <button
              v-for="choice in item.choices"
              :key="choice"
              type="button"
              :class="['activity-choice', { selected: selectedChoices(item).includes(choice) }]"
              :aria-pressed="selectedChoices(item).includes(choice)"
              :disabled="item.replying || !!sendingID"
              @click="toggleChoice(item, choice)"
            >
              <CheckOutlined v-if="selectedChoices(item).includes(choice)" aria-hidden="true" />
              <span>{{ choice }}</span>
            </button>
          </div>

          <div v-if="!item.responded_at" class="activity-reply">
            <XSender
              :model-value="drafts[item.id] || ''"
              compact
              :placeholder="item.kind === 'choice' ? '补充你的想法（可直接只选一项）' : '回复这条活动…'"
              :disabled="item.replying || (!!sendingID && sendingID !== item.id)"
              :submitting="sendingID === item.id || item.replying"
              :submit-disabled="!canReply(item)"
              @update:model-value="drafts[item.id] = $event"
              @submit="reply(item)"
            />
          </div>

          <div v-else-if="item.responded_at" class="activity-response">
            <div>
              <span>你的回复</span>
              <strong v-if="item.response_choices?.length && !item.response_text">
                {{ item.response_choices.join('、') }}
              </strong>
              <p v-if="item.response_text">{{ item.response_text }}</p>
            </div>
            <XButton
              v-if="item.reply_session_id"
              variant="secondary"
              :disabled="unavailableReplySessions.has(item.reply_session_id)"
              @click="continueConversation(item)"
            >
              {{ unavailableReplySessions.has(item.reply_session_id) ? '对话已删除' : '打开对话' }}
            </XButton>
            <span v-else class="activity-session-deleted">对话已删除</span>
          </div>
        </article>
      </div>

      <div v-if="activityStore.nextCursor" class="activity-load-more">
        <XButton :disabled="activityStore.loadingMore" @click="activityStore.loadMore()">
          {{ activityStore.loadingMore ? '正在加载…' : '加载更多' }}
        </XButton>
      </div>
    </div>

    <Dialog.Root
      :open="deleteDialogOpen"
      lazy-mount
      unmount-on-exit
      :close-on-escape="!deletingID"
      :close-on-interact-outside="!deletingID"
      @exit-complete="deleteDialog.clearAfterExit"
      @update:open="updateDeleteDialogOpen"
    >
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content class="dialog-panel" style="max-width: 400px">
            <div class="dialog-header">
              <Dialog.Title>删除活动</Dialog.Title>
              <Dialog.CloseTrigger class="dialog-close" :disabled="!!deletingID">
                <CloseOutlined aria-hidden="true" />
              </Dialog.CloseTrigger>
            </div>
            <div class="dialog-body">
              <p class="activity-delete-confirm-copy">
                「{{ deleteTarget?.title }}」将从活动列表中永久删除。如有相关智能体对话，该对话不会被删除。
              </p>
              <p v-if="deleteError" class="activity-error" role="alert">{{ deleteError }}</p>
              <div class="activity-delete-confirm-actions">
                <XButton :disabled="!!deletingID" @click="deleteDialog.close">
                  <CloseOutlined aria-hidden="true" />取消
                </XButton>
                <XButton variant="danger" :disabled="!!deletingID" @click="confirmDelete">
                  <DeleteOutlined aria-hidden="true" />{{ deletingID ? '删除中…' : '确认删除' }}
                </XButton>
              </div>
            </div>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>
  </main>
</template>

<style lang="scss" scoped>
.activity-page {
  height: 100%;
  overflow-y: auto;
  box-sizing: border-box;
  padding: clamp(18px, 3vw, 40px);
  color: var(--text-primary);
}

.activity-feed {
  width: min(100%, 920px);
  margin: 0 auto;
  padding-bottom: 40px;
}

.activity-heading {
  margin-bottom: 24px;

  h1 {
    margin: 0;
    font-size: var(--marvo-type-24);
    line-height: 1.25;
  }

  p {
    margin: 7px 0 0;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-13);
  }
}

.activity-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.activity-card {
  position: relative;
  padding: 20px;
  border: 1px solid var(--border-primary);
  border-radius: 16px;
  background: var(--bg-card);
  box-shadow: 0 5px 18px color-mix(in srgb, #000 5%, transparent);
}

.activity-card.is-unread {
  border-color: color-mix(in srgb, var(--marvo-accent-color) 36%, var(--border-primary));
}

.activity-card-heading {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 11px;
}

.activity-delete-confirm-copy {
  margin: 0;
  color: var(--text-secondary);
  font-size: var(--marvo-type-13);
  line-height: 1.65;
  overflow-wrap: anywhere;
}

.activity-delete-confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 20px;
}

.activity-card-icon {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border-radius: 10px;
  background: color-mix(in srgb, var(--marvo-accent-color) 10%, transparent);
  color: var(--text-accent);
  font-size: var(--marvo-type-16);
}

.activity-card-title {
  min-width: 0;

  time {
    display: block;
    margin-top: 4px;
    color: var(--text-muted);
    font-size: var(--marvo-type-11);
  }
}

.activity-title-line {
  display: flex;
  align-items: center;
  gap: 8px;

  h2 {
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
    font-size: var(--marvo-type-16);
    line-height: 1.45;
  }
}

.activity-unread-dot {
  width: 7px;
  height: 7px;
  flex: none;
  border-radius: 50%;
  background: var(--marvo-accent-color);
}

.activity-content {
  margin: 16px 0 0 45px;
  color: var(--text-secondary);
}

.activity-choices {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: 18px 0 0 45px;
}

.activity-choice {
  min-height: 36px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 6px 13px;
  border: 1px solid var(--border-primary);
  border-radius: 999px;
  background: var(--bg-primary);
  color: var(--text-secondary);
  cursor: pointer;
  touch-action: manipulation;
  font: inherit;
  font-size: var(--marvo-type-12);

  &:hover:not(:disabled) {
    border-color: color-mix(in srgb, var(--marvo-accent-color) 50%, var(--border-primary));
    color: var(--text-primary);
  }

  &.selected {
    border-color: color-mix(in srgb, var(--marvo-accent-color) 64%, transparent);
    background: color-mix(in srgb, var(--marvo-accent-color) 10%, var(--bg-card));
    color: var(--text-accent);
  }

  &:disabled {
    cursor: default;
    opacity: 0.55;
  }
}

.activity-reply,
.activity-response {
  margin: 16px 0 0 45px;
}

.activity-reply :deep(.x-sender) {
  border-radius: 13px;
  box-shadow: none;
}

.activity-response {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 14px;
  border-radius: 11px;
  background: var(--bg-secondary);

  > div {
    min-width: 0;
  }

  span {
    display: block;
    color: var(--text-muted);
    font-size: var(--marvo-type-11);
  }

  strong,
  p {
    display: block;
    margin: 4px 0 0;
    color: var(--text-secondary);
    font-size: var(--marvo-type-13);
    line-height: 1.55;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  > .activity-session-deleted {
    color: var(--text-tertiary);
    font-size: var(--marvo-type-13);
    white-space: nowrap;
  }
}

.activity-error,
.activity-load-error {
  border: 1px solid color-mix(in srgb, var(--text-danger) 30%, var(--border-primary));
  border-radius: 12px;
  background: color-mix(in srgb, var(--text-danger) 5%, var(--bg-card));
  color: var(--text-danger);
}

.activity-error {
  margin: 0 0 14px;
  padding: 10px 13px;
  font-size: var(--marvo-type-12);
}

.activity-load-error {
  padding: 22px;

  p {
    margin: 6px 0 16px;
    color: var(--text-secondary);
  }
}

.activity-loading {
  min-height: 220px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--text-tertiary);
}

.activity-loading .page-loading-spinner {
  position: static;
}

.activity-empty {
  min-height: clamp(210px, 30vh, 240px);
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 28px 24px;
  text-align: center;
}

.activity-empty :deep(.x-welcome-content) {
  flex: 0 1 auto;
  align-items: center;
  max-width: 420px;
}

.activity-empty :deep(.x-welcome-title-row) {
  justify-content: center;
}

.activity-load-more {
  display: flex;
  justify-content: center;
  padding: 20px 0 0;
}

@media (max-width: 600px) {
  .activity-page {
    padding: 16px 12px 28px;
  }

  .activity-card {
    padding: 16px 13px;
    border-radius: 13px;
  }

  .activity-empty {
    min-height: 210px;
    padding: 24px 18px;
  }

  .activity-card-heading {
    grid-template-columns: auto minmax(0, 1fr) auto;
  }

  .activity-content,
  .activity-choices,
  .activity-reply,
  .activity-response {
    margin-left: 0;
  }

  .activity-choice {
    min-height: 42px;
  }

  .activity-response {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
