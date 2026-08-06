<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { Dialog } from '@ark-ui/vue/dialog'
import { Toast, Toaster, createToaster } from '@ark-ui/vue/toast'
import { CloseOutlined, DeleteOutlined, EditOutlined, RobotOutlined } from '@ant-design/icons-vue'
import { useAgentStore } from '../../stores/agent'
import { formatAgentError, isAbortedAgentError, type AgentFilePartInput } from '../../sdk'
import AgentComposer from '../../components/AgentComposer.vue'
import AgentMessageList from '../../components/AgentMessageList.vue'
import AgentRequestPrompts from '../../components/AgentRequestPrompts.vue'
import {
  XButton,
  XConversations,
  XErrorCard,
  XWelcome,
  type XConversationAction,
  type XConversationItem,
} from '../../components/x'

const agent = useAgentStore()
const initError = ref('')
const deleteTargetId = ref('')
const deletingSession = ref(false)
const renameTargetId = ref('')
const renameTitle = ref('')
const renamingSession = ref(false)

const blocked = computed(() => {
  const id = agent.currentSessionId
  return id ? agent.hasPendingRequest(id) : false
})
const deleteTarget = computed(() => agent.sessions.find((s) => s.id === deleteTargetId.value))
const messageScrollKey = computed(
  () => `${agent.currentSessionId || ''}:${agent.messagesLoading ? 'loading' : 'ready'}`,
)
const currentStatus = computed(() => agent.statusForSession(agent.currentSessionId))
const conversationItems = computed<XConversationItem[]>(() =>
  agent.sessions.map((session) => ({
    key: session.id,
    label: sessionTitle(session),
    status: agent.sessionIndicator(session.id),
    statusLabel: sessionIndicatorLabel(agent.sessionIndicator(session.id)),
  })),
)
const timelineHasError = computed(() =>
  agent.messages.some((message) => message.error && !isAbortedAgentError(message.error)),
)
const runtimeNotice = computed(() => {
  if (agent.sessionsError) {
    return { title: '无法加载对话', message: agent.sessionsError, variant: 'error' as const, retryable: true }
  }
  if (initError.value) {
    return { title: '操作未完成', message: initError.value, variant: 'error' as const, retryable: true }
  }
  if (agent.conversationError) {
    return {
      title: '对话未能更新',
      message: agent.conversationError,
      variant: 'warning' as const,
      retryable: true,
    }
  }
  if (agent.globalError) {
    return {
      title: '智能体服务异常',
      message: agent.globalError.message,
      detail: agent.globalError.detail,
      variant: 'error' as const,
      retryable: true,
    }
  }
  const sessionError = agent.errorForSession(agent.currentSessionId)
  if (sessionError && !timelineHasError.value) {
    return {
      title: '本次执行未完成',
      message: sessionError.message,
      detail: sessionError.detail,
      variant: 'error' as const,
      retryable: false,
    }
  }
  if (agent.connectionState === 'reconnecting') {
    return {
      title: '连接已中断',
      message: '正在重新连接，现有对话仍会保留。',
      variant: 'warning' as const,
      retryable: true,
    }
  }
  if (agent.connectionState === 'disconnected' && agent.sessionsLoaded) {
    return {
      title: '连接已断开',
      message: '重新连接后才能继续接收执行进度。',
      variant: 'warning' as const,
      retryable: true,
    }
  }
  return null
})
const conversationActions: XConversationAction[] = [
  { key: 'rename', label: '重命名', icon: EditOutlined },
  { key: 'delete', label: '删除会话', icon: DeleteOutlined, danger: true },
]

const DEFAULT_SESSION_TITLE = /^New session - \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/

const toaster = createToaster({
  placement: 'bottom',
  duration: 2500,
  max: 3,
  offsets: { top: '16px', right: '16px', bottom: '80px', left: '16px' },
})

function showToast(text: string, type: 'success' | 'error') {
  toaster.create({ title: text, type })
}

function sessionTitle(session?: { title?: string }) {
  if (!session?.title) return '新对话'
  return DEFAULT_SESSION_TITLE.test(session.title) ? '新对话' : session.title
}

function sessionIndicatorLabel(status?: XConversationItem['status']) {
  if (status === 'attention') return '需要你的回应'
  if (status === 'retry') return '正在等待重试'
  if (status === 'running') return '正在执行'
  if (status === 'error') return '执行未完成'
  return undefined
}

function displayError(cause: unknown, fallback: string) {
  const message = formatAgentError(cause)
  return message && message !== '智能体服务暂时不可用' ? message : fallback
}

onMounted(async () => {
  agent.connect()
  try {
    await agent.loadSessions()
  } catch (cause) {
    initError.value = displayError(cause, '智能体服务暂时不可用')
  }
  if (agent.currentSessionId && agent.sessions.some((session) => session.id === agent.currentSessionId)) {
    await agent.selectSession(agent.currentSessionId)
  } else if (agent.sessions.length > 0) {
    await agent.selectSession(agent.sessions[0].id)
  }
})

