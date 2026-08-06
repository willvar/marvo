<script setup lang="ts">
import { ref } from 'vue'
import type { SessionStatus } from '@opencode-ai/sdk/v2/client'
import type { AgentFilePartInput, MessageInfo, MessagePart } from '../sdk'
import AgentComposer from './AgentComposer.vue'
import AgentMessageList from './AgentMessageList.vue'
import AgentRequestPrompts from './AgentRequestPrompts.vue'
import { XButton, XErrorCard, XPrompts, XWelcome, type XPromptItem } from './x'
import { RobotOutlined } from '@ant-design/icons-vue'

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
    status?: SessionStatus
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
    sessionId: null,
  },
)

const emit = defineEmits<{
  error: [message: string]
  prompt: [text: string]
  retry: []
}>()

const composer = ref<{ clear: () => void } | null>(null)

function clear() {
  composer.value?.clear()
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
    <div v-if="messages.length === 0" class="agent-assistant-welcome">
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
      :status="status"
      :scroll-reset-key="sessionId"
    />
  </div>

  <footer class="agent-assistant-input">
    <AgentRequestPrompts v-if="sessionId" compact :session-id="sessionId" @error="emit('error', $event)" />
    <AgentComposer
      v-if="!blocked"
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

@media (max-width: 768px) {
  .agent-assistant-input {
    padding-bottom: max(20px, env(safe-area-inset-bottom));
  }
}
</style>
