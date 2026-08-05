<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { VerticalAlignBottomOutlined } from '@ant-design/icons-vue'

const props = withDefaults(
  defineProps<{
    compact?: boolean
    autoScroll?: boolean
    working?: boolean
    scrollResetKey?: string | number | null
  }>(),
  {
    compact: false,
    autoScroll: true,
    working: false,
    scrollResetKey: null,
  },
)

const scrollBox = ref<HTMLDivElement | null>(null)
const content = ref<HTMLDivElement | null>(null)
const userScrolled = ref(false)
const showJumpToBottom = ref(false)
let observer: ResizeObserver | undefined
let autoScrollMarker: { top: number; time: number; smooth: boolean } | undefined
let autoMarkerTimer: ReturnType<typeof setTimeout> | undefined
let settleTimer: ReturnType<typeof setTimeout> | undefined
let touchY: number | undefined

const BOTTOM_THRESHOLD = 10
const AUTO_MARKER_TTL = 1_500

function maxScrollTop(box: HTMLElement) {
  return Math.max(0, box.scrollHeight - box.clientHeight)
}

function distanceFromBottom(box: HTMLElement) {
  return Math.max(0, maxScrollTop(box) - box.scrollTop)
}

function canScroll(box: HTMLElement) {
  return maxScrollTop(box) > 1
}

function clearAutoScrollMarker() {
  autoScrollMarker = undefined
  if (autoMarkerTimer) clearTimeout(autoMarkerTimer)
  autoMarkerTimer = undefined
}

function markAutoScroll(box: HTMLElement, smooth = false) {
  autoScrollMarker = { top: maxScrollTop(box), time: Date.now(), smooth }
  if (autoMarkerTimer) clearTimeout(autoMarkerTimer)
  autoMarkerTimer = setTimeout(clearAutoScrollMarker, AUTO_MARKER_TTL)
}

function isAutoScroll(box: HTMLElement) {
  const marker = autoScrollMarker
  if (!marker) return false
  if (Date.now() - marker.time > AUTO_MARKER_TTL) {
    clearAutoScrollMarker()
    return false
  }
  return marker.smooth || Math.abs(box.scrollTop - marker.top) < 2
}

function updateOverflowAnchor(box: HTMLElement) {
  box.style.overflowAnchor = userScrolled.value ? 'auto' : 'none'
}

function updateScrollState(box: HTMLElement) {
  const distance = distanceFromBottom(box)
  if (!canScroll(box) || distance <= BOTTOM_THRESHOLD) userScrolled.value = false
  showJumpToBottom.value = canScroll(box) && distance > Math.max(400, box.clientHeight)
  updateOverflowAnchor(box)
}

function pauseFollowing() {
  const box = scrollBox.value
  if (!box || !canScroll(box)) return
  clearAutoScrollMarker()
  userScrolled.value = true
  updateScrollState(box)
}

function handleScroll() {
  const box = scrollBox.value
  if (!box) return
  if (!canScroll(box) || distanceFromBottom(box) <= BOTTOM_THRESHOLD) {
    userScrolled.value = false
  } else if (!isAutoScroll(box)) {
    userScrolled.value = true
  }
  updateScrollState(box)
}

function scrollToBottom(behavior: ScrollBehavior = 'smooth', force = true) {
  const box = scrollBox.value
  if (!box || (!force && userScrolled.value) || !props.autoScroll) return
  if (force) userScrolled.value = false
  markAutoScroll(box, behavior === 'smooth')
  if (behavior === 'smooth') box.scrollTo({ top: box.scrollHeight, behavior })
  else box.scrollTop = box.scrollHeight
  updateScrollState(box)
}

function resumeFollowing(behavior: ScrollBehavior = 'auto') {
  userScrolled.value = false
  scrollToBottom(behavior, true)
}

function normalizedWheelDelta(event: WheelEvent, box: HTMLElement) {
  if (event.deltaMode === WheelEvent.DOM_DELTA_LINE) return event.deltaY * 40
  if (event.deltaMode === WheelEvent.DOM_DELTA_PAGE) return event.deltaY * box.clientHeight
  return event.deltaY
}

function nestedScrollable(target: EventTarget | null) {
  const box = scrollBox.value
  const element = target instanceof Element ? target.closest<HTMLElement>('[data-scrollable]') : null
  return element && element !== box ? element : null
}

function nestedConsumesDelta(target: EventTarget | null, delta: number) {
  const nested = nestedScrollable(target)
  if (!nested) return false
  const max = nested.scrollHeight - nested.clientHeight
  if (max <= 1) return false
  if (delta < 0) return nested.scrollTop > 0
  if (delta > 0) return max - nested.scrollTop > 1
  return true
}

function handleWheel(event: WheelEvent) {
  const box = scrollBox.value
  if (!box) return
  const delta = normalizedWheelDelta(event, box)
  if (!delta || nestedConsumesDelta(event.target, delta)) return
  clearAutoScrollMarker()
  if (delta < 0) pauseFollowing()
}

function handleTouchStart(event: TouchEvent) {
  clearAutoScrollMarker()
  touchY = event.touches[0]?.clientY
}

function handleTouchMove(event: TouchEvent) {
  const next = event.touches[0]?.clientY
  const previous = touchY
  touchY = next
  if (next === undefined || previous === undefined) return
  const delta = previous - next
  if (!delta || nestedConsumesDelta(event.target, delta)) return
  clearAutoScrollMarker()
  if (delta < 0) pauseFollowing()
}

function handleTouchEnd() {
  touchY = undefined
}

function handlePointerDown(event: PointerEvent) {
  if (nestedScrollable(event.target)) return
  clearAutoScrollMarker()
}

