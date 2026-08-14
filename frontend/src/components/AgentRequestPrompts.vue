<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Checkbox, type CheckboxCheckedState } from '@ark-ui/vue/checkbox'
import { RadioGroup } from '@ark-ui/vue/radio-group'
import {
  CheckOutlined,
  CloseOutlined,
  DownOutlined,
  LeftOutlined,
  LoadingOutlined,
  RightOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons-vue'
import { formatAgentError } from '../sdk'
import { useAgentStore } from '../stores/agent'
import { XButton } from './x'

const props = defineProps<{ sessionId: string; compact?: boolean }>()
const emit = defineEmits<{ error: [message: string] }>()
const agent = useAgentStore()

interface QuestionDraft {
  tab: number
  answers: string[][]
  custom: string[]
  customOn: boolean[]
  minimized: boolean
}

const questionDrafts = new Map<string, QuestionDraft>()
const settledDrafts = new Set<string>()
const activeDraftID = ref('')
const respondingId = ref('')
const tab = ref(0)
const answers = ref<string[][]>([])
const custom = ref<string[]>([])
const customOn = ref<boolean[]>([])
const minimized = ref(false)

const permissionRequest = computed(() => agent.pendingPermission(props.sessionId))
const questionRequest = computed(() => agent.pendingQuestion(props.sessionId))
const questions = computed(() => questionRequest.value?.questions || [])
const currentQuestion = computed(() => questions.value[tab.value])
const lastQuestion = computed(() => tab.value >= questions.value.length - 1)
const responding = computed(() => respondingId.value !== '')
const canAdvance = computed(() => answered(tab.value))
const customValue = computed(() => `__marvo_custom_${questionRequest.value?.id || ''}_${tab.value}`)
const singleValue = computed<string | null>({
  get() {
    if (customOn.value[tab.value]) return customValue.value
    return answers.value[tab.value]?.[0] || null
  },
  set(value) {
    if (!value) return
    if (value === customValue.value) {
      enableCustom()
      return
    }
    setCustomOn(tab.value, false)
    setAnswers(tab.value, [value])
  },
})

watch(
  () => questionRequest.value?.id,
  (id, previousID) => {
    if (previousID) {
      if (settledDrafts.delete(previousID)) questionDrafts.delete(previousID)
      else saveDraft(previousID)
    }
    if (!id) {
      activeDraftID.value = ''
      resetDraft()
      return
    }
    activeDraftID.value = id
    const request = questionRequest.value
    const cached = questionDrafts.get(id)
    const count = request?.questions.length || 0
    tab.value = Math.min(cached?.tab || 0, Math.max(0, count - 1))
    answers.value = Array.from({ length: count }, (_, index) => [...(cached?.answers[index] || [])])
    custom.value = Array.from({ length: count }, (_, index) => cached?.custom[index] || '')
    customOn.value = Array.from({ length: count }, (_, index) => cached?.customOn[index] || false)
    minimized.value = cached?.minimized || false
  },
  { immediate: true },
)

watch(
  [tab, answers, custom, customOn, minimized],
  () => {
    const id = questionRequest.value?.id
    if (id) saveDraft(id)
  },
  { deep: true },
)

onBeforeUnmount(() => {
  const id = activeDraftID.value
  if (id && !settledDrafts.has(id)) saveDraft(id)
})

function resetDraft() {
  tab.value = 0
  answers.value = []
  custom.value = []
  customOn.value = []
  minimized.value = false
}

function saveDraft(id: string) {
  questionDrafts.set(id, {
    tab: tab.value,
    answers: answers.value.map((item) => [...item]),
    custom: [...custom.value],
    customOn: [...customOn.value],
    minimized: minimized.value,
  })
}

function setAnswers(index: number, value: string[]) {
  const next = answers.value.map((item) => [...item])
  next[index] = value
  answers.value = next
}

function setCustomOn(index: number, value: boolean) {
  const next = [...customOn.value]
  next[index] = value
  customOn.value = next
}

function setCustom(index: number, value: string) {
  const previous = custom.value[index]?.trim() || ''
  const next = [...custom.value]
  next[index] = value
  custom.value = next
  if (!customOn.value[index]) return

  const normalized = value.trim()
  if (questions.value[index]?.multiple) {
    const selected = (answers.value[index] || []).filter((answer) => answer !== previous)
    setAnswers(index, normalized && !selected.includes(normalized) ? [...selected, normalized] : selected)
    return
  }
  setAnswers(index, normalized ? [normalized] : [])
}

function enableCustom() {
  setCustomOn(tab.value, true)
  const value = custom.value[tab.value]?.trim() || ''
  if (!currentQuestion.value?.multiple) setAnswers(tab.value, value ? [value] : [])
  else if (value && !answers.value[tab.value]?.includes(value))
    setAnswers(tab.value, [...(answers.value[tab.value] || []), value])
  void nextTick(() => {
    document
      .querySelector<HTMLTextAreaElement>(`[data-agent-custom-answer="${questionRequest.value?.id}-${tab.value}"]`)
      ?.focus()
  })
}

function toggleMultiple(label: string, checked: CheckboxCheckedState) {
  const selected = answers.value[tab.value] || []
  setAnswers(
    tab.value,
    checked === true ? [...new Set([...selected, label])] : selected.filter((value) => value !== label),
  )
}

function toggleCustomMultiple(checked: CheckboxCheckedState) {
  const enabled = checked === true
  const value = custom.value[tab.value]?.trim() || ''
  setCustomOn(tab.value, enabled)
  if (!value) {
    if (enabled) enableCustom()
    return
  }
  const selected = answers.value[tab.value] || []
  setAnswers(tab.value, enabled ? [...new Set([...selected, value])] : selected.filter((answer) => answer !== value))
  if (enabled) enableCustom()
}

function answered(index: number) {
  return (answers.value[index]?.length || 0) > 0
}

function jump(index: number) {
  if (responding.value) return
  tab.value = Math.max(0, Math.min(index, questions.value.length - 1))
  minimized.value = false
}

function back() {
  jump(tab.value - 1)
}

async function next() {
  if (!canAdvance.value) return
  if (!lastQuestion.value) {
    jump(tab.value + 1)
    return
  }
  await submitQuestion()
}

function requestError(prefix: string, cause: unknown) {
  const message = formatAgentError(cause)
  return message ? `${prefix}：${message}` : prefix
}

async function permission(reply: 'once' | 'always' | 'reject') {
  const request = permissionRequest.value
  if (!request || responding.value) return
  respondingId.value = request.id
  try {
    await agent.respondPermission(request.id, reply)
  } catch (cause) {
    emit('error', requestError('权限响应失败', cause))
  } finally {
    respondingId.value = ''
  }
}

async function submitQuestion() {
  const request = questionRequest.value
  if (!request || responding.value || request.questions.some((_, index) => !answered(index))) return
  respondingId.value = request.id
  settledDrafts.add(request.id)
  try {
    await agent.respondQuestion(
      request.id,
      request.questions.map((_, index) => answers.value[index] || []),
    )
    questionDrafts.delete(request.id)
  } catch (cause) {
    settledDrafts.delete(request.id)
    emit('error', requestError('回答提交失败', cause))
  } finally {
    respondingId.value = ''
  }
}

async function rejectQuestion() {
  const request = questionRequest.value
  if (!request || responding.value) return
  respondingId.value = request.id
  settledDrafts.add(request.id)
  try {
    await agent.rejectQuestion(request.id)
    questionDrafts.delete(request.id)
  } catch (cause) {
    settledDrafts.delete(request.id)
    emit('error', requestError('取消提问失败', cause))
  } finally {
    respondingId.value = ''
  }
}

function handleKeydown(event: KeyboardEvent) {
  if (responding.value) return
  if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
    event.preventDefault()
    void next()
    return
  }
  if (event.key !== 'Escape') return
  if (event.target instanceof HTMLTextAreaElement) {
    event.target.blur()
    return
  }
  event.preventDefault()
  void rejectQuestion()
}

