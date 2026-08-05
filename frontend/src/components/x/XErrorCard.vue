<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string
    message: string
    detail?: string
    variant?: 'error' | 'warning' | 'info'
    compact?: boolean
  }>(),
  {
    title: '',
    detail: '',
    variant: 'error',
    compact: false,
  },
)
</script>

<template>
  <section
    :class="['x-error-card', `x-error-card-${variant}`, { 'x-error-card-compact': compact }]"
    :title="detail || undefined"
    :role="variant === 'error' ? 'alert' : 'status'"
  >
    <strong v-if="title">{{ title }}</strong>
    <span>{{ message }}</span>
    <slot />
  </section>
</template>

<style lang="scss" scoped>
.x-error-card {
  --x-error-accent: var(--text-danger);
  position: relative;
  max-height: 240px;
  display: flex;
  flex-direction: column;
  gap: 3px;
  overflow-y: auto;
  margin-bottom: 12px;
  padding: 8px 12px 8px 14px;
  border-radius: 8px;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--marvo-type-13);
  white-space: pre-wrap;
  overflow-wrap: anywhere;

  &::before {
    content: '';
    position: absolute;
    top: 8px;
    bottom: 8px;
    left: 0;
    width: 2px;
    border-radius: 2px;
    background: var(--x-error-accent);
  }

  strong {
    color: var(--text-primary);
  }
}

.x-error-card-warning {
  --x-error-accent: #d97706;
}

.x-error-card-info {
  --x-error-accent: var(--marvo-accent-color);
}

.x-error-card-compact {
  max-height: 180px;
  padding-block: 6px;
  font-size: var(--marvo-type-12);
}
</style>
