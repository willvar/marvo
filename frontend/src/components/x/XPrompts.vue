<script setup lang="ts">
import type { XPromptItem } from './types'

withDefaults(
  defineProps<{
    items: XPromptItem[]
    title?: string
    vertical?: boolean
    wrap?: boolean
    compact?: boolean
  }>(),
  {
    title: '',
    vertical: false,
    wrap: true,
    compact: false,
  },
)

const emit = defineEmits<{
  select: [item: XPromptItem]
}>()
</script>

<template>
  <section :class="['x-prompts', { 'x-prompts-compact': compact }]">
    <h5 v-if="title" class="x-prompts-title">{{ title }}</h5>
    <div :class="['x-prompts-list', { vertical, wrap }]">
      <button
        v-for="item in items"
        :key="item.key"
        class="x-prompts-item"
        type="button"
        :disabled="item.disabled"
        @click="emit('select', item)"
      >
        <span v-if="item.icon" class="x-prompts-icon"><component :is="item.icon" /></span>
        <span class="x-prompts-content">
          <strong>{{ item.label }}</strong>
          <small v-if="item.description">{{ item.description }}</small>
        </span>
      </button>
    </div>
  </section>
</template>

<style lang="scss" scoped>
.x-prompts {
  max-width: 100%;
}
.x-prompts-title {
  margin: 0 0 10px;
  color: var(--text-tertiary);
  font: inherit;
  font-size: var(--marvo-type-13);
  font-weight: 400;
}

.x-prompts-list {
  display: flex;
  gap: 10px;
  overflow-x: auto;
  align-items: stretch;

  &.wrap {
    flex-wrap: wrap;
  }
  &.vertical {
    flex-direction: column;
    align-items: stretch;
  }
}

.x-prompts-item {
  flex: 1 1 180px;
  min-width: 0;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 11px 14px;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: var(--bg-card);
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
  font: inherit;
  transition:
    border-color 0.2s,
    background 0.2s,
    transform 0.16s;

  &:hover:not(:disabled) {
    background: var(--bg-tertiary);
    border-color: transparent;
  }
  &:active:not(:disabled) {
    transform: scale(0.985);
  }
  &:disabled {
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.x-prompts-icon {
  flex: none;
  color: var(--text-accent);
  font-size: var(--marvo-type-15);
  line-height: 1.45;
}
.x-prompts-content {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.x-prompts-content strong {
  color: var(--text-primary);
  font-size: var(--marvo-type-13);
  font-weight: 500;
  line-height: 1.45;
}
.x-prompts-content small {
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
  line-height: 1.4;
}

.x-prompts-compact {
  .x-prompts-list {
    gap: 8px;
  }
  .x-prompts-item {
    flex-basis: 140px;
    padding: 9px 10px;
    border-radius: 10px;
  }
  .x-prompts-content strong {
    font-size: var(--marvo-type-12);
  }
  .x-prompts-content small {
    font-size: var(--marvo-type-11);
  }
}
</style>
