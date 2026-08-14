<script setup lang="ts">
import { computed, onMounted, ref, watch, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { Dialog } from '@ark-ui/vue/dialog'
import { useAgentStore } from '../stores/agent'
import { AGENT_DISPLAY_MODE_STORAGE_KEY, useUIPreferencesStore } from '../stores/uiPreferences'
import { formatAgentError, isAbortedAgentError, prepareNoteForAgent, type AgentFilePartInput } from '../sdk'
import AgentAssistantSurface from './AgentAssistantSurface.vue'
import type { XPromptItem } from './x'
import {
  FileTextOutlined,
  TagsOutlined,
  OrderedListOutlined,
  EditOutlined,
  SearchOutlined,
  BulbOutlined,
  PushpinOutlined,
  ReloadOutlined,
  CloseOutlined,
  LayoutOutlined,
  LeftOutlined,
  MessageOutlined,
  RobotOutlined,
} from '@ant-design/icons-vue'

const PINNED_STORAGE_KEY = 'marvo.agentFloating.pinned'
const SIDEBAR_MIN_VIEWPORT_WIDTH = 1100
const FLOATING_PANEL_BOTTOM = 88
const FLOATING_PANEL_TOP_GAP = 16
const MIN_WIDTH = 320
const MIN_HEIGHT = 420
const DEFAULT_WIDTH = 400
const DEFAULT_HEIGHT = 540

const agent = useAgentStore()
const uiPreferences = useUIPreferencesStore()
const route = useRoute()
const open = ref(false)
const error = ref('')
const assistantSurface = ref<{ clear: () => void } | null>(null)
const parkedSurfaceHost = ref<HTMLElement | null>(null)
const sidebarSurfaceHost = ref<HTMLElement | null>(null)
const floatingSurfaceHost = ref<HTMLElement | null>(null)
const pinned = ref(localStorage.getItem(PINNED_STORAGE_KEY) === 'true')
const subtaskStack = ref<Array<{ id: string; title: string }>>([])
const fabRef = ref<HTMLElement>()
const floatingDialogIDs = { content: 'agent-floating-panel' }
const persistentElements = computed(() => [() => fabRef.value || null])
const hidden = computed(() => route.name === 'user-agent')
const viewportWidth = ref(typeof window !== 'undefined' ? window.innerWidth : 0)
const canUseSidebar = computed(() => viewportWidth.value >= SIDEBAR_MIN_VIEWPORT_WIDTH)
const renderSidebar = computed(
  () => !hidden.value && uiPreferences.agentAssistantDisplayMode === 'sidebar' && canUseSidebar.value,
)
const renderFloating = computed(() => !hidden.value && !renderSidebar.value)
const shouldMountAssistantSurface = computed(() => renderSidebar.value || (renderFloating.value && open.value))
const assistantSurfaceTarget = computed(() => {
  if (renderSidebar.value && sidebarSurfaceHost.value) return sidebarSurfaceHost.value
  if (renderFloating.value && open.value && floatingSurfaceHost.value) return floatingSurfaceHost.value
  return parkedSurfaceHost.value
})
const floatingBlocked = computed(() => !!agent.floatingSessionId && agent.hasPendingRequest(agent.floatingSessionId))
const floatingStatus = computed(() => agent.statusForSession(agent.floatingSessionId))
const floatingSending = computed(
  () => agent.floatingSending || floatingStatus.value?.type === 'busy' || floatingStatus.value?.type === 'retry',
)
const activeSubtask = computed(() => subtaskStack.value[subtaskStack.value.length - 1])
const displayedSessionId = computed(() => activeSubtask.value?.id || agent.floatingSessionId)
const displayedConversation = computed(() =>
  activeSubtask.value ? agent.conversations[activeSubtask.value.id] : undefined,
)
const displayedMessages = computed(() =>
  activeSubtask.value ? displayedConversation.value?.messages || [] : agent.floatingMessages,
)
const displayedParts = computed(() =>
  activeSubtask.value ? displayedConversation.value?.parts || {} : agent.floatingParts,
)
const displayedStatus = computed(() => agent.statusForSession(displayedSessionId.value))
const displayedSending = computed(() => {
  if (!activeSubtask.value) return floatingSending.value
  return displayedStatus.value?.type === 'busy' || displayedStatus.value?.type === 'retry'
})
const displayedBlocked = computed(() =>
  displayedSessionId.value ? agent.hasPendingRequest(displayedSessionId.value) : false,
)
const displayedLoading = computed(() =>
  activeSubtask.value ? !!displayedConversation.value?.loading && !displayedConversation.value.loaded : false,
)
const floatingTimelineHasError = computed(() =>
  displayedMessages.value.some((message) => message.error && !isAbortedAgentError(message.error)),
)
const floatingRuntimeNotice = computed(() => {
  if (agent.sessionsError) {
    return { title: '无法加载对话', message: agent.sessionsError, variant: 'error' as const, retryable: true }
  }
  if (error.value) return { title: '操作未完成', message: error.value, variant: 'error' as const, retryable: false }
  if (activeSubtask.value && displayedConversation.value?.error) {
    return {
      title: '子任务未能更新',
      message: displayedConversation.value.error,
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
  const sessionError = agent.errorForSession(displayedSessionId.value)
  if (sessionError && !floatingTimelineHasError.value) {
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
      message: '正在重新连接，当前内容仍会保留。',
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
const floatingIndicator = computed(() => {
  if (agent.floatingSessionId) return agent.sessionIndicator(agent.floatingSessionId)
  if (agent.sessionsError || agent.globalError || agent.connectionState === 'reconnecting') return 'error'
  return undefined
})
const isMobile = ref(typeof window !== 'undefined' && window.innerWidth <= 768)

const panelWidth = ref(DEFAULT_WIDTH)
const panelHeight = ref(DEFAULT_HEIGHT)
const resizing = ref(false)
let stopResize: (() => void) | null = null

function clampWidth(val: number) {
  const max = Math.min(window.innerWidth * 0.72, window.innerWidth - 48)
  return Math.min(Math.max(val, MIN_WIDTH), max)
}

function clampHeight(val: number) {
  const available = Math.max(240, window.innerHeight - FLOATING_PANEL_BOTTOM - FLOATING_PANEL_TOP_GAP)
  const minimum = Math.min(MIN_HEIGHT, available)
  return Math.min(Math.max(val, minimum), available)
}

function onResizeStart(e: PointerEvent) {
  if (!e.isPrimary || (e.pointerType === 'mouse' && e.button !== 0)) return
  const handle = e.currentTarget
  if (!(handle instanceof HTMLElement)) return
  e.preventDefault()
  e.stopPropagation()
  stopResize?.()
  resizing.value = true
  const pointerId = e.pointerId
  const startX = e.clientX
  const startY = e.clientY
  const startW = panelWidth.value
  const startH = panelHeight.value
  const onMove = (ev: PointerEvent) => {
    if (ev.pointerId !== pointerId) return
    ev.preventDefault()
    panelWidth.value = clampWidth(startW - (ev.clientX - startX))
    panelHeight.value = clampHeight(startH - (ev.clientY - startY))
  }
  const finish = () => {
    resizing.value = false
    handle.removeEventListener('pointermove', onMove)
    handle.removeEventListener('pointerup', finish)
    handle.removeEventListener('pointercancel', finish)
    if (handle.hasPointerCapture(pointerId)) handle.releasePointerCapture(pointerId)
    if (stopResize === finish) stopResize = null
  }
  handle.setPointerCapture(pointerId)
  handle.addEventListener('pointermove', onMove)
  handle.addEventListener('pointerup', finish)
  handle.addEventListener('pointercancel', finish)
  stopResize = finish
}

function onResizeWin() {
  viewportWidth.value = window.innerWidth
  panelWidth.value = clampWidth(panelWidth.value)
  panelHeight.value = clampHeight(panelHeight.value)
  isMobile.value = window.innerWidth <= 768
}

function onStorage(event: StorageEvent) {
  if (event.key === AGENT_DISPLAY_MODE_STORAGE_KEY) uiPreferences.syncAgentAssistantDisplayMode()
}

onMounted(() => {
  onResizeWin()
  agent.connect()
  void agent
    .loadSessions()
    .then(() => agent.restoreFloatingSession())
    .catch((cause) => {
      if (!hidden.value) error.value = formatAgentError(cause)
    })
  window.addEventListener('resize', onResizeWin)
  window.addEventListener('storage', onStorage)
})

onBeforeUnmount(() => {
  stopResize?.()
  window.removeEventListener('resize', onResizeWin)
  window.removeEventListener('storage', onStorage)
  agent.disconnect()
})

const currentNoteTitle = computed(() => {
  return typeof route.params.title === 'string' ? route.params.title : ''
})

const promptItems = computed<XPromptItem[]>(() => {
  if (currentNoteTitle.value) {
    return [
      { key: 'summary', icon: FileTextOutlined, label: '总结当前笔记', description: '生成笔记内容摘要' },
      { key: 'tags', icon: TagsOutlined, label: '提取标签', description: '分析并建议标签' },
      { key: 'outline', icon: OrderedListOutlined, label: '生成大纲', description: '为笔记生成结构化大纲' },
      { key: 'polish', icon: EditOutlined, label: '润色当前笔记', description: '改进措辞和表达' },
    ]
  }
  return [
    { key: 'search', icon: SearchOutlined, label: '搜索笔记内容', description: '在笔记库中查找' },
    { key: 'find', icon: BulbOutlined, label: '帮我找相关笔记', description: '根据主题搜索' },
  ]
})

const welcomeTitle = computed(() => (currentNoteTitle.value ? '需要我怎么处理这篇笔记？' : '需要我帮你做什么？'))
const welcomeDescription = computed(() =>
  currentNoteTitle.value ? `已关联「${currentNoteTitle.value}」` : '可以帮你搜索、整理和编辑笔记',
)

watch(pinned, (value) => localStorage.setItem(PINNED_STORAGE_KEY, String(value)))
watch(
  () => agent.floatingSessionId,
  (id, previousID) => {
    if (id !== previousID) subtaskStack.value = []
  },
)
watch(hidden, (value) => {
  if (value) open.value = false
})
watch(renderSidebar, (value, previous) => {
  if (value) {
    open.value = false
  } else if (previous && !hidden.value) {
    open.value = true
  }
})

function moveToSidebar() {
  if (!canUseSidebar.value) return
  open.value = false
  uiPreferences.setAgentAssistantDisplayMode('sidebar')
}

function moveToFloating() {
  open.value = true
  uiPreferences.setAgentAssistantDisplayMode('floating')
}

async function showPanel() {
  open.value = true
  error.value = ''
  agent.connect()
  try {
    await agent.loadSessions()
    await agent.restoreFloatingSession()
  } catch (cause) {
    error.value = formatAgentError(cause)
  }
}

async function togglePanel() {
  if (open.value) {
    open.value = false
    return
  }
  await showPanel()
}

async function send(text: string, files: AgentFilePartInput[] = []) {
  const value = text.trim()
  if ((!value && files.length === 0) || floatingSending.value || floatingBlocked.value) return
  error.value = ''
  agent.connect()
  if (currentNoteTitle.value) {
    try {
      await prepareNoteForAgent(currentNoteTitle.value)
    } catch (prepareError) {
      throw new Error(prepareError instanceof Error ? prepareError.message : '请先完成当前笔记保存')
    }
  }
  if (!agent.floatingSessionId) {
    await agent.initFloatingSession(currentNoteTitle.value)
  }
  agent.setFloatingNoteTitle(currentNoteTitle.value)
  await agent.sendFloatingMessage(value, value, currentNoteTitle.value, files)
}

async function sendPrompt(text: string) {
  try {
    await send(text)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '发送失败，智能体服务可能不可用'
  }
}

async function newChat() {
  if (floatingSending.value) {
    error.value = '当前任务仍在运行；请先停止或等待完成。'
    return
  }
  try {
    subtaskStack.value = []
    await agent.resetFloatingSession()
    assistantSurface.value?.clear()
    error.value = ''
  } catch {
    error.value = '操作失败'
  }
}

async function openSubtask(sessionID: string, title: string) {
  if (!sessionID || activeSubtask.value?.id === sessionID) return
  subtaskStack.value = [...subtaskStack.value, { id: sessionID, title: title || '子任务' }]
  error.value = ''
  try {
    await agent.ensureSessionLineage(sessionID)
    await agent.loadConversation(sessionID)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '暂时无法加载子任务'
  }
}

function backFromSubtask() {
  subtaskStack.value = subtaskStack.value.slice(0, -1)
  error.value = ''
}

async function recoverFloating() {
  error.value = ''
  try {
    await agent.reconnect()
    if (activeSubtask.value) await agent.loadConversation(activeSubtask.value.id)
    else await agent.restoreFloatingSession()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '重新连接失败，请稍后再试'
  }
}

const panelStyle = computed(() => ({
  width: `${panelWidth.value}px`,
  height: `${panelHeight.value}px`,
}))
</script>

<template>
  <div ref="parkedSurfaceHost" class="agent-assistant-surface-parking" aria-hidden="true" />

  <aside v-if="renderSidebar" class="agent-side-panel" aria-label="智能体侧栏">
    <div class="agent-float-header agent-side-header">
      <div class="agent-float-heading">
        <button v-if="activeSubtask" type="button" class="agent-float-back" @click="backFromSubtask">
          <LeftOutlined aria-hidden="true" />
          <span>{{ subtaskStack.length > 1 ? '上一级' : '主对话' }}</span>
        </button>
        <h2 class="agent-float-title" :title="activeSubtask?.title">{{ activeSubtask?.title || '智能体' }}</h2>
      </div>
      <div class="agent-side-actions">
        <button
          type="button"
          class="agent-side-layout-action"
          title="切换为浮窗"
          aria-label="切换为浮窗"
          @click="moveToFloating"
        >
          <MessageOutlined aria-hidden="true" />
        </button>
        <button
          v-if="!activeSubtask"
          type="button"
          class="agent-side-action"
          title="新对话"
          aria-label="新对话"
          @click="newChat"
        >
          <ReloadOutlined aria-hidden="true" />
        </button>
      </div>
    </div>
    <div ref="sidebarSurfaceHost" class="agent-assistant-surface-host" />
  </aside>

  <button
    v-if="renderFloating"
    ref="fabRef"
    :class="['agent-fab', floatingIndicator && `agent-fab-${floatingIndicator}`]"
    type="button"
    :title="open ? '关闭智能体' : '打开智能体'"
    :aria-label="open ? '关闭智能体' : '打开智能体'"
    aria-controls="agent-floating-panel"
    :aria-expanded="open"
    @click="togglePanel"
  >
    <RobotOutlined />
    <span v-if="floatingIndicator" class="agent-fab-status" aria-hidden="true" />
  </button>

  <template v-if="renderFloating">
    <Dialog.Root
      :open="open"
      :ids="floatingDialogIDs"
      lazy-mount
      unmount-on-exit
      :modal="isMobile"
      :trap-focus="isMobile"
      :prevent-scroll="isMobile"
      :restore-focus="true"
      :close-on-interact-outside="!pinned"
      :persistent-elements="persistentElements"
      @update:open="open = $event"
    >
      <template v-if="isMobile">
        <Teleport to="body">
          <Dialog.Backdrop class="dialog-backdrop" />
          <Dialog.Positioner class="agent-drawer-positioner">
            <Dialog.Content id="agent-floating-panel" class="agent-float-panel agent-float-mobile">
              <div class="agent-float-header">
                <div class="agent-float-heading">
                  <button v-if="activeSubtask" type="button" class="agent-float-back" @click="backFromSubtask">
                    <LeftOutlined aria-hidden="true" />
                    <span>{{ subtaskStack.length > 1 ? '上一级' : '主对话' }}</span>
                  </button>
                  <Dialog.Title class="agent-float-title" :title="activeSubtask?.title">
                    {{ activeSubtask?.title || '智能体' }}
                  </Dialog.Title>
                </div>
                <div class="agent-float-actions">
                  <button v-if="!activeSubtask" type="button" title="新对话" aria-label="新对话" @click="newChat">
                    <ReloadOutlined />
                  </button>
                  <Dialog.CloseTrigger as-child>
                    <button type="button" title="关闭" aria-label="关闭"><CloseOutlined /></button>
                  </Dialog.CloseTrigger>
                </div>
              </div>
              <div ref="floatingSurfaceHost" class="agent-assistant-surface-host" />
            </Dialog.Content>
          </Dialog.Positioner>
        </Teleport>
      </template>

      <template v-else>
        <Teleport to="body">
          <Dialog.Positioner class="agent-dialog-positioner">
            <Dialog.Content
              id="agent-floating-panel"
              :class="['agent-float-panel', 'agent-float-desktop', { 'is-resizing': resizing }]"
              :style="panelStyle"
            >
              <div class="agent-float-resize-handle" aria-hidden="true" @pointerdown="onResizeStart" />
              <div class="agent-float-header">
                <div class="agent-float-heading">
                  <button v-if="activeSubtask" type="button" class="agent-float-back" @click="backFromSubtask">
                    <LeftOutlined aria-hidden="true" />
                    <span>{{ subtaskStack.length > 1 ? '上一级' : '主对话' }}</span>
                  </button>
                  <Dialog.Title class="agent-float-title" :title="activeSubtask?.title">
                    {{ activeSubtask?.title || '智能体' }}
                  </Dialog.Title>
                </div>
                <div class="agent-float-actions">
                  <button
                    v-if="canUseSidebar"
                    type="button"
                    title="移到右侧栏"
                    aria-label="移到右侧栏"
                    @click="moveToSidebar"
                  >
                    <LayoutOutlined />
                  </button>
                  <button
                    type="button"
                    :class="{ active: pinned }"
                    title="保持开启"
                    aria-label="保持开启"
                    @click="pinned = !pinned"
                  >
                    <PushpinOutlined />
                  </button>
                  <button v-if="!activeSubtask" type="button" title="新对话" aria-label="新对话" @click="newChat">
                    <ReloadOutlined />
                  </button>
                  <Dialog.CloseTrigger as-child>
                    <button type="button" title="关闭" aria-label="关闭"><CloseOutlined /></button>
                  </Dialog.CloseTrigger>
                </div>
              </div>
              <div ref="floatingSurfaceHost" class="agent-assistant-surface-host" />
            </Dialog.Content>
          </Dialog.Positioner>
        </Teleport>
      </template>
    </Dialog.Root>
  </template>

  <Teleport v-if="shouldMountAssistantSurface && assistantSurfaceTarget" :to="assistantSurfaceTarget">
    <AgentAssistantSurface
      ref="assistantSurface"
      :error="floatingRuntimeNotice?.message"
      :error-title="floatingRuntimeNotice?.title"
      :error-detail="floatingRuntimeNotice?.detail"
      :error-variant="floatingRuntimeNotice?.variant"
      :error-retryable="floatingRuntimeNotice?.retryable"
      :welcome-title="welcomeTitle"
      :welcome-description="welcomeDescription"
      :prompt-items="promptItems"
      :messages="displayedMessages"
      :parts="displayedParts"
      :sending="displayedSending"
      :stopping="activeSubtask ? false : agent.floatingStopping"
      :loading="displayedLoading"
      :readonly="!!activeSubtask"
      :status="displayedStatus"
      :blocked="displayedBlocked"
      :session-id="displayedSessionId"
      :sessions="agent.allSessions"
      :session-statuses="agent.sessionStatuses"
      :session-errors="agent.sessionErrors"
      :submit-message="send"
      :stop-message="() => agent.abortFloatingSession()"
      @prompt="sendPrompt"
      @error="error = $event"
      @retry="recoverFloating"
      @open-subtask="openSubtask"
    />
  </Teleport>
</template>

<style lang="scss" scoped>
.agent-assistant-surface-parking {
  display: none;
}

.agent-assistant-surface-host {
  display: flex;
  min-width: 0;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  overflow: hidden;
}

.agent-fab {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 70;
  width: 52px;
  height: 52px;
  border-radius: 50%;
  border: none;
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 86%, transparent);
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 16px color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 35%, transparent);
  transition:
    transform 0.15s,
    box-shadow 0.15s;
  &:hover {
    transform: scale(1.06);
    box-shadow: 0 6px 22px color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 45%, transparent);
  }
}

.agent-fab-status {
  position: absolute;
  top: 1px;
  right: 1px;
  width: 12px;
  height: 12px;
  box-sizing: border-box;
  border: 2px solid var(--bg-primary);
  border-radius: 50%;
  background: var(--text-danger);
}

.agent-fab-attention .agent-fab-status,
.agent-fab-retry .agent-fab-status {
  background: #f59e0b;
}

.agent-fab-running .agent-fab-status {
  border-color: color-mix(in srgb, var(--bg-primary) 72%, transparent);
  border-top-color: #fff;
  background: transparent;
  animation: agent-fab-spin 0.8s linear infinite;
}

@keyframes agent-fab-spin {
  to {
    transform: rotate(360deg);
  }
}

.agent-dialog-positioner {
  --dialog-z-index: 1000;
  position: fixed;
  top: 16px;
  right: 24px;
  bottom: 88px;
  z-index: calc(var(--dialog-z-index) + var(--layer-index, 0));
  display: flex;
  align-items: flex-end;
}

.agent-drawer-positioner {
  --dialog-z-index: 1000;
  position: fixed;
  inset: 0;
  z-index: calc(var(--dialog-z-index) + var(--layer-index, 0));
  display: flex;
  align-items: flex-end;
  justify-content: center;
}

.agent-float-panel {
  display: flex;
  max-height: 100%;
  box-sizing: border-box;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-card, #fff);
  border: 1px solid var(--border-primary);
  border-radius: 16px;
  box-shadow: var(--shadow-card);
  &:focus {
    outline: none;
  }
}

.agent-float-desktop {
  position: relative;
  transform-origin: bottom right;
  animation: agent-float-in 180ms cubic-bezier(0.16, 1, 0.3, 1);
  &.is-resizing {
    user-select: none;
  }
  &[data-state='closed'] {
    pointer-events: none;
    animation: agent-float-out 120ms cubic-bezier(0.7, 0, 0.84, 0) forwards;
  }
}

.agent-float-mobile {
  width: 100% !important;
  height: 90vh !important;
  height: 90dvh !important;
  max-height: 100dvh;
  border-radius: 16px 16px 0 0;
  animation: agent-drawer-in 220ms cubic-bezier(0.16, 1, 0.3, 1);
  &[data-state='closed'] {
    pointer-events: none;
    animation: agent-drawer-out 150ms cubic-bezier(0.7, 0, 0.84, 0) forwards;
  }
}

.agent-side-panel {
  display: flex;
  width: clamp(340px, 27vw, 400px);
  min-width: clamp(340px, 27vw, 400px);
  height: 100%;
  box-sizing: border-box;
  flex-direction: column;
  overflow: hidden;
  border-left: 1px solid var(--border-primary);
  background: var(--bg-card);
  color: var(--text-primary);
}
.agent-side-header {
  border-bottom: 1px solid var(--border-primary);
}
.agent-side-actions {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 4px;
}
.agent-side-layout-action,
.agent-side-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 32px;
  padding: 0 9px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font: inherit;
  font-size: var(--marvo-type-12);
  transition:
    background 0.15s,
    color 0.15s;
}
.agent-side-layout-action {
  width: 32px;
  padding: 0;
  font-size: var(--marvo-type-16);
}
.agent-side-action {
  width: 32px;
  padding: 0;
  font-size: var(--marvo-type-16);
}
.agent-side-layout-action:hover,
.agent-side-action:hover {
  background: var(--bg-hover);
  color: var(--text-accent);
}
.agent-side-layout-action:focus-visible,
.agent-side-action:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 42%, transparent);
  outline-offset: 1px;
}

