<script setup lang="ts">
withDefaults(
  defineProps<{
    placement?: 'start' | 'end'
    variant?: 'filled' | 'outlined' | 'shadow' | 'borderless'
    loading?: boolean
    compact?: boolean
    time?: string
  }>(),
  {
    placement: 'start',
    variant: 'filled',
    loading: false,
    compact: false,
    time: '',
  },
)
</script>

<template>
  <article :class="['x-bubble', `x-bubble-${placement}`, { 'x-bubble-compact': compact }]">
    <div v-if="$slots.avatar" class="x-bubble-avatar"><slot name="avatar" /></div>
    <div class="x-bubble-body">
      <div v-if="$slots.header || time" class="x-bubble-header">
        <slot name="header" />
        <time v-if="time">{{ time }}</time>
      </div>
      <div :class="['x-bubble-content', `x-bubble-content-${variant}`]">
        <div v-if="loading" class="x-bubble-loading" aria-label="思考中"><span /><span /><span /></div>
        <slot v-else />
      </div>
      <div v-if="$slots.footer" class="x-bubble-footer"><slot name="footer" /></div>
    </div>
    <div v-if="$slots.extra" class="x-bubble-extra"><slot name="extra" /></div>
  </article>
</template>

<style lang="scss" scoped>
.x-bubble {
  width: 100%;
  display: flex;
  align-items: flex-start;
  column-gap: 12px;
  padding-block: 10px;
  box-sizing: border-box;
}

.x-bubble-start {
  align-self: flex-start;
  padding-inline-end: 15%;
}
.x-bubble-end {
  align-self: flex-end;
  flex-direction: row-reverse;
  padding-inline-start: 15%;
}

.x-bubble-avatar {
  width: 32px;
  height: 32px;
  min-width: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  border-radius: 50%;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--marvo-type-15);
}

.x-bubble-body {
  min-width: 0;
  max-width: 100%;
  display: flex;
  flex-direction: column;
}
.x-bubble-end .x-bubble-body {
  align-items: flex-end;
}

.x-bubble-header {
  display: flex;
  margin-bottom: 4px;
  color: var(--text-secondary);
  font-size: var(--marvo-type-13);
}
.x-bubble-end .x-bubble-header {
  justify-content: flex-end;
}
.x-bubble-header time {
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  line-height: 1.3;
}

.x-bubble-content {
  position: relative;
  box-sizing: border-box;
  min-width: 0;
  max-width: 100%;
  min-height: 42px;
  padding: 10px 16px;
  border-radius: 16px;
  color: var(--text-primary);
  font-size: var(--marvo-type-14);
  line-height: 1.65;
  overflow-wrap: anywhere;
}

.x-bubble-content-filled {
  background: var(--bg-tertiary);
}
.x-bubble-content-outlined {
  border: 1px solid var(--border-primary);
  background: var(--bg-card);
}
.x-bubble-content-shadow {
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
}
.x-bubble-content-borderless {
  min-height: 0;
  padding: 0;
  border-radius: 0;
  background: transparent;
}

.x-bubble-end .x-bubble-content-filled {
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 10%, var(--bg-tertiary));
}

.x-bubble-footer {
  display: flex;
  margin-top: 6px;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  line-height: 1.3;
}

.x-bubble-end .x-bubble-footer {
  flex-direction: row-reverse;
}
.x-bubble-extra {
  flex: none;
}

.x-bubble-loading {
  height: 22px;
  display: flex;
  align-items: center;
  gap: 7px;

  span {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--marvo-accent-color, #4f46e5);
    animation: x-bubble-dot 1.8s linear infinite;
  }
  span:nth-child(2) {
    animation-delay: 0.18s;
  }
  span:nth-child(3) {
    animation-delay: 0.36s;
  }
}

.x-bubble-compact {
  column-gap: 8px;
  padding-block: 7px;

  &.x-bubble-start {
    padding-inline-end: 5%;
  }
  &.x-bubble-end {
    padding-inline-start: 5%;
  }
  .x-bubble-avatar {
    width: 26px;
    height: 26px;
    min-width: 26px;
    font-size: var(--marvo-type-12);
  }
  .x-bubble-content {
    min-height: 36px;
    padding: 8px 12px;
    border-radius: 14px;
    font-size: var(--marvo-type-13);
    line-height: 1.55;
  }
  .x-bubble-content-borderless {
    min-height: 0;
    padding: 0;
  }
  .x-bubble-header time {
    font-size: var(--marvo-type-10);
  }
  .x-bubble-footer {
    font-size: var(--marvo-type-10);
  }
}

@keyframes x-bubble-dot {
  0%,
  40%,
  100% {
    transform: translateY(0);
  }
  10% {
    transform: translateY(4px);
  }
  20% {
    transform: translateY(-4px);
  }
}

@media (max-width: 768px) {
  .x-bubble-start {
    padding-inline-end: 5%;
  }
  .x-bubble-end {
    padding-inline-start: 5%;
  }
}
</style>
