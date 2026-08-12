<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Dialog } from '@ark-ui/vue/dialog'
import { Toast, Toaster, createToaster } from '@ark-ui/vue/toast'
import {
  CloseOutlined,
  DeleteOutlined,
  EditOutlined,
  LeftOutlined,
  LockOutlined,
  RobotOutlined,
} from '@ant-design/icons-vue'
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
import { useRetainedDialog } from '../../composables/useRetainedDialog'

const agent = useAgentStore()
const initError = ref('')
const deleteDialog = useRetainedDialog<{ id: string; title: string }>()
const { open: deleteDialogOpen, payload: deleteTarget } = deleteDialog
const deletingSession = ref(false)
const renameTargetId = ref('')
const renameTitle = ref('')
const renamingSession = ref(false)
const subtaskStack = ref<Array<{ id: string; title: string }>>([])

const activeSubtask = computed(() => subtaskStack.value[subtaskStack.value.length - 1])
const activeSessionId = computed(() => activeSubtask.value?.id || agent.currentSessionId)
const activeConversation = computed(() =>
  activeSubtask.value ? agent.conversations[activeSubtask.value.id] : undefined,
)
const visibleMessages = computed(() =>
  activeSubtask.value ? activeConversation.value?.messages || [] : agent.messages,
)
const visibleParts = computed(() => (activeSubtask.value ? activeConversation.value?.parts || {} : agent.parts))
const visibleLoading = computed(() =>
  activeSubtask.value ? !!activeConversation.value?.loading && !activeConversation.value.loaded : agent.messagesLoading,
)
const currentStatus = computed(() => agent.statusForSession(activeSessionId.value))
const visibleSending = computed(() => {
  if (!activeSubtask.value) return agent.sending
  return currentStatus.value?.type === 'busy' || currentStatus.value?.type === 'retry'
})

