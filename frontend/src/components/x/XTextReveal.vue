<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    text?: string
    duration?: number
    travel?: number
    growOnly?: boolean
  }>(),
  {
    text: '',
    duration: 450,
    travel: 0,
    growOnly: true,
  },
)

const root = ref<HTMLElement>()
const entering = ref<HTMLElement>()
const leaving = ref<HTMLElement>()
const current = ref(props.text)
const previous = ref('')
const width = ref('auto')
const ready = ref(false)
const swapping = ref(false)
let frame: number | undefined

const style = computed(() => ({
  '--x-text-reveal-duration': `${props.duration}ms`,
  '--x-text-reveal-travel': `${props.travel}px`,
}))

function measuredWidth(element?: HTMLElement) {
  return element?.scrollWidth || 0
}

function widen(next: number) {
  if (next <= 0) return
  if (props.growOnly) {
    const old = Number.parseFloat(width.value)
    if (Number.isFinite(old) && next <= old) return
  }
  width.value = `${next}px`
}

function cancelFrame() {
  if (frame === undefined) return
  cancelAnimationFrame(frame)
  frame = undefined
}

watch(
  () => props.text,
  async (next, old) => {
    if (next === old) return
    if (next.startsWith(old)) {
      current.value = next
      await nextTick()
      widen(measuredWidth(entering.value))
      return
    }

    swapping.value = true
    previous.value = old
    current.value = next
    await nextTick()
    cancelFrame()
    frame = requestAnimationFrame(() => {
      widen(Math.max(measuredWidth(entering.value), measuredWidth(leaving.value)))
      void root.value?.offsetHeight
      swapping.value = false
      frame = undefined
    })
  },
)

onMounted(() => {
  widen(measuredWidth(entering.value))
  const markReady = () => {
    widen(measuredWidth(entering.value))
    requestAnimationFrame(() => {
      ready.value = true
    })
  }
  if (document.fonts) void document.fonts.ready.finally(markReady)
  else markReady()
})

onBeforeUnmount(cancelFrame)
</script>

<template>
  <span
    ref="root"
    class="x-text-reveal"
    :data-ready="ready ? 'true' : 'false'"
    :data-swapping="swapping ? 'true' : 'false'"
    :aria-label="text"
    :style="style"
  >
    <span class="x-text-reveal-track" :style="{ width }">
      <span ref="entering" class="x-text-reveal-entering" aria-hidden="true">{{ current || '\u00a0' }}</span>
      <span ref="leaving" class="x-text-reveal-leaving" aria-hidden="true">{{ previous || '\u00a0' }}</span>
    </span>
  </span>
</template>

<style lang="scss" scoped>
.x-text-reveal {
  --x-text-reveal-duration: 450ms;
  --x-text-reveal-travel: 0px;
  --x-text-reveal-edge: 17%;
  --x-text-reveal-spring: cubic-bezier(0.34, 1.08, 0.64, 1);
  --x-text-reveal-spring-soft: cubic-bezier(0.34, 1, 0.64, 1);

  display: inline-flex;
  min-width: 0;
  align-items: center;
  overflow: visible;
}

.x-text-reveal-track {
  display: grid;
  min-height: 1.5em;
  align-items: center;
  justify-items: start;
  overflow: visible;
  line-height: 1.5;
  transition: width var(--x-text-reveal-duration) var(--x-text-reveal-spring-soft);
}

.x-text-reveal-entering,
.x-text-reveal-leaving {
  grid-area: 1 / 1;
  justify-self: start;
  white-space: nowrap;
  line-height: 1.5;
  text-align: start;
  mask-size: 100% 300%;
  mask-repeat: no-repeat;
  transition-duration: var(--x-text-reveal-duration);
  transition-timing-function: var(--x-text-reveal-spring);
  -webkit-mask-size: 100% 300%;
  -webkit-mask-repeat: no-repeat;
}

.x-text-reveal-entering {
  mask-image: linear-gradient(to top, #fff 33%, transparent calc(33% + var(--x-text-reveal-edge)));
  mask-position: 0 100%;
  transition-property:
    mask-position,
    -webkit-mask-position,
    transform;
  transform: translateY(0);
  -webkit-mask-image: linear-gradient(to top, #fff 33%, transparent calc(33% + var(--x-text-reveal-edge)));
  -webkit-mask-position: 0 100%;
}

.x-text-reveal-leaving {
  mask-image: linear-gradient(to bottom, #fff 33%, transparent calc(33% + var(--x-text-reveal-edge)));
  mask-position: 0 100%;
  transition-property:
    mask-position,
    -webkit-mask-position,
    transform;
  transform: translateY(var(--x-text-reveal-travel));
  -webkit-mask-image: linear-gradient(to bottom, #fff 33%, transparent calc(33% + var(--x-text-reveal-edge)));
  -webkit-mask-position: 0 100%;
}

.x-text-reveal[data-swapping='true'] .x-text-reveal-entering {
  mask-position: 0 0;
  transition-duration: 0ms !important;
  transform: translateY(calc(var(--x-text-reveal-travel) * -1));
  -webkit-mask-position: 0 0;
}

.x-text-reveal[data-swapping='true'] .x-text-reveal-leaving {
  mask-position: 0 0;
  transition-duration: 0ms !important;
  transform: translateY(0);
  -webkit-mask-position: 0 0;
}

.x-text-reveal[data-ready='false'] .x-text-reveal-track,
.x-text-reveal[data-ready='false'] .x-text-reveal-entering,
.x-text-reveal[data-ready='false'] .x-text-reveal-leaving {
  transition-duration: 0ms !important;
}

@media (prefers-reduced-motion: reduce) {
  .x-text-reveal-track,
  .x-text-reveal-entering,
  .x-text-reveal-leaving {
    transition-duration: 0ms !important;
  }
}
</style>
