<script setup lang="ts">
import { computed } from 'vue'
import {
  CloseCircleFilled,
  DeploymentUnitOutlined,
  LoadingOutlined,
  RightOutlined,
  StopOutlined,
} from '@ant-design/icons-vue'
import type { XSubtaskStatus } from './types'

const props = withDefaults(
  defineProps<{
    title: string
    description?: string
    status?: XSubtaskStatus
    background?: boolean
    clickable?: boolean
    compact?: boolean
  }>(),
  {
    description: '',
    status: 'default',
    background: false,
    clickable: false,
    compact: false,
  },
)

const emit = defineEmits<{ open: [] }>()

const statusLabel = computed(() => {
  if (props.status === 'running') return '执行中'
  if (props.status === 'retry') return '等待重试'
  if (props.status === 'error') return '未完成'
  if (props.status === 'stopped') return '已停止'
  return ''
})

function open() {
  if (props.clickable) emit('open')
}
</script>

<template>
  <component
    :is="clickable ? 'button' : 'div'"
    :type="clickable ? 'button' : undefined"
    :class="[
      'x-subtask-card',
      `x-subtask-card-${status}`,
      { 'x-subtask-card-clickable': clickable, 'x-subtask-card-compact': compact },
    ]"
    :aria-label="clickable ? `查看子任务：${description || title}` : undefined"
    @click="open"
  >
    <span class="x-subtask-card-icon" aria-hidden="true">
      <LoadingOutlined v-if="status === 'running' || status === 'retry'" class="x-subtask-card-spin" />
      <CloseCircleFilled v-else-if="status === 'error'" />
      <StopOutlined v-else-if="status === 'stopped'" />
      <DeploymentUnitOutlined v-else />
    </span>

    <span class="x-subtask-card-copy">
      <span class="x-subtask-card-heading">
        <span class="x-subtask-card-title">{{ title }}</span>
        <span v-if="background" class="x-subtask-card-badge">后台</span>
        <span v-if="statusLabel" class="x-subtask-card-status">{{ statusLabel }}</span>
      </span>
      <span v-if="description" class="x-subtask-card-description">{{ description }}</span>
    </span>

    <RightOutlined v-if="clickable" class="x-subtask-card-arrow" aria-hidden="true" />
  </component>
</template>

<style lang="scss" scoped>
.x-subtask-card {
  width: 100%;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  box-sizing: border-box;
  margin-bottom: 14px;
  padding: 10px 12px;
  border: 1px solid var(--border-light);
  border-radius: 9px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font: inherit;
  text-align: start;
  -webkit-tap-highlight-color: transparent;
}

.x-subtask-card-clickable {
  cursor: pointer;
  touch-action: manipulation;
  transition:
    border-color 0.16s ease,
    background 0.16s ease,
    transform 0.12s ease;

  &:hover {
    border-color: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 28%, var(--border-light));
    background: var(--bg-hover);
  }

  &:active {
    transform: scale(0.992);
  }

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 42%, transparent);
    outline-offset: 2px;
  }
}

.x-subtask-card-icon {
  width: 20px;
  height: 20px;
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--text-accent);
  font-size: var(--marvo-type-15);
}

.x-subtask-card-error .x-subtask-card-icon {
  color: var(--text-danger);
}

.x-subtask-card-stopped .x-subtask-card-icon {
  color: var(--text-muted);
}

.x-subtask-card-copy {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.x-subtask-card-heading {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 7px;
}

.x-subtask-card-title {
  min-width: 0;
  overflow: hidden;
  color: var(--text-primary);
  font-size: var(--marvo-type-13);
  font-weight: 600;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.x-subtask-card-badge,
.x-subtask-card-status {
  flex: none;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  font-weight: 400;
}

.x-subtask-card-description {
  overflow: hidden;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.x-subtask-card-arrow {
  flex: none;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}

.x-subtask-card-spin {
  animation: x-subtask-spin 0.9s linear infinite;
}

.x-subtask-card-compact {
  gap: 8px;
  margin-bottom: 10px;
  padding: 8px 10px;

  .x-subtask-card-title {
    font-size: var(--marvo-type-12);
  }

  .x-subtask-card-description,
  .x-subtask-card-badge,
  .x-subtask-card-status {
    font-size: var(--marvo-type-11);
  }
}

@keyframes x-subtask-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
