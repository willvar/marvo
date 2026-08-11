<script setup lang="ts">
import { ref } from 'vue'
import type { SessionStatus } from '@opencode-ai/sdk/v2/client'
import type { AgentFilePartInput, AgentSession, AgentSessionError, MessageInfo, MessagePart } from '../sdk'
import AgentComposer from './AgentComposer.vue'
import AgentMessageList from './AgentMessageList.vue'
import AgentRequestPrompts from './AgentRequestPrompts.vue'
import { XButton, XErrorCard, XPrompts, XWelcome, type XPromptItem } from './x'
import { LockOutlined, RobotOutlined } from '@ant-design/icons-vue'

withDefaults(
  defineProps<{
    error?: string
    errorTitle?: string
    errorDetail?: string
    errorVariant?: 'error' | 'warning' | 'info'
    errorRetryable?: boolean
    welcomeTitle: string
    welcomeDescription: string
    promptItems: XPromptItem[]
    messages: MessageInfo[]
    parts: Record<string, MessagePart[]>
    sending?: boolean
    stopping?: boolean
    blocked?: boolean
    loading?: boolean
    readonly?: boolean
    status?: SessionStatus
    sessions?: AgentSession[]
    sessionStatuses?: Record<string, SessionStatus | undefined>
    sessionErrors?: Record<string, AgentSessionError | undefined>
    sessionId?: string | null
    submitMessage: (text: string, files: AgentFilePartInput[]) => Promise<void>
    stopMessage: () => Promise<void> | void
  }>(),
  {
    error: '',
    errorTitle: '操作未完成',
    errorDetail: '',
    errorVariant: 'error',
    errorRetryable: false,
    sending: false,
    stopping: false,
    blocked: false,
    loading: false,
    readonly: false,
    sessions: () => [],
    sessionStatuses: () => ({}),
    sessionErrors: () => ({}),
    sessionId: null,
  },
)

const emit = defineEmits<{
  error: [message: string]
  prompt: [text: string]
  retry: []
  'open-subtask': [sessionID: string, title: string]
}>()

const composer = ref<{ clear: () => void } | null>(null)

function clear() {
  composer.value?.clear()
}

function openSubtask(sessionID: string, title: string) {
  emit('open-subtask', sessionID, title)
}

defineExpose({ clear })
</script>

<template>
  <XErrorCard
    v-if="error"
    class="agent-assistant-error"
    :title="errorTitle"
    :message="error"
    :detail="errorDetail"
    :variant="errorVariant"
    compact
  >
    <XButton v-if="errorRetryable" variant="ghost" size="small" @click="emit('retry')">重新连接</XButton>
  </XErrorCard>

  <div class="agent-assistant-body">
    <div v-if="messages.length === 0 && !loading && !readonly" class="agent-assistant-welcome">
      <XWelcome :title="welcomeTitle" :description="welcomeDescription" :icon="RobotOutlined" compact />
      <XPrompts :items="promptItems" compact wrap @select="emit('prompt', $event.label)" />
    </div>

    <AgentMessageList
      v-else
      compact
      :messages="messages"
      :parts="parts"
      :sending="sending"
      :stopping="stopping"
      :waiting="blocked"
      :loading="loading"
      :status="status"
      :sessions="sessions"
      :session-statuses="sessionStatuses"
      :session-errors="sessionErrors"
      :scroll-reset-key="sessionId"
      :empty-title="readonly ? '暂无子任务记录' : '有什么可以帮你？'"
      :empty-description="readonly ? '该子任务尚未产生可展示内容' : '发送消息或添加图片、文件来开始对话'"
      @open-subtask="openSubtask"
    />
  </div>

  <footer class="agent-assistant-input">
    <AgentRequestPrompts v-if="sessionId" compact :session-id="sessionId" @error="emit('error', $event)" />
    <AgentComposer
      v-if="!blocked && !readonly"
      ref="composer"
      compact
      :sending="sending"
      :stopping="stopping"
      :blocked="blocked"
      :draft-key="sessionId"
      :submit-message="submitMessage"
      :stop-message="stopMessage"
      @error="emit('error', $event)"
    />
    <div v-if="readonly && !blocked" class="agent-assistant-readonly">
      <LockOutlined aria-hidden="true" />
      <span>子任务记录为只读，请返回主对话继续发送消息</span>
    </div>
  </footer>
</template>

<style lang="scss" scoped>
.agent-assistant-error {
  margin: 8px 14px 0;

  :deep(.x-button) {
    align-self: flex-start;
    margin-top: 3px;
  }
}
.agent-assistant-body {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  overflow: hidden;
}
.agent-assistant-welcome {
  display: flex;
  flex: 1;
  flex-direction: column;
  justify-content: center;
  gap: 18px;
  padding: 22px 18px;
}
.agent-assistant-input {
  display: flex;
  flex-shrink: 0;
  flex-direction: column;
  gap: 8px;
  padding: 10px 14px 12px;
  background: linear-gradient(to bottom, transparent, var(--bg-card) 16px);
}
.agent-assistant-readonly {
  min-height: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  text-align: center;
}

@media (max-width: 768px) {
  .agent-assistant-input {
    padding-bottom: max(20px, env(safe-area-inset-bottom));
  }
}
</style>