function permissionDescription(permissionName: string) {
  const descriptions: Record<string, string> = {
    read: '读取工作区内容',
    glob: '查找文件',
    grep: '搜索文件内容',
    edit: '修改工作区文件',
    bash: '执行命令',
    task: '运行子任务',
    webfetch: '访问网页',
    websearch: '搜索网络',
    external_directory: '访问工作区外的路径',
  }
  return descriptions[permissionName] || '执行一项需要确认的操作'
}
</script>

<template>
  <section
    v-if="questionRequest && currentQuestion"
    :class="['agent-request', 'agent-question', { compact, minimized }]"
    @keydown="handleKeydown"
  >
    <div class="agent-request-shell">
      <header class="agent-request-header">
        <strong class="agent-question-summary">{{ `第 ${tab + 1} 题，共 ${questions.length} 题` }}</strong>
        <div class="agent-request-header-actions">
          <div v-if="questions.length > 1" class="agent-question-progress" aria-label="问题进度">
            <button
              v-for="(_, index) in questions"
              :key="index"
              type="button"
              :class="{ active: index === tab, answered: answered(index) }"
              :aria-label="`第 ${index + 1} 题${answered(index) ? '，已回答' : ''}`"
              :disabled="responding"
              @click="jump(index)"
            />
          </div>
          <XButton
            class="agent-request-minimize"
            size="small"
            variant="ghost"
            :aria-label="minimized ? '展开提问' : '收起提问'"
            :title="minimized ? '展开' : '收起'"
            :disabled="responding"
            @click="minimized = !minimized"
          >
            <DownOutlined :class="{ rotated: minimized }" />
          </XButton>
        </div>
      </header>

      <div class="agent-request-question-text">{{ currentQuestion.question }}</div>

      <div v-if="!minimized" class="agent-request-body">
        <p class="agent-question-hint">{{ currentQuestion.multiple ? '可选择多项' : '请选择一项' }}</p>

        <div v-if="currentQuestion.multiple" class="agent-question-options">
          <Checkbox.Root
            v-for="option in currentQuestion.options"
            :key="option.label"
            class="agent-question-option"
            :checked="answers[tab]?.includes(option.label)"
            :disabled="responding"
            @update:checked="toggleMultiple(option.label, $event)"
          >
            <Checkbox.HiddenInput />
            <Checkbox.Control class="agent-question-control"><CheckOutlined /></Checkbox.Control>
            <Checkbox.Label class="agent-question-option-copy">
              <b>{{ option.label }}</b>
              <span>{{ option.description }}</span>
            </Checkbox.Label>
          </Checkbox.Root>

          <Checkbox.Root
            v-if="currentQuestion.custom !== false"
            class="agent-question-option agent-question-custom-option"
            :checked="customOn[tab]"
            :disabled="responding"
            @update:checked="toggleCustomMultiple"
          >
            <Checkbox.HiddenInput />
            <Checkbox.Control class="agent-question-control"><CheckOutlined /></Checkbox.Control>
            <Checkbox.Label class="agent-question-option-copy">
              <b>输入自己的答案</b>
              <textarea
                v-if="customOn[tab]"
                :data-agent-custom-answer="`${questionRequest.id}-${tab}`"
                :value="custom[tab] || ''"
                rows="1"
                placeholder="输入答案…"
                :disabled="responding"
                @click.stop="enableCustom"
                @input="setCustom(tab, ($event.target as HTMLTextAreaElement).value)"
              />
              <span v-else>输入答案…</span>
            </Checkbox.Label>
          </Checkbox.Root>
        </div>

        <RadioGroup.Root v-else v-model="singleValue" class="agent-question-options" :disabled="responding">
          <RadioGroup.Item
            v-for="option in currentQuestion.options"
            :key="option.label"
            class="agent-question-option"
            :value="option.label"
          >
            <RadioGroup.ItemHiddenInput />
            <RadioGroup.ItemControl class="agent-question-control"><span /></RadioGroup.ItemControl>
            <RadioGroup.ItemText class="agent-question-option-copy">
              <b>{{ option.label }}</b>
              <span>{{ option.description }}</span>
            </RadioGroup.ItemText>
          </RadioGroup.Item>

          <RadioGroup.Item
            v-if="currentQuestion.custom !== false"
            class="agent-question-option agent-question-custom-option"
            :value="customValue"
            @click="enableCustom"
          >
            <RadioGroup.ItemHiddenInput />
            <RadioGroup.ItemControl class="agent-question-control"><span /></RadioGroup.ItemControl>
            <RadioGroup.ItemText class="agent-question-option-copy">
              <b>输入自己的答案</b>
              <textarea
                v-if="customOn[tab]"
                :data-agent-custom-answer="`${questionRequest.id}-${tab}`"
                :value="custom[tab] || ''"
                rows="1"
                placeholder="输入答案…"
                :disabled="responding"
                @click.stop="enableCustom"
                @input="setCustom(tab, ($event.target as HTMLTextAreaElement).value)"
              />
              <span v-else>输入答案…</span>
            </RadioGroup.ItemText>
          </RadioGroup.Item>
        </RadioGroup.Root>
      </div>
    </div>

    <footer class="agent-request-actions">
      <XButton variant="ghost" :disabled="responding" @click="rejectQuestion">
        <CloseOutlined aria-hidden="true" />取消
      </XButton>
      <div>
        <XButton v-if="tab > 0" variant="secondary" :disabled="responding" @click="back">
          <LeftOutlined aria-hidden="true" />上一题
        </XButton>
        <XButton :variant="lastQuestion ? 'primary' : 'secondary'" :disabled="responding || !canAdvance" @click="next">
          <LoadingOutlined v-if="responding" class="agent-request-loading" aria-hidden="true" />
          <CheckOutlined v-else-if="lastQuestion" aria-hidden="true" />
          <RightOutlined v-else aria-hidden="true" />
          {{ lastQuestion ? '提交' : '下一题' }}
        </XButton>
      </div>
    </footer>
  </section>

  <section v-else-if="permissionRequest" :class="['agent-request', 'agent-permission', { compact }]">
    <div class="agent-request-shell">
      <header class="agent-request-header">
        <div class="agent-request-heading">
          <SafetyCertificateOutlined aria-hidden="true" />
          <div>
            <strong>需要确认权限</strong>
            <span>{{ permissionDescription(permissionRequest.permission) }}</span>
          </div>
        </div>
      </header>
      <div v-if="permissionRequest.patterns?.length" class="agent-permission-patterns">
        <code v-for="pattern in permissionRequest.patterns" :key="pattern">{{ pattern }}</code>
      </div>
    </div>
    <footer class="agent-request-actions agent-permission-actions">
      <XButton variant="ghost" :disabled="responding" @click="permission('reject')">
        <CloseOutlined aria-hidden="true" />拒绝
      </XButton>
      <div>
        <XButton variant="secondary" :disabled="responding" @click="permission('always')">
          <SafetyCertificateOutlined aria-hidden="true" />始终允许
        </XButton>
        <XButton variant="primary" :disabled="responding" @click="permission('once')">
          <LoadingOutlined v-if="responding" class="agent-request-loading" aria-hidden="true" />
          <CheckOutlined v-else aria-hidden="true" />允许本次
        </XButton>
      </div>
    </footer>
  </section>