async function send(text: string, files: AgentFilePartInput[]) {
  initError.value = ''
  if (!agent.currentSessionId) await agent.createSession()
  await agent.sendMessage(text, files)
}

async function newSession() {
  try {
    await agent.createSession()
  } catch (cause) {
    initError.value = displayError(cause, '暂时无法创建对话')
  }
}

async function selectSession(id: string) {
  try {
    if (renameTargetId.value && renameTargetId.value !== id) await confirmRenameSession()
    await agent.selectSession(id)
  } catch (cause) {
    initError.value = displayError(cause, '暂时无法加载对话')
  }
}

async function recoverAgent() {
  initError.value = ''
  try {
    await agent.reconnect()
    if (agent.currentSessionId) await agent.loadConversation(agent.currentSessionId)
  } catch (cause) {
    initError.value = displayError(cause, '重新连接失败，请稍后再试')
  }
}

function handleConversationAction(actionKey: string, item: XConversationItem) {
  // Let the action menu finish closing and restore focus before replacing its
  // label with an input. Otherwise that focus restoration commits on blur.
  if (actionKey === 'rename') window.setTimeout(() => void startRenameSession(item.key), 120)
  if (actionKey === 'delete') requestDeleteSession(item.key)
}

function requestDeleteSession(id: string) {
  deleteTargetId.value = id
}

async function confirmDeleteSession() {
  if (!deleteTargetId.value || deletingSession.value) return
  const id = deleteTargetId.value
  deletingSession.value = true
  try {
    await agent.deleteSession(id)
    deleteTargetId.value = ''
    showToast('已删除', 'success')
  } catch {
    showToast('删除失败', 'error')
  } finally {
    deletingSession.value = false
  }
  if (agent.sessions.length > 0 && !agent.currentSessionId) await agent.selectSession(agent.sessions[0].id)
}

async function startRenameSession(id: string) {
  if (renamingSession.value) return
  renameTargetId.value = id
  renameTitle.value = sessionTitle(agent.sessions.find((session) => session.id === id))
  await nextTick()
  // Ark Menu restores focus to its trigger after closing. Wait for that cycle so
  // it cannot immediately blur (and therefore commit) the inline title input.
  await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
  if (renameTargetId.value !== id) return
  const input = document.querySelector<HTMLInputElement>('.agent-chat-session-title-input')
  input?.focus()
  input?.select()
}

async function confirmRenameSession() {
  if (!renameTargetId.value || renamingSession.value) return
  const id = renameTargetId.value
  const current = agent.sessions.find((session) => session.id === id)
  const nextTitle = renameTitle.value.trim()
  if (!nextTitle || nextTitle === sessionTitle(current)) {
    renameTargetId.value = ''
    return
  }
  renamingSession.value = true
  try {
    await agent.updateSessionTitle(id, nextTitle)
    showToast('已重命名', 'success')
  } catch {
    showToast('重命名失败', 'error')
  } finally {
    renamingSession.value = false
    renameTargetId.value = ''
  }
}

function cancelRenameSession() {
  if (renamingSession.value) return
  renameTargetId.value = ''
  renameTitle.value = ''
}
</script>

