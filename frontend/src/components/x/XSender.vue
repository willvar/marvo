<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { ArrowUpOutlined, InboxOutlined } from '@ant-design/icons-vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string
    disabled?: boolean
    loading?: boolean
    stopping?: boolean
    submitting?: boolean
    submitDisabled?: boolean
    compact?: boolean
  }>(),
  {
    placeholder: '输入消息...',
    disabled: false,
    loading: false,
    stopping: false,
    submitting: false,
    submitDisabled: true,
    compact: false,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  submit: [value: string]
  cancel: []
  pasteFiles: [files: File[]]
}>()

const inputRef = ref<HTMLTextAreaElement | null>(null)
const composing = ref(false)
const dragging = ref(false)
const actionDisabled = computed(() => props.disabled || props.submitting || props.submitDisabled)
let dragDepth = 0

function updateValue(event: Event) {
  emit('update:modelValue', (event.target as HTMLTextAreaElement).value)
}

function submit() {
  if (props.loading || actionDisabled.value) return
  emit('submit', props.modelValue)
}

function handleKeydown(event: KeyboardEvent) {
  if (composing.value || event.isComposing || event.key !== 'Enter') return
  if (event.shiftKey || event.ctrlKey || event.altKey || event.metaKey) return
  event.preventDefault()
  submit()
}

function handlePaste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.files || [])
  const text = event.clipboardData?.getData('text/plain') || ''
  if (files.length === 0) return
  if (!text) event.preventDefault()
  emit('pasteFiles', files)
}

function handleDragEnter(event: DragEvent) {
  if (props.disabled || !event.dataTransfer?.types.includes('Files')) return
  dragDepth++
  dragging.value = true
}

function handleDragLeave() {
  dragDepth = Math.max(0, dragDepth - 1)
  if (dragDepth === 0) dragging.value = false
}

function handleDrop(event: DragEvent) {
  dragDepth = 0
  dragging.value = false
  if (props.disabled) return
  const files = Array.from(event.dataTransfer?.files || [])
  if (files.length) emit('pasteFiles', files)
}

function focus() {
  inputRef.value?.focus()
}

function clear() {
  emit('update:modelValue', '')
}

async function insert(value: string) {
  const input = inputRef.value
  if (!input) return
  const start = input.selectionStart ?? props.modelValue.length
  const end = input.selectionEnd ?? props.modelValue.length
  emit('update:modelValue', `${props.modelValue.slice(0, start)}${value}${props.modelValue.slice(end)}`)
  await nextTick()
  input.focus()
  input.setSelectionRange(start + value.length, start + value.length)
}

defineExpose({ focus, clear, insert, inputElement: inputRef })
</script>

<template>
  <div
    :class="['x-sender', { 'x-sender-disabled': disabled, 'x-sender-compact': compact, 'x-sender-dragging': dragging }]"
    @click.self="focus"
    @dragenter.prevent="handleDragEnter"
    @dragover.prevent
    @dragleave.prevent="handleDragLeave"
    @drop.prevent="handleDrop"
  >
    <div v-if="dragging" class="x-sender-drop-mask">
      <InboxOutlined />
      <span>松开添加文件</span>
    </div>

    <div v-if="$slots.header" class="x-sender-header">
      <slot name="header" />
    </div>

    <div class="x-sender-content" @click.self="focus">
      <textarea
        ref="inputRef"
        class="x-sender-input"
        rows="1"
        :value="modelValue"
        :placeholder="placeholder"
        :disabled="disabled"
        aria-label="消息"
        @input="updateValue"
        @keydown="handleKeydown"
        @paste="handlePaste"
        @compositionstart="composing = true"
        @compositionend="composing = false"
      />
    </div>

    <div class="x-sender-toolbar">
      <div v-if="$slots.prefix" class="x-sender-prefix">
        <slot name="prefix" />
      </div>
      <div class="x-sender-suffix">
        <slot name="suffix">
          <button v-if="stopping" class="x-sender-action" type="button" aria-label="正在停止" disabled>
            <span class="x-sender-spinner" />
          </button>
          <button
            v-else-if="loading"
            class="x-sender-action x-sender-action-stop"
            type="button"
            aria-label="停止"
            title="停止"
            @click="emit('cancel')"
          >
            <span class="x-sender-stop-mark" />
          </button>
          <button v-else-if="submitting" class="x-sender-action" type="button" aria-label="处理中" disabled>
            <span class="x-sender-spinner" />
          </button>
          <button
            v-else
            class="x-sender-action x-sender-action-submit"
            type="button"
            aria-label="发送"
            title="发送"
            :disabled="actionDisabled"
            @click="submit"
          >
            <ArrowUpOutlined />
          </button>
        </slot>
      </div>
    </div>

    <div v-if="$slots.footer" class="x-sender-footer">
      <slot name="footer" />
    </div>
  </div>