</template>

<style lang="scss" scoped>
.agent-request {
  position: relative;
  width: 100%;
  min-height: 0;
  color: var(--text-primary);
  font-size: var(--marvo-type-12);
}

.agent-request-shell {
  position: relative;
  z-index: 2;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
}

.agent-request-header {
  min-height: 34px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 10px 0 18px;
}

.agent-question-summary {
  min-width: 0;
  color: var(--text-primary);
  font-size: var(--marvo-type-13);
  font-weight: 500;
  line-height: 1.5;
}

.agent-request-heading {
  min-width: 0;
  display: flex;
  align-items: flex-start;
  gap: 8px;

  > :first-child {
    flex: none;
    margin-top: 2px;
    color: var(--text-accent);
    font-size: var(--marvo-type-15);
  }

  > div {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  strong {
    font-size: var(--marvo-type-13);
  }

  span {
    color: var(--text-tertiary);
    font-size: var(--marvo-type-11);
  }
}

.agent-request-header-actions,
.agent-request-actions > div {
  display: flex;
  align-items: center;
  gap: 6px;
}

.agent-question-progress {
  display: flex;
  align-items: center;
  gap: 2px;
  max-width: min(40vw, 240px);
  overflow-x: auto;
  scrollbar-width: none;

  &::-webkit-scrollbar {
    display: none;
  }

  button {
    width: 32px;
    height: 32px;
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    border: none;
    border-radius: 50%;
    background: transparent;
    cursor: pointer;
    touch-action: manipulation;

    &::after {
      content: '';
      width: 16px;
      height: 2px;
      border-radius: 2px;
      background: var(--border-primary);
      transition: background 0.16s;
    }

    &.answered::after {
      background: color-mix(in srgb, var(--marvo-accent-color) 55%, var(--border-primary));
    }

    &.active::after {
      background: var(--marvo-accent-color);
    }

    &:disabled {
      cursor: default;
      opacity: 0.55;
    }
  }
}

.agent-request-minimize {
  width: 36px;
  padding: 0;

  svg {
    transition: transform 0.18s;
  }

  svg.rotated {
    transform: rotate(180deg);
  }
}

.agent-request-question-text {
  padding: 10px 18px 0;
  color: var(--text-primary);
  font-size: var(--marvo-type-14);
  font-weight: 500;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.minimized .agent-request-question-text {
  display: -webkit-box;
  overflow: hidden;
  padding-bottom: 14px;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.agent-request-body {
  max-height: min(44vh, 360px);
  overflow-y: auto;
  padding: 0 8px 16px;
  scrollbar-width: none;

  &::-webkit-scrollbar {
    display: none;
  }
}

.agent-question-hint {
  margin: 2px 10px 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
  line-height: 1.5;
}

.agent-question-options {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 12px;
  outline: none;
}

.agent-question-option {
  min-width: 0;
  display: flex;
  align-items: flex-start;
  width: 100%;
  box-sizing: border-box;
  gap: 12px;
  padding: 8px 8px 8px 10px;
  border: 1px solid var(--border-light);
  border-radius: 6px;
  outline: none;
  background: var(--bg-secondary);
  cursor: pointer;
  touch-action: manipulation;
  transition:
    border-color 0.15s,
    background 0.15s;

  &:hover:not([data-disabled]),
  &[data-focus] {
    border-color: color-mix(in srgb, var(--marvo-accent-color) 30%, var(--border-primary));
    background: var(--bg-primary);
  }

  &[data-state='checked'] {
    border-color: transparent;
    background: color-mix(in srgb, var(--marvo-accent-color) 10%, var(--bg-card));
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--marvo-accent-color) 22%, transparent);
  }

  &[data-disabled] {
    cursor: default;
    opacity: 0.58;
  }
}

.agent-question-control {
  width: 16px;
  height: 16px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 2px;
  border: 1px solid var(--border-primary);
  border-radius: 4px;
  background: var(--bg-card);
  color: transparent;
  font-size: 10px;

  [data-state='checked'] &,
  &[data-state='checked'] {
    border-color: var(--marvo-accent-color);
    background: var(--marvo-accent-color);
    color: #fff;
  }
}

[data-scope='radio-group'][data-part='item-control'].agent-question-control {
  border-radius: 50%;

  span {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
  }
}

.agent-question-option-copy {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 1px;
  color: var(--text-primary);

  b {
    font-size: var(--marvo-type-13);
    font-weight: 500;
    line-height: 1.5;
  }

  span {
    color: var(--text-tertiary);
    line-height: 1.45;
  }

  textarea {
    width: 100%;
    min-height: 22px;
    max-height: 90px;
    box-sizing: border-box;
    resize: none;
    margin-top: 1px;
    padding: 0;
    border: none;
    border-radius: 3px;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    line-height: 1.45;

    &:focus-visible {
      outline: 1px solid var(--marvo-accent-color);
      outline-offset: 2px;
    }
  }
}

.agent-request-actions {
  position: relative;
  z-index: 1;
  min-height: 62px;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: -14px;
  padding: 24px 8px 8px;
  border: 1px solid var(--border-primary);
  border-radius: 0 0 12px 12px;
  background: var(--bg-primary);
}

.agent-request-loading {
  animation: agent-request-spin 0.9s linear infinite;
}

.agent-permission {
  .agent-request-shell {
    border-color: color-mix(in srgb, #d97706 38%, var(--border-primary));
  }

  .agent-request-heading > :first-child {
    color: #d97706;
  }
}

.agent-permission-patterns {
  max-height: 150px;
  display: flex;
  flex-direction: column;
  gap: 5px;
  overflow-y: auto;
  padding: 10px 14px;

  code {
    padding: 5px 7px;
    border-radius: 6px;
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: var(--marvo-type-11);
    overflow-wrap: anywhere;
  }
}

.agent-permission-actions {
  justify-content: flex-end;
}

.compact {
  .agent-request-shell {
    border-radius: 10px;
  }

  .agent-request-header {
    min-height: 32px;
    padding: 6px 7px 0 12px;
  }

  .agent-request-question-text {
    padding: 8px 12px 0;
    font-size: var(--marvo-type-12);
  }

  .agent-request-body {
    max-height: min(36vh, 280px);
    padding: 0 6px 14px;
  }

  .agent-question-hint {
    margin-inline: 6px;
  }

  .agent-question-option {
    padding: 7px 8px;
  }

  .agent-request-actions {
    min-height: 58px;
    flex-wrap: wrap;
    padding: 23px 6px 6px;
  }
}

@keyframes agent-request-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 620px) {
  .agent-request-header {
    align-items: flex-start;
  }

  .agent-question-progress button {
    width: 40px;
    height: 40px;
  }

  .agent-request-minimize {
    width: 40px;
  }

  .agent-request-actions {
    flex-wrap: wrap;
  }
}

@media (hover: none) and (pointer: coarse) {
  .agent-question-progress button,
  .agent-request-minimize {
    width: 40px;
    height: 40px;
  }
}
</style>