<template>
  <div class="agent-chat">
    <aside class="agent-chat-sessions">
      <XConversations
        :items="conversationItems"
        :active-key="agent.currentSessionId"
        :actions="conversationActions"
        :creation-disabled="agent.sessionsLoading"
        :loading="agent.sessionsLoading"
        @create="newSession"
        @active-change="selectSession"
        @action="handleConversationAction"
      >
        <template #label="{ item }">
          <input
            v-if="renameTargetId === item.key"
            v-model="renameTitle"
            class="agent-chat-session-title-input"
            aria-label="会话名称"
            maxlength="80"
            :disabled="renamingSession"
            @click.stop
            @keydown.enter.stop.prevent
            @keyup.enter.stop.prevent="confirmRenameSession"
            @keydown.escape.stop.prevent="cancelRenameSession"
            @blur="confirmRenameSession"
          />
          <span v-else class="agent-chat-session-title">{{ item.label }}</span>
        </template>
      </XConversations>
    </aside>

    <main class="agent-chat-main">
      <XErrorCard
        v-if="runtimeNotice"
        class="agent-chat-runtime-notice"
        :title="runtimeNotice.title"
        :message="runtimeNotice.message"
        :detail="runtimeNotice.detail"
        :variant="runtimeNotice.variant"
      >
        <XButton v-if="runtimeNotice.retryable" size="small" variant="ghost" @click="recoverAgent"> 重新连接 </XButton>
      </XErrorCard>

      <div v-if="!agent.currentSessionId" class="agent-chat-welcome">
        <XWelcome title="智能体" description="选择或创建一个对话开始" :icon="RobotOutlined" variant="filled" />
      </div>

      <template v-else>
        <AgentMessageList
          :messages="agent.messages"
          :parts="agent.parts"
          :sending="agent.sending"
          :stopping="agent.stopping"
          :waiting="blocked"
          :loading="agent.messagesLoading"
          :status="currentStatus"
          :scroll-reset-key="messageScrollKey"
        />

        <footer class="agent-chat-input">
          <div class="agent-chat-composer-wrap">
            <AgentRequestPrompts :session-id="agent.currentSessionId" @error="showToast($event, 'error')" />
            <AgentComposer
              v-if="!blocked"
              :key="agent.currentSessionId"
              :sending="agent.sending"
              :stopping="agent.stopping"
              :blocked="blocked"
              :draft-key="agent.currentSessionId"
              :disabled="agent.conversationLoading"
              :submit-message="send"
              :stop-message="() => agent.abortSession()"
              @error="initError = $event"
            />
          </div>
        </footer>
      </template>
    </main>

    <Toaster v-slot="toast" :toaster="toaster">
      <Toast.Root>
        <Toast.Title>{{ toast.title }}</Toast.Title>
      </Toast.Root>
    </Toaster>

    <Dialog.Root
      :open="!!deleteTargetId"
      lazy-mount
      unmount-on-exit
      @update:open="deleteTargetId = $event ? deleteTargetId : ''"
    >
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content class="dialog-panel" style="max-width: 360px">
            <div class="dialog-header">
              <Dialog.Title>删除会话</Dialog.Title>
              <Dialog.CloseTrigger class="dialog-close"><CloseOutlined /></Dialog.CloseTrigger>
            </div>
            <div class="dialog-body">
              <p class="agent-chat-delete-text">确定要删除「{{ sessionTitle(deleteTarget) }}」？此操作不可撤销。</p>
              <div class="btn-group" style="justify-content: flex-end">
                <button class="admin-btn" :disabled="deletingSession" @click="deleteTargetId = ''">
                  <CloseOutlined aria-hidden="true" />取消
                </button>
                <button class="admin-btn admin-btn-danger" :disabled="deletingSession" @click="confirmDeleteSession">
                  <DeleteOutlined aria-hidden="true" />{{ deletingSession ? '删除中...' : '删除' }}
                </button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>
  </div>
</template>

<style lang="scss" scoped>
.agent-chat {
  display: flex;
  height: 100%;
  overflow: hidden;
}

.agent-chat-sessions {
  width: 252px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border-right: 1px solid var(--border-primary);
  background: var(--bg-secondary);

  :deep(.x-conversations) {
    flex: 1;
  }
}

.agent-chat-session-title {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-chat-session-title-input {
  width: 100%;
  min-width: 0;
  height: 28px;
  box-sizing: border-box;
  padding: 0 7px;
  border: 1px solid var(--text-accent);
  border-radius: 6px;
  outline: none;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-13);

  &:disabled {
    opacity: 0.65;
  }
}

.agent-chat-delete-text {
  margin-bottom: 16px;
  color: var(--text-secondary);
  font-size: var(--marvo-type-14);
  line-height: 1.6;
}
.agent-chat-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
}

.agent-chat-runtime-notice {
  margin: 10px 24px 0;
  flex-direction: row;
  align-items: center;

  :deep(.x-button) {
    margin-left: auto;
    flex: none;
  }
}

.agent-chat-welcome {
  flex: 1;
  width: min(620px, calc(100% - 48px));
  display: flex;
  align-items: center;
  margin: auto;
}

.agent-chat-input {
  flex-shrink: 0;
  padding: 8px 24px 12px;
  background: linear-gradient(to bottom, transparent, var(--bg-primary) 18px);
}

.agent-chat-composer-wrap {
  width: min(100%, 932px);
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-inline: auto;
}

:deep([data-scope='toast'][data-part='root']) {
  padding: 8px 20px;
  border-radius: 8px;
  font-size: var(--marvo-type-13);
  white-space: nowrap;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  translate: var(--x) var(--y);
  scale: var(--scale);
  opacity: var(--opacity);
  height: var(--height);
  will-change: translate, scale, opacity, height;
  transition:
    translate 0.2s ease,
    scale 0.2s ease,
    opacity 0.2s ease,
    height 0.2s ease;

  &[data-type='success'] {
    background: #f6ffed;
    border: 1px solid #b7eb8f;
    color: #389e0d;
  }
  &[data-type='error'] {
    background: #fff2f0;
    border: 1px solid #ffccc7;
    color: #cf1322;
  }
}

@media (max-width: 768px) {
  .agent-chat {
    flex-direction: column;
  }
  .agent-chat-sessions {
    width: 100%;
    min-width: 0;
    max-height: 210px;
    border-right: none;
    border-bottom: 1px solid var(--border-primary);
  }
  .agent-chat-input {
    padding: 6px 12px max(12px, env(safe-area-inset-bottom));
  }
}
</style>
