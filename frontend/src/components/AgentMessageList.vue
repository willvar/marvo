<script setup lang="ts">
import { computed } from 'vue'
import type { SessionStatus } from '@opencode-ai/sdk/v2/client'
import { RobotOutlined, UserOutlined } from '@ant-design/icons-vue'
import type { AgentSession, AgentSessionError, MessageInfo, MessagePart } from '../sdk'
import { buildAgentTimeline, reconcileAgentTimeline, type AgentTimelineItem } from './agentTimeline'
import {
  XActionsCopy,
  XAttachments,
  XBubble,
  XBubbleList,
  XErrorCard,
  XMarkdown,
  XMessageDivider,
  XQuestionSummary,
  XRetry,
  XSubtaskCard,
  XThink,
  XThoughtChain,
  XWelcome,
  type XAttachmentItem,
} from './x'

const props = withDefaults(
  defineProps<{
    messages: MessageInfo[]
    parts: Record<string, MessagePart[]>
    sending?: boolean
    stopping?: boolean
    waiting?: boolean
    loading?: boolean
    compact?: boolean
    status?: SessionStatus
    sessions?: AgentSession[]
    sessionStatuses?: Record<string, SessionStatus | undefined>
    sessionErrors?: Record<string, AgentSessionError | undefined>
    scrollResetKey?: string | number | null
    emptyTitle?: string
    emptyDescription?: string
  }>(),
  {
    sending: false,
    stopping: false,
    waiting: false,
    loading: false,
    compact: false,
    status: () => ({ type: 'idle' }),
    sessions: () => [],
    sessionStatuses: () => ({}),
    sessionErrors: () => ({}),
    scrollResetKey: null,
    emptyTitle: '有什么可以帮你？',
    emptyDescription: '发送消息或添加图片、文件来开始对话',
  },
)

const emit = defineEmits<{
  'open-subtask': [sessionID: string, title: string]
}>()

const activelyRunning = computed(() => props.sending && !props.stopping && !props.waiting)
let previousTimeline: AgentTimelineItem[] = []
let previousTimelineKey = props.scrollResetKey
const timeline = computed(() => {
  if (!Object.is(previousTimelineKey, props.scrollResetKey)) {
    previousTimelineKey = props.scrollResetKey
    previousTimeline = []
  }
  const next = buildAgentTimeline(props.messages, props.parts, {
    running: activelyRunning.value,
    unsettled: props.sending,
    status: props.status,
    sessions: props.sessions,
    sessionStatuses: props.sessionStatuses,
    sessionErrors: props.sessionErrors,
  })
  previousTimeline = reconcileAgentTimeline(previousTimeline, next)
  return previousTimeline
})

function attachmentName(part: MessagePart) {
  return typeof part.filename === 'string' && part.filename ? part.filename : '附件'
}

function attachmentImageUrl(part: MessagePart) {
  if (typeof part.mime !== 'string' || !part.mime.startsWith('image/') || typeof part.url !== 'string') return ''
  return /^(data:image\/|https?:\/\/)/i.test(part.url) ? part.url : ''
}

function attachmentItems(parts: MessagePart[]): XAttachmentItem[] {
  return parts.map((part, index) => ({
    key: part.id || `attachment-${index}`,
    name: attachmentName(part),
    mime: typeof part.mime === 'string' ? part.mime : undefined,
    url: attachmentImageUrl(part) || undefined,
  }))
}

function formatTime(timestamp?: number) {
  if (!timestamp) return ''
  const date = new Date(timestamp)
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}.${pad(date.getMonth() + 1)}.${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}
</script>

