<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { renderMarkdown } from '../../sdk'

const props = withDefaults(
  defineProps<{
    text: string
    streaming?: boolean
    openLinksInNewTab?: boolean
  }>(),
  {
    streaming: false,
    openLinksInNewTab: false,
  },
)

const FRAME_DELAY = 24
const IMMEDIATE_BACKLOG = 512
const PUNCTUATION_LOOKAHEAD = 12
const shown = ref(props.text)
let target = props.text
let timer: ReturnType<typeof setTimeout> | undefined

const reducedMotion = typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches

const html = computed(() => {
  try {
    return renderMarkdown(shown.value, { openLinksInNewTab: props.openLinksInNewTab })
  } catch {
    return escapeText(shown.value)
  }
})

function escapeText(value: string) {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;')
    .replace(/\n/g, '<br>')
}

function stopTimer() {
  if (timer) clearTimeout(timer)
  timer = undefined
}

function flush() {
  stopTimer()
  shown.value = target
}

function safeEnd(value: string, end: number) {
  const code = value.charCodeAt(end - 1)
  return code >= 0xd800 && code <= 0xdbff ? Math.min(value.length, end + 1) : end
}

function nextEnd(value: string, start: number) {
  const remaining = value.length - start
  const step = Math.min(24, Math.max(1, Math.ceil(remaining / 8)))
  let end = Math.min(value.length, start + step)
  const lookahead = value.slice(end, Math.min(value.length, end + PUNCTUATION_LOOKAHEAD))
  const punctuation = lookahead.search(/[。！？.!?\n，,；;：:]/)
  if (punctuation >= 0) end += punctuation + 1
  return safeEnd(value, end)
}

function tick() {
  timer = undefined
  if (!props.streaming || reducedMotion || !target.startsWith(shown.value)) {
    flush()
    return
  }
  const backlog = target.length - shown.value.length
  if (backlog <= 0) return
  if (backlog > IMMEDIATE_BACKLOG) {
    flush()
    return
  }
  shown.value = target.slice(0, nextEnd(target, shown.value.length))
  if (shown.value.length < target.length) timer = setTimeout(tick, FRAME_DELAY)
}

function schedule() {
  if (!timer) timer = setTimeout(tick, FRAME_DELAY)
}

watch(
  () => props.text,
  (value) => {
    target = value
    if (!props.streaming || reducedMotion || !value.startsWith(shown.value)) flush()
    else schedule()
  },
)

watch(
  () => props.streaming,
  (streaming) => {
    if (!streaming) flush()
    else schedule()
  },
)

onBeforeUnmount(stopTimer)
</script>

<template>
  <div class="x-markdown" v-html="html" />
</template>
