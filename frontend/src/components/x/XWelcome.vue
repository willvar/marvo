<script setup lang="ts">
import type { Component } from 'vue'

withDefaults(
  defineProps<{
    title: string
    description?: string
    icon?: Component
    variant?: 'filled' | 'borderless'
    compact?: boolean
  }>(),
  {
    description: '',
    icon: undefined,
    variant: 'borderless',
    compact: false,
  },
)
</script>

<template>
  <section :class="['x-welcome', `x-welcome-${variant}`, { 'x-welcome-compact': compact }]">
    <div v-if="icon || $slots.icon" class="x-welcome-icon">
      <slot name="icon"><component :is="icon" /></slot>
    </div>
    <div class="x-welcome-content">
      <div class="x-welcome-title-row">
        <h3>{{ title }}</h3>
        <div v-if="$slots.extra" class="x-welcome-extra"><slot name="extra" /></div>
      </div>
      <p v-if="description">{{ description }}</p>
      <slot />
    </div>
  </section>
</template>

<style lang="scss" scoped>
.x-welcome {
  width: 100%;
  display: flex;
  align-items: flex-start;
  gap: 16px;
  color: var(--text-primary);
}

.x-welcome-filled {
  padding: 14px 16px;
  border-radius: 12px;
  background: var(--bg-tertiary);
}
.x-welcome-icon {
  width: 46px;
  height: 46px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 14px;
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 12%, var(--bg-secondary));
  color: var(--text-accent);
  font-size: var(--marvo-type-23);
}
.x-welcome-content {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.x-welcome-title-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.x-welcome h3 {
  margin: 0;
  color: var(--text-primary);
  font-size: var(--marvo-type-20);
  line-height: 1.35;
  font-weight: 600;
}
.x-welcome p {
  margin: 0;
  color: var(--text-tertiary);
  font-size: var(--marvo-type-13);
  line-height: 1.5;
}
.x-welcome-extra {
  margin-inline-start: auto;
}

.x-welcome-compact {
  gap: 11px;
  .x-welcome-icon {
    width: 38px;
    height: 38px;
    border-radius: 11px;
    font-size: var(--marvo-type-19);
  }
  h3 {
    font-size: var(--marvo-type-16);
  }
  p {
    font-size: var(--marvo-type-12);
  }
}
</style>