function handlePointerMove(event: PointerEvent) {
  if (event.buttons !== 1 || nestedScrollable(event.target)) return
  const selection = window.getSelection()
  if (selection?.toString()) pauseFollowing()
}

function handleInteraction() {
  if (window.getSelection()?.toString()) pauseFollowing()
}

function handleKeydown(event: KeyboardEvent) {
  if (event.altKey || event.ctrlKey || event.metaKey) return
  const upward =
    event.key === 'ArrowUp' || event.key === 'PageUp' || event.key === 'Home' || (event.key === ' ' && event.shiftKey)
  const downward = event.key === 'ArrowDown' || event.key === 'PageDown' || event.key === 'End' || event.key === ' '
  if (!upward && !downward) return
  clearAutoScrollMarker()
  if (upward) pauseFollowing()
}

watch(
  () => props.scrollResetKey,
  async () => {
    await nextTick()
    resumeFollowing('auto')
  },
  { flush: 'post' },
)

watch(
  () => props.working,
  (working, previous) => {
    if (settleTimer) clearTimeout(settleTimer)
    settleTimer = undefined
    if (working && !previous && !userScrolled.value) {
      resumeFollowing('auto')
      return
    }
    if (!working && previous) {
      settleTimer = setTimeout(() => {
        settleTimer = undefined
        if (!userScrolled.value) scrollToBottom('auto', false)
      }, 300)
    }
  },
)

onMounted(async () => {
  await nextTick()
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(() => {
      const box = scrollBox.value
      if (!box) return
      if (props.autoScroll && !userScrolled.value) scrollToBottom('auto', false)
      else updateScrollState(box)
    })
    if (content.value) observer.observe(content.value)
    if (scrollBox.value) observer.observe(scrollBox.value)
  }
  resumeFollowing('auto')
})

onBeforeUnmount(() => {
  observer?.disconnect()
  clearAutoScrollMarker()
  if (settleTimer) clearTimeout(settleTimer)
})

defineExpose({
  scrollToBottom: (behavior: ScrollBehavior = 'smooth') => resumeFollowing(behavior),
  nativeElement: scrollBox,
  userScrolled,
})
</script>

<template>
  <div :class="['x-bubble-list', { 'x-bubble-list-compact': compact }]">
    <div
      ref="scrollBox"
      class="x-bubble-list-scroll"
      data-scrollable
      tabindex="0"
      @scroll.passive="handleScroll"
      @wheel.passive="handleWheel"
      @touchstart.passive="handleTouchStart"
      @touchmove.passive="handleTouchMove"
      @touchend.passive="handleTouchEnd"
      @touchcancel.passive="handleTouchEnd"
      @pointerdown="handlePointerDown"
      @pointermove="handlePointerMove"
      @click="handleInteraction"
      @keydown="handleKeydown"
    >
      <div ref="content" class="x-bubble-list-content">
        <slot />
      </div>
    </div>

    <Transition name="x-bubble-list-jump">
      <button
        v-if="showJumpToBottom"
        class="x-bubble-list-jump"
        type="button"
        aria-label="回到底部"
        title="回到底部"
        @click="resumeFollowing('smooth')"
      >
        <VerticalAlignBottomOutlined aria-hidden="true" />
      </button>
    </Transition>
  </div>
</template>

<style lang="scss" scoped>
.x-bubble-list {
  position: relative;
  flex: 1;
  min-height: 0;
  width: 100%;
}

.x-bubble-list-scroll {
  width: 100%;
  height: 100%;
  overflow-y: auto;
  overflow-anchor: none;
  outline: none;

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 58%, transparent);
    outline-offset: -2px;
  }
}

.x-bubble-list-content {
  width: min(100%, 980px);
  min-height: 100%;
  display: flex;
  flex-direction: column;
  margin-inline: auto;
  padding: 18px 24px 28px;
  box-sizing: border-box;
}

.x-bubble-list-jump {
  position: absolute;
  z-index: 4;
  left: 50%;
  bottom: 10px;
  width: 32px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 1px solid var(--border-primary);
  border-radius: 9px;
  background: color-mix(in srgb, var(--bg-card) 92%, transparent);
  box-shadow: 0 4px 14px color-mix(in srgb, #000 14%, transparent);
  color: var(--text-secondary);
  cursor: pointer;
  transform: translateX(-50%);
  backdrop-filter: blur(5px);
  -webkit-tap-highlight-color: transparent;
  transition:
    color 0.16s ease,
    border-color 0.16s ease,
    background-color 0.16s ease;

  &:hover {
    border-color: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 42%, var(--border-primary));
    background: var(--bg-card);
    color: var(--text-accent);
  }

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 55%, transparent);
    outline-offset: 2px;
  }
}

.x-bubble-list-jump-enter-active,
.x-bubble-list-jump-leave-active {
  transition:
    opacity 0.16s ease,
    transform 0.16s ease;
}

.x-bubble-list-jump-enter-from,
.x-bubble-list-jump-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(6px) scale(0.88);
}

.x-bubble-list-compact .x-bubble-list-content {
  width: 100%;
  padding: 10px 14px 16px;
}

@media (hover: none) and (pointer: coarse) {
  .x-bubble-list-jump {
    width: 40px;
    height: 36px;
    bottom: 8px;
  }
}

@media (max-width: 768px) {
  .x-bubble-list-content {
    padding: 12px 16px 18px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .x-bubble-list-jump,
  .x-bubble-list-jump-enter-active,
  .x-bubble-list-jump-leave-active {
    transition-duration: 0ms;
  }
}
</style>