</template>

<style lang="scss" scoped>
.x-sender {
  position: relative;
  width: 100%;
  box-sizing: border-box;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--border-primary) 78%, transparent);
  border-radius: 16px;
  background: var(--bg-card);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 10%, transparent);
  transition:
    border-color 0.2s,
    box-shadow 0.2s,
    background 0.2s;

  &:focus-within {
    border-color: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 55%, var(--border-primary));
    box-shadow: 0 4px 16px color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 14%, transparent);
  }
}

.x-sender-disabled {
  background: var(--bg-secondary);
}

.x-sender-drop-mask {
  position: absolute;
  inset: 4px;
  z-index: 3;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border: 1px dashed var(--marvo-accent-color, #4f46e5);
  border-radius: 13px;
  background: color-mix(in srgb, var(--bg-card) 92%, transparent);
  color: var(--text-accent);
  pointer-events: none;
  font-size: var(--marvo-type-13);
  font-weight: 500;
}

.x-sender-header {
  padding: 12px 12px 0;
}

.x-sender-content {
  padding: 9px 11px 1px;
}

.x-sender-toolbar {
  min-height: 32px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 8px 8px;
}

.x-sender-prefix,
.x-sender-suffix {
  flex: none;
  display: inline-flex;
  align-items: center;
}

.x-sender-prefix {
  min-width: 0;
}
.x-sender-suffix {
  margin-inline-start: auto;
}

.x-sender-input {
  field-sizing: content;
  display: block;
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  min-height: 28px;
  max-height: 156px;
  resize: none;
  overflow-y: auto;
  padding: 2px 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  caret-color: var(--marvo-accent-color, #4f46e5);
  font: inherit;
  font-size: var(--marvo-type-14);
  line-height: 1.6;

  &::placeholder {
    color: var(--text-muted);
  }
  &:disabled {
    cursor: not-allowed;
    opacity: 0.62;
  }
}

.x-sender-action {
  width: 32px;
  height: 32px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  padding: 0;
  border: none;
  border-radius: 50%;
  color: var(--text-accent);
  background: transparent;
  cursor: pointer;
  font-size: var(--marvo-type-16);
  transition:
    transform 0.16s,
    background 0.16s,
    opacity 0.16s;

  &:hover:not(:disabled) {
    background: var(--bg-hover);
  }
  &:active:not(:disabled) {
    transform: scale(0.94);
  }
  &:disabled {
    cursor: not-allowed;
    opacity: 0.42;
  }
}

.x-sender-action-submit {
  background: var(--marvo-accent-color, #4f46e5);
  color: #fff;

  &:hover:not(:disabled) {
    background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 86%, #fff);
  }

  &:disabled {
    background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 42%, var(--bg-secondary));
    color: #fff;
  }
}

.x-sender-action-stop {
  position: relative;
  color: var(--text-accent);
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 7%, transparent);

  &::before {
    content: '';
    position: absolute;
    inset: 2px;
    border: 2px solid color-mix(in srgb, currentColor 20%, transparent);
    border-top-color: currentColor;
    border-radius: 50%;
    animation: x-sender-spin 0.85s linear infinite;
  }
}
.x-sender-stop-mark {
  position: relative;
  width: 10px;
  height: 10px;
  border-radius: 2px;
  background: currentColor;
}

.x-sender-spinner {
  width: 15px;
  height: 15px;
  border: 2px solid color-mix(in srgb, currentColor 24%, transparent);
  border-top-color: currentColor;
  border-radius: 50%;
  animation: x-sender-spin 0.8s linear infinite;
}

.x-sender-footer {
  padding: 0 12px 12px;
}

.x-sender-compact {
  border-radius: 14px;

  .x-sender-header {
    padding: 9px 9px 0;
  }
  .x-sender-content {
    padding: 7px 9px 0;
  }
  .x-sender-toolbar {
    min-height: 30px;
    gap: 6px;
    padding: 0 7px 7px;
  }
  .x-sender-input {
    min-height: 24px;
    max-height: 104px;
    font-size: var(--marvo-type-13);
  }
  .x-sender-action {
    width: 30px;
    height: 30px;
  }
}

@keyframes x-sender-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (hover: none), (max-width: 768px) {
  .x-sender-action,
  .x-sender-compact .x-sender-action {
    width: 40px;
    height: 40px;
  }

  .x-sender-toolbar,
  .x-sender-compact .x-sender-toolbar {
    min-height: 40px;
  }
}
</style>