@keyframes agent-float-in {
  from {
    opacity: 0;
    transform: translateY(10px) scale(0.96);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

@keyframes agent-float-out {
  from {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
  to {
    opacity: 0;
    transform: translateY(10px) scale(0.96);
  }
}

@keyframes agent-drawer-in {
  from {
    transform: translateY(100%);
  }
  to {
    transform: translateY(0);
  }
}

@keyframes agent-drawer-out {
  from {
    transform: translateY(0);
  }
  to {
    transform: translateY(100%);
  }
}

.agent-float-resize-handle {
  position: absolute;
  top: 0;
  left: 0;
  width: 44px;
  height: 44px;
  cursor: nwse-resize;
  touch-action: none;
  user-select: none;
  -webkit-user-select: none;
  z-index: 3;
  &::before {
    content: '';
    position: absolute;
    top: 8px;
    left: 8px;
    width: 12px;
    height: 12px;
    border-left: 2px solid var(--border-light);
    border-top: 2px solid var(--border-light);
    border-radius: 2px 0 0 0;
  }
}

.agent-float-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px 10px;
  color: var(--text-primary);
  flex-shrink: 0;
}
.agent-float-header.agent-side-header {
  height: var(--dsh-shell-header-height, 52px);
  min-height: var(--dsh-shell-header-height, 52px);
  padding: 0 16px;
}
.agent-float-desktop > .agent-float-header {
  padding-left: 52px;
}
.agent-float-heading {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}
.agent-float-back {
  min-height: 30px;
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 3px 7px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  touch-action: manipulation;
  font: inherit;
  font-size: var(--marvo-type-11);
  -webkit-tap-highlight-color: transparent;

  &:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 42%, transparent);
    outline-offset: 1px;
  }
}
.agent-float-title {
  margin: 0;
  min-width: 0;
  overflow: hidden;
  font: inherit;
  font-size: var(--marvo-type-16);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.agent-float-actions {
  display: flex;
  gap: 4px;
  button {
    width: 28px;
    height: 28px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
    border-radius: 6px;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: var(--marvo-type-16);
    transition:
      background 0.15s,
      color 0.15s;
    &:hover,
    &.active {
      background: var(--bg-hover);
      color: var(--text-accent);
    }
  }
}

@media (hover: none), (max-width: 768px) {
  .agent-float-actions button,
  .agent-float-back,
  .agent-side-layout-action,
  .agent-side-action {
    min-width: 40px;
    min-height: 40px;
  }
}
</style>