<template>
  <XBubbleList
    :class="['agent-message-list', { compact }]"
    :compact="compact"
    :working="sending"
    :scroll-reset-key="scrollResetKey"
  >
    <div v-if="loading" class="page-loading agent-message-loading"><span class="page-loading-spinner" /></div>

    <template v-else>
      <div v-if="timeline.length === 0" class="agent-message-empty">
        <XWelcome :title="emptyTitle" :description="emptyDescription" :icon="RobotOutlined" :compact="compact" />
      </div>

      <template v-for="item in timeline" :key="item.key">
        <XMessageDivider
          v-if="item.role === 'divider'"
          class="agent-message-interrupted"
          :label="item.label"
          :compact="compact"
        />

        <XBubble
          v-else
          :class="['agent-message-bubble', `agent-message-${item.role}`]"
          :placement="item.role === 'user' ? 'end' : 'start'"
          :variant="item.role === 'user' ? 'filled' : 'borderless'"
          :compact="compact"
          :time="formatTime(item.created)"
        >
          <template #avatar>
            <UserOutlined v-if="item.role === 'user'" />
            <RobotOutlined v-else />
          </template>

          <template v-if="item.role === 'user'">
            <XAttachments
              v-if="item.files.length"
              class="agent-message-attachments"
              :items="attachmentItems(item.files)"
              :removable="false"
              :compact="compact"
              overflow="wrap"
            />
            <XMarkdown v-if="item.text" class="agent-message-text" :text="item.text" open-links-in-new-tab />
          </template>

          <template v-else>
            <template v-for="segment in item.segments" :key="segment.key">
              <XThink
                v-if="segment.type === 'reasoning'"
                :title="segment.heading ? `思考过程 · ${segment.heading}` : '思考过程'"
                :loading="segment.streaming"
                :loading-detail="segment.heading"
                :has-content="true"
                :compact="compact"
              >
                <XMarkdown
                  class="agent-message-text agent-message-reasoning-text"
                  :text="segment.text"
                  :streaming="segment.streaming"
                  open-links-in-new-tab
                />
              </XThink>

              <XThoughtChain v-else-if="segment.type === 'action'" :items="segment.items" :compact="compact" />

              <XSubtaskCard
                v-else-if="segment.type === 'subtask'"
                :title="segment.title"
                :description="segment.description"
                :status="segment.status"
                :background="segment.background"
                :clickable="!!segment.sessionID"
                :compact="compact"
                @open="emit('open-subtask', segment.sessionID, segment.description || segment.title)"
              />

              <XRetry
                v-else-if="segment.type === 'retry'"
                :attempt="segment.attempt"
                :message="segment.message"
                :detail="segment.detail"
                :action="segment.action"
                :next="segment.next"
                :compact="compact"
              />

              <XThink
                v-else-if="segment.type === 'thinking'"
                loading
                :loading-detail="segment.heading"
                :has-content="false"
                :compact="compact"
              />

              <XErrorCard
                v-else-if="segment.type === 'error'"
                class="agent-message-error"
                title="消息执行失败"
                :message="segment.text"
                :detail="segment.detail"
                :compact="compact"
              />

              <XQuestionSummary
                v-else-if="segment.type === 'question'"
                :status="segment.status"
                :items="segment.items"
                :message="segment.message"
                :compact="compact"
              />

              <XAttachments
                v-else-if="segment.type === 'files'"
                class="agent-message-attachments"
                :items="attachmentItems(segment.files)"
                :removable="false"
                :compact="compact"
                overflow="wrap"
              />

              <XMarkdown
                v-else-if="segment.type === 'text'"
                :class="['agent-message-text', { 'agent-message-text-intermediate': !segment.final }]"
                :text="segment.text"
                :streaming="segment.streaming"
                open-links-in-new-tab
              />
            </template>
          </template>

          <template v-if="item.role === 'assistant' && item.copyText && !item.streaming" #footer>
            <XActionsCopy :text="item.copyText" :compact="compact" />
          </template>
        </XBubble>
      </template>
    </template>
  </XBubbleList>
</template>

<style lang="scss" scoped>
.agent-message-list {
  background: var(--bg-primary);
}

.agent-message-loading {
  min-height: 160px;
}

.agent-message-empty {
  width: min(100%, 620px);
  margin: auto;
  padding: 32px 0;
}

.agent-message-user :deep(.x-bubble-avatar) {
  background: var(--marvo-accent-color, #4f46e5);
  color: #fff;
}

.agent-message-text {
  color: var(--text-primary);
  word-break: break-word;

  & + &,
  & + .agent-message-error,
  & + :deep(.x-question-summary),
  & + :deep(.x-retry),
  & + :deep(.x-thought-chain),
  & + :deep(.x-subtask-card),
  & + :deep(.x-think) {
    margin-top: 14px;
  }

  :deep(p) {
    margin: 0 0 8px;
  }

  :deep(p:last-child) {
    margin-bottom: 0;
  }

  :deep(a) {
    color: var(--text-accent);
    text-decoration: none;

    &:hover {
      text-decoration: underline;
    }
  }

  :deep(code) {
    padding: 2px 6px;
    border-radius: 5px;
    background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 11%, var(--bg-secondary));
    font-family: 'SF Mono', 'Fira Code', ui-monospace, monospace;
    font-size: var(--marvo-type-12);
  }

  :deep(pre) {
    margin: 10px 0;
    padding: 12px 14px;
    overflow: auto;
    border: 1px solid var(--border-light);
    border-radius: 9px;
    background: var(--bg-tertiary);
    color: var(--text-primary);
    font-size: var(--marvo-type-12);
    line-height: 1.55;
  }

  :deep(pre code) {
    padding: 0;
    border-radius: 0;
    background: none;
  }

  :deep(ul),
  :deep(ol) {
    margin: 6px 0 8px;
    padding-left: 20px;
  }

  :deep(blockquote) {
    margin: 8px 0;
    padding-left: 12px;
    border-left: 3px solid var(--border-primary);
    color: var(--text-tertiary);
  }

  :deep(table) {
    margin: 10px 0;
    border-collapse: collapse;
    font-size: var(--marvo-type-13);
  }

  :deep(th),
  :deep(td) {
    padding: 6px 8px;
    border: 1px solid var(--border-light);
  }

  :deep(th) {
    background: var(--bg-secondary);
  }
}

.agent-message-text-intermediate {
  color: var(--text-secondary);
}

.agent-message-reasoning-text {
  color: var(--text-tertiary);
}

.agent-message-interrupted {
  margin-block: 2px;
}

.agent-message-attachments {
  margin-bottom: 10px;
}

.compact .agent-message-text :deep(pre) {
  padding: 10px;
  font-size: var(--marvo-type-11);
}
</style>
