<script setup lang="ts">
withDefaults(
  defineProps<{
    label: string
    compact?: boolean
  }>(),
  {
    compact: false,
  },
)
</script>

<template>
  <div :class="['x-message-divider', { 'x-message-divider-compact': compact }]" role="separator" :aria-label="label">
    <span class="x-message-divider-line" aria-hidden="true" />
    <span class="x-message-divider-label" aria-hidden="true">{{ label }}</span>
    <span class="x-message-divider-line" aria-hidden="true" />
  </div>
</template>

<style lang="scss" scoped>
.x-message-divider {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  box-sizing: border-box;
  padding: 10px 0;
}

.x-message-divider-line {
  height: 1px;
  flex: 1 1 auto;
  background: var(--border-light);
}

.x-message-divider-label {
  flex: 0 0 auto;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
  line-height: 1.4;
  white-space: nowrap;
}

.x-message-divider-compact {
  gap: 9px;
  padding: 7px 0;

  .x-message-divider-label {
    font-size: var(--marvo-type-11);
  }
}
</style>
