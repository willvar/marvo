<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  CheckCircleFilled,
  CloseCircleFilled,
  ExclamationCircleFilled,
  LoadingOutlined,
  RightOutlined,
  StopOutlined,
} from '@ant-design/icons-vue'
import type { XThoughtItem } from './types'

const props = withDefaults(
  defineProps<{
    items: XThoughtItem[]
    compact?: boolean
    defaultExpandedKeys?: string[]
    expandedKeys?: string[]
    line?: boolean | 'solid' | 'dashed' | 'dotted'
    nested?: boolean
  }>(),
  {
    compact: false,
    defaultExpandedKeys: () => [],
    expandedKeys: undefined,
    line: 'solid',
    nested: false,
  },
)

const emit = defineEmits<{
  'update:expandedKeys': [keys: string[]]
  expand: [keys: string[]]
}>()

const innerExpandedKeys = ref([...props.defaultExpandedKeys])
const currentExpandedKeys = computed(() => props.expandedKeys ?? innerExpandedKeys.value)

function isCollapsible(item: XThoughtItem) {
  return item.collapsible === true && !!item.children?.length
}

function isExpanded(item: XThoughtItem) {
  return isCollapsible(item) && currentExpandedKeys.value.includes(item.key)
}

function toggle(item: XThoughtItem) {
  if (!isCollapsible(item)) return
  const keys = currentExpandedKeys.value
  const next = keys.includes(item.key) ? keys.filter((key) => key !== item.key) : [...keys, item.key]
  if (props.expandedKeys === undefined) innerExpandedKeys.value = next
  emit('update:expandedKeys', next)
  emit('expand', next)
}
</script>

<template>
  <div
    :class="[
      'x-thought-chain',
      `x-thought-chain-line-${line === true ? 'solid' : line || 'none'}`,
      { 'x-thought-chain-compact': compact, 'x-thought-chain-nested': nested },
    ]"
  >
    <div
      v-for="(item, index) in items"
      :key="item.key"
      :class="['x-thought-node', { 'x-thought-node-expanded': isExpanded(item) }]"
    >
      <div
        :class="[
          'x-thought-node-icon',
          `x-thought-node-${item.status || 'default'}`,
          { 'x-thought-node-icon-connected': line !== false && (index < items.length - 1 || isExpanded(item)) },
        ]"
      >
        <LoadingOutlined v-if="item.status === 'loading'" class="x-thought-spin" />
        <CheckCircleFilled v-else-if="item.status === 'success'" />
        <ExclamationCircleFilled v-else-if="item.status === 'warning'" />
        <CloseCircleFilled v-else-if="item.status === 'error'" />
        <StopOutlined v-else-if="item.status === 'stopped'" />
        <span v-else>{{ index + 1 }}</span>
      </div>
      <div class="x-thought-node-main">
        <button
          v-if="isCollapsible(item)"
          class="x-thought-node-header x-thought-node-trigger"
          type="button"
          :aria-expanded="isExpanded(item)"
          @click="toggle(item)"
        >
          <span class="x-thought-node-copy">
            <span class="x-thought-node-title">{{ item.title }}</span>
            <span v-if="item.description" class="x-thought-node-description">{{ item.description }}</span>
          </span>
          <RightOutlined :class="['x-thought-node-arrow', { expanded: isExpanded(item) }]" />
        </button>
        <div v-else class="x-thought-node-header">
          <span class="x-thought-node-title">{{ item.title }}</span>
          <span v-if="item.description" class="x-thought-node-description">{{ item.description }}</span>
        </div>

        <Transition name="x-thought-collapse">
          <div v-if="isExpanded(item)" class="x-thought-node-content">
            <XThoughtChain v-if="item.children?.length" :items="item.children" :compact="compact" :line="line" nested />
          </div>
        </Transition>
      </div>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.x-thought-chain {
  width: 100%;
  display: flex;
  flex-direction: column;
  margin-bottom: 14px;
}

.x-thought-chain-nested {
  margin-bottom: 0;
}

.x-thought-node {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-height: 40px;
}

.x-thought-node-icon {
  position: relative;
  width: 17px;
  height: 17px;
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-top: 2px;
  border-radius: 50%;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--marvo-type-10);
}

.x-thought-node-icon-connected::after {
  content: '';
  position: absolute;
  z-index: -1;
  top: 19px;
  bottom: -21px;
  left: 8px;
  border-inline-start: 1px solid var(--border-primary);
}
.x-thought-node-expanded > .x-thought-node-icon::after {
  bottom: -9px;
}
.x-thought-chain-line-dashed .x-thought-node-icon::after {
  border-inline-start-style: dashed;
}
.x-thought-chain-line-dotted .x-thought-node-icon::after {
  border-inline-start-style: dotted;
}

.x-thought-node-loading {
  color: var(--text-accent);
}
.x-thought-node-success {
  color: #16a34a;
  background: transparent;
}
.x-thought-node-warning {
  color: #d97706;
  background: transparent;
}
.x-thought-node-error {
  color: var(--text-danger);
  background: transparent;
}
.x-thought-node-stopped {
  color: var(--text-muted);
  background: transparent;
}
.x-thought-spin {
  animation: x-thought-spin 0.9s linear infinite;
}

.x-thought-node-main {
  min-width: 0;
  flex: 1;
  padding-bottom: 12px;
}
.x-thought-node-header {
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}
.x-thought-node-trigger {
  width: 100%;
  flex-direction: row;
  align-items: flex-start;
  gap: 10px;
  padding: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: start;
  -webkit-tap-highlight-color: transparent;
}
.x-thought-node-trigger:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 55%, transparent);
  outline-offset: 3px;
}
.x-thought-node-copy {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}
.x-thought-node-title {
  color: var(--text-primary);
  font-size: var(--marvo-type-13);
  font-weight: 500;
  line-height: 1.5;
}
.x-thought-node-description {
  margin-top: 1px;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
  overflow-wrap: anywhere;
}
.x-thought-node-arrow {
  flex: none;
  margin-top: 5px;
  color: var(--text-muted);
  font-size: var(--marvo-type-10);
  transition: transform 0.2s ease;
}
.x-thought-node-arrow.expanded {
  transform: rotate(90deg);
}

.x-thought-node-content {
  margin-top: 12px;
  padding-inline-start: 2px;
}

.x-thought-collapse-enter-active,
.x-thought-collapse-leave-active {
  transition:
    opacity 0.16s ease,
    transform 0.16s ease;
}
.x-thought-collapse-enter-from,
.x-thought-collapse-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.x-thought-chain-compact {
  margin-bottom: 10px;

  &.x-thought-chain-nested {
    margin-bottom: 0;
  }
  .x-thought-node {
    gap: 8px;
    min-height: 34px;
  }
  .x-thought-node-main {
    padding-bottom: 8px;
  }
  .x-thought-node-title {
    font-size: var(--marvo-type-12);
  }
  .x-thought-node-description {
    font-size: var(--marvo-type-11);
  }
  .x-thought-node-content {
    margin-top: 9px;
  }
}

@keyframes x-thought-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
