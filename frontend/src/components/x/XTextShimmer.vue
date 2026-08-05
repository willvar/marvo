<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    text?: string
    active?: boolean
    offset?: number
  }>(),
  {
    text: '',
    active: true,
    offset: 0,
  },
)

const characters = computed(() => Array.from(props.text))

function characterStyle(index: number) {
  return { '--x-text-shimmer-delay': `${-320 + (index + props.offset) * 95}ms` }
}
</script>

<template>
  <span class="x-text-shimmer" :data-active="active ? 'true' : 'false'" :aria-label="text">
    <span
      v-for="(character, index) in characters"
      :key="`${index}-${character}`"
      class="x-text-shimmer-char"
      :style="characterStyle(index)"
      aria-hidden="true"
      >{{ character }}</span
    >
  </span>
</template>

<style lang="scss" scoped>
.x-text-shimmer {
  display: inline-flex;
  align-items: baseline;
  color: var(--text-tertiary);
  font: inherit;
  letter-spacing: inherit;
  line-height: inherit;
  user-select: none;
  white-space: pre;
}

.x-text-shimmer-char {
  --x-text-shimmer-delay: 0ms;
  display: inline-block;
  color: var(--text-tertiary);
  font: inherit;
  letter-spacing: inherit;
  line-height: inherit;
  opacity: 0.68;
  transform: translateY(0);
}

.x-text-shimmer[data-active='true'] .x-text-shimmer-char {
  animation: x-text-shimmer-wave 1.35s cubic-bezier(0.4, 0, 0.2, 1) infinite;
  animation-delay: var(--x-text-shimmer-delay);
  will-change: color, opacity, transform, text-shadow;
}

@keyframes x-text-shimmer-wave {
  0%,
  58%,
  100% {
    color: var(--text-tertiary);
    opacity: 0.68;
    text-shadow: none;
    transform: translateY(0);
  }
  24% {
    color: var(--text-accent);
    opacity: 1;
    text-shadow: 0 0 0.55em color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 52%, transparent);
    transform: translateY(-0.08em);
  }
  38% {
    color: var(--text-primary);
    opacity: 0.94;
    text-shadow: 0 0 0.25em color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 22%, transparent);
    transform: translateY(-0.025em);
  }
}

@media (prefers-reduced-motion: reduce) {
  .x-text-shimmer[data-active='true'] .x-text-shimmer-char {
    animation: none;
    color: var(--text-secondary);
    opacity: 1;
    text-shadow: none;
    transform: none;
  }
}
</style>
