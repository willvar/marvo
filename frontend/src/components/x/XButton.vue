<script setup lang="ts">
withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
    size?: 'small' | 'normal'
    disabled?: boolean
    type?: 'button' | 'submit'
  }>(),
  {
    variant: 'secondary',
    size: 'normal',
    disabled: false,
    type: 'button',
  },
)
</script>

<template>
  <button :class="['x-button', `x-button-${variant}`, `x-button-${size}`]" :type="type" :disabled="disabled">
    <slot />
  </button>
</template>

<style lang="scss" scoped>
.x-button {
  min-width: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1px solid transparent;
  border-radius: 8px;
  outline: none;
  cursor: pointer;
  touch-action: manipulation;
  font: inherit;
  font-size: var(--marvo-type-12);
  font-weight: 500;
  transition:
    background 0.16s,
    border-color 0.16s,
    color 0.16s,
    transform 0.12s;

  &:active:not(:disabled) {
    transform: scale(0.97);
  }

  &:focus-visible {
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--marvo-accent-color) 24%, transparent);
  }

  &:disabled {
    cursor: default;
    opacity: 0.52;
  }
}

.x-button-normal {
  min-height: 34px;
  padding: 5px 12px;
}

.x-button-small {
  min-height: 30px;
  padding: 4px 10px;
  font-size: var(--marvo-type-11);
}

.x-button-primary {
  background: var(--marvo-accent-color);
  color: #fff;

  &:hover:not(:disabled) {
    background: color-mix(in srgb, var(--marvo-accent-color) 88%, #000);
  }
}

.x-button-secondary {
  border-color: var(--border-primary);
  background: var(--bg-card);
  color: var(--text-primary);

  &:hover:not(:disabled) {
    background: var(--bg-hover);
  }
}

.x-button-ghost {
  background: transparent;
  color: var(--text-secondary);

  &:hover:not(:disabled) {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
}

.x-button-danger {
  border-color: color-mix(in srgb, var(--text-danger) 32%, transparent);
  background: transparent;
  color: var(--text-danger);

  &:hover:not(:disabled) {
    background: color-mix(in srgb, var(--text-danger) 8%, transparent);
  }
}

@media (hover: none), (max-width: 768px) {
  .x-button-normal {
    min-height: 44px;
  }

  .x-button-small {
    min-height: 40px;
    padding-inline: 11px;
  }
}
</style>