const blocked = computed(() => {
  const id = activeSessionId.value
  return id ? agent.hasPendingRequest(id) : false
})
const messageScrollKey = computed(() => `${activeSessionId.value || ''}:${visibleLoading.value ? 'loading' : 'ready'}`)
const conversationItems = computed<XConversationItem[]>(() =>
  agent.sessions.map((session) => ({
    key: session.id,
    label: sessionTitle(session),
    status: agent.sessionIndicator(session.id),
    statusLabel: sessionIndicatorLabel(agent.sessionIndicator(session.id)),
  })),
)
const timelineHasError = computed(() =>
  visibleMessages.value.some((message) => message.error && !isAbortedAgentError(message.error)),
)
const runtimeNotice = computed(() => {
  if (agent.sessionsError) {
    return { title: '无法加载对话', message: agent.sessionsError, variant: 'error' as const, retryable: true }
  }
  if (initError.value) {
    return { title: '操作未完成', message: initError.value, variant: 'error' as const, retryable: true }
  }
  if (activeSubtask.value && activeConversation.value?.error) {
    return {
      title: '子任务未能更新',
      message: activeConversation.value.error,
      variant: 'warning' as const,
      retryable: true,
    }
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
  const sessionError = agent.errorForSession(activeSessionId.value)
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

watch(
  () => agent.currentSessionId,
  (id, previousID) => {
    if (id !== previousID) subtaskStack.value = []
  },
)

async function send(text: string, files: AgentFilePartInput[]) {
  initError.value = ''
  if (!agent.currentSessionId) await agent.createSession()
  await agent.sendMessage(text, files)
}

async function newSession() {
  try {
    subtaskStack.value = []
    await agent.createSession()
  } catch (cause) {
    initError.value = displayError(cause, '暂时无法创建对话')
  }
}

async function selectSession(id: string) {
  try {
    subtaskStack.value = []
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
    if (activeSessionId.value) await agent.loadConversation(activeSessionId.value)
  } catch (cause) {
    initError.value = displayError(cause, '重新连接失败，请稍后再试')
  }
}

async function openSubtask(sessionID: string, title: string) {
  if (!sessionID || activeSubtask.value?.id === sessionID) return
  subtaskStack.value = [...subtaskStack.value, { id: sessionID, title: title || '子任务' }]
  initError.value = ''
  try {
    await agent.ensureSessionLineage(sessionID)
    await agent.loadConversation(sessionID)
  } catch (cause) {
    initError.value = displayError(cause, '暂时无法加载子任务')
  }
}

function backFromSubtask() {
  subtaskStack.value = subtaskStack.value.slice(0, -1)
  initError.value = ''
}

function handleConversationAction(actionKey: string, item: XConversationItem) {
  // Let the action menu finish closing and restore focus before replacing its
  // label with an input. Otherwise that focus restoration commits on blur.
  if (actionKey === 'rename') window.setTimeout(() => void startRenameSession(item.key), 120)
  if (actionKey === 'delete') requestDeleteSession(item.key)
}

function requestDeleteSession(id: string) {
  deleteDialog.show({ id, title: sessionTitle(agent.sessions.find((session) => session.id === id)) })
}

function updateDeleteDialogOpen(open: boolean) {
  deleteDialog.updateOpen(open, !deletingSession.value)
}

function completeDeleteDialogClose() {
  deleteDialog.clearAfterExit()
}

async function confirmDeleteSession() {
  if (!deleteTarget.value || deletingSession.value) return
  const id = deleteTarget.value.id
  deletingSession.value = true
  try {
    await agent.deleteSession(id)
    deleteDialog.close()
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
        <header v-if="activeSubtask" class="agent-chat-subtask-header">
          <XButton class="agent-chat-subtask-back" variant="ghost" size="small" @click="backFromSubtask">
            <LeftOutlined aria-hidden="true" />
            {{ subtaskStack.length > 1 ? '返回上一级' : '返回主对话' }}
          </XButton>
          <div class="agent-chat-subtask-heading">
            <span>子任务</span>
            <strong :title="activeSubtask.title">{{ activeSubtask.title }}</strong>
          </div>
        </header>

        <AgentMessageList
          :messages="visibleMessages"
          :parts="visibleParts"
          :sending="visibleSending"
          :stopping="activeSubtask ? false : agent.stopping"
          :waiting="blocked"
          :loading="visibleLoading"
          :status="currentStatus"
          :sessions="agent.allSessions"
          :session-statuses="agent.sessionStatuses"
          :session-errors="agent.sessionErrors"
          :scroll-reset-key="messageScrollKey"
          :empty-title="activeSubtask ? '暂无子任务记录' : '有什么可以帮你？'"
          :empty-description="activeSubtask ? '该子任务尚未产生可展示内容' : '发送消息或添加图片、文件来开始对话'"
          @open-subtask="openSubtask"
        />

        <footer class="agent-chat-input">
          <div class="agent-chat-composer-wrap">
            <AgentRequestPrompts :session-id="activeSessionId || ''" @error="showToast($event, 'error')" />
            <AgentComposer
              v-if="!blocked && !activeSubtask"
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
            <div v-if="activeSubtask && !blocked" class="agent-chat-subtask-readonly">
              <LockOutlined aria-hidden="true" />
              <span>子任务记录为只读，请返回主对话继续发送消息</span>
            </div>
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
      :open="deleteDialogOpen"
      lazy-mount
      unmount-on-exit
      :close-on-escape="!deletingSession"
      :close-on-interact-outside="!deletingSession"
      @exit-complete="completeDeleteDialogClose"
      @update:open="updateDeleteDialogOpen"
    >
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content class="dialog-panel" style="max-width: 360px">
            <div class="dialog-header">
              <Dialog.Title>删除会话</Dialog.Title>
              <Dialog.CloseTrigger class="dialog-close" :disabled="deletingSession"
                ><CloseOutlined
              /></Dialog.CloseTrigger>
            </div>
            <div class="dialog-body">
              <p class="agent-chat-delete-text">确定要删除「{{ deleteTarget?.title }}」？此操作不可撤销。</p>
              <div class="btn-group" style="justify-content: flex-end">
                <button class="admin-btn" :disabled="deletingSession" @click="deleteDialog.close">
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

.agent-chat-subtask-header {
  min-height: 52px;
  display: flex;
  align-items: center;
  gap: 12px;
  box-sizing: border-box;
  padding: 8px 24px;
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-primary);
}

.agent-chat-subtask-back {
  flex: none;
}

.agent-chat-subtask-heading {
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 8px;

  span {
    flex: none;
    color: var(--text-muted);
    font-size: var(--marvo-type-11);
  }

  strong {
    min-width: 0;
    overflow: hidden;
    color: var(--text-primary);
    font-size: var(--marvo-type-13);
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
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

.agent-chat-subtask-readonly {
  min-height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
  text-align: center;
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
  .agent-chat-subtask-header {
    gap: 6px;
    padding-inline: 12px;
  }
  .agent-chat-subtask-heading {
    gap: 6px;
  }
}
</style>
