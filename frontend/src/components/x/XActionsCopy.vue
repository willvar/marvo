<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { CheckOutlined, CopyOutlined, ExclamationCircleOutlined } from '@ant-design/icons-vue'

const props = withDefaults(
  defineProps<{
    text: string
    compact?: boolean
    disabled?: boolean
  }>(),
  {
    compact: false,
    disabled: false,
  },
)

type CopyState = 'idle' | 'copied' | 'error'

const state = ref<CopyState>('idle')
let resetTimer: ReturnType<typeof setTimeout> | undefined

const labels: Record<CopyState, string> = {
  idle: '复制',
  copied: '已复制',
  error: '复制失败',
}

function clearResetTimer() {
  if (!resetTimer) return
  clearTimeout(resetTimer)
  resetTimer = undefined
}

function resetLater() {
  clearResetTimer()
  resetTimer = setTimeout(() => {
    state.value = 'idle'
    resetTimer = undefined
  }, 2_000)
}

async function copy() {
  if (props.disabled || !props.text) return
  const copied = await writeClipboard(props.text)
  state.value = copied ? 'copied' : 'error'
  resetLater()
}

async function writeClipboard(text: string) {
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // LAN deployments commonly run on HTTP, where the Clipboard API may be unavailable.
    }
  }
  return legacyCopy(text)
}

function legacyCopy(text: string) {
  if (typeof document === 'undefined' || typeof document.execCommand !== 'function') return false
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.readOnly = true
  textarea.setAttribute('aria-hidden', 'true')
  textarea.style.position = 'fixed'
  textarea.style.inset = '0 auto auto -9999px'
  textarea.style.opacity = '0'
  textarea.style.pointerEvents = 'none'

  const selection = document.getSelection()
  const ranges = selection
    ? Array.from({ length: selection.rangeCount }, (_, index) => selection.getRangeAt(index))
    : []
  document.body.appendChild(textarea)
  textarea.focus({ preventScroll: true })
  textarea.select()
  textarea.setSelectionRange(0, text.length)

  let copied = false
  try {
    copied = document.execCommand('copy')
  } catch {
    copied = false
  } finally {
    textarea.remove()
    if (selection && ranges.length > 0) {
      selection.removeAllRanges()
      for (const range of ranges) selection.addRange(range)
    }
  }
  return copied
}

watch(
  () => props.text,
  () => {
    clearResetTimer()
    state.value = 'idle'
  },
)

onBeforeUnmount(clearResetTimer)
</script>

<template>
  <button
    type="button"
    :class="['x-actions-copy', { 'x-actions-copy-compact': compact }]"
    :data-state="state"
    :disabled="disabled || !text"
    :aria-label="labels[state]"
    @click="copy"
  >
    <CheckOutlined v-if="state === 'copied'" />
    <ExclamationCircleOutlined v-else-if="state === 'error'" />
    <CopyOutlined v-else />
    <span aria-live="polite">{{ labels[state] }}</span>
  </button>
</template>

<style lang="scss" scoped>
.x-actions-copy {
  min-height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 4px 8px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--text-muted);
  font: inherit;
  font-size: var(--marvo-type-12);
  line-height: 1;
  cursor: pointer;
  transition:
    color 0.15s ease,
    background-color 0.15s ease;

  &:hover {
    background: var(--bg-hover);
    color: var(--text-secondary);
  }

  &:focus-visible {
    outline: 2px solid var(--text-accent);
    outline-offset: 1px;
  }

  &[data-state='copied'] {
    color: var(--text-accent);
  }

  &[data-state='error'] {
    color: var(--text-danger);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.x-actions-copy-compact {
  min-height: 28px;
  padding-inline: 6px;
  font-size: var(--marvo-type-11);
}

@media (hover: none) and (pointer: coarse) {
  .x-actions-copy {
    min-height: 40px;
    padding-inline: 10px;
  }
}
</style>
