<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { Menu, type MenuSelectionDetails } from '@ark-ui/vue/menu'
import {
  ExclamationCircleFilled,
  LoadingOutlined,
  PlusOutlined,
  WarningFilled,
  EllipsisOutlined,
} from '@ant-design/icons-vue'
import type { XConversationAction, XConversationItem } from './types'

const props = withDefaults(
  defineProps<{
    items: XConversationItem[]
    activeKey?: string | null
    actions?: XConversationAction[]
    creationLabel?: string
    creationDisabled?: boolean
    loading?: boolean
    teleportMenus?: boolean
  }>(),
  {
    activeKey: null,
    actions: () => [],
    creationLabel: '新对话',
    creationDisabled: false,
    loading: false,
    teleportMenus: true,
  },
)

const emit = defineEmits<{
  create: []
  activeChange: [key: string]
  action: [actionKey: string, item: XConversationItem]
}>()
const listElement = ref<HTMLUListElement | null>(null)

async function revealActiveItem() {
  await nextTick()
  const list = listElement.value
  if (!list || !props.activeKey) return
  const item = Array.from(list.children).find(
    (element) => element instanceof HTMLElement && element.dataset.key === props.activeKey,
  )
  if (!(item instanceof HTMLElement)) return
  const listRect = list.getBoundingClientRect()
  const itemRect = item.getBoundingClientRect()
  if (itemRect.top < listRect.top) list.scrollTop -= listRect.top - itemRect.top
  else if (itemRect.bottom > listRect.bottom) list.scrollTop += itemRect.bottom - listRect.bottom
}

watch(
  [() => props.activeKey, () => props.items, () => props.loading],
  () => {
    void revealActiveItem()
  },
  { immediate: true, flush: 'post' },
)

function selectAction(item: XConversationItem, details: MenuSelectionDetails) {
  emit('action', details.value, item)
}
</script>

<template>
  <nav class="x-conversations" aria-label="智能体对话">
    <button
      class="x-conversations-creation"
      type="button"
      :aria-label="creationLabel"
      :disabled="creationDisabled || loading"
      @click="emit('create')"
    >
      <PlusOutlined />
      <span>{{ creationLabel }}</span>
    </button>

    <div v-if="loading && items.length === 0" class="x-conversations-loading" aria-label="正在加载会话">
      <span v-for="index in 4" :key="index" />
    </div>
    <ul v-else ref="listElement" class="x-conversations-list" :aria-busy="loading">
      <li
        v-for="item in items"
        :key="item.key"
        :class="['x-conversations-item', { active: activeKey === item.key, disabled: item.disabled }]"
        :title="item.label"
        :data-key="item.key"
        @click="!item.disabled && activeKey !== item.key && emit('activeChange', item.key)"
      >
        <div class="x-conversations-label">
          <slot name="label" :item="item">{{ item.label }}</slot>
        </div>

        <span
          v-if="item.status"
          :class="['x-conversations-status', `x-conversations-status-${item.status}`]"
          :title="item.statusLabel"
          :aria-label="item.statusLabel"
          role="status"
        >
          <LoadingOutlined v-if="item.status === 'running' || item.status === 'retry'" spin />
          <WarningFilled v-else-if="item.status === 'attention'" />
          <ExclamationCircleFilled v-else />
        </span>

        <Menu.Root v-if="!item.disabled && actions.length" @select="selectAction(item, $event)">
          <Menu.Trigger as-child>
            <button class="x-conversations-more" type="button" title="更多操作" aria-label="更多操作" @click.stop>
              <EllipsisOutlined />
            </button>
          </Menu.Trigger>
          <Teleport to="body" :disabled="!teleportMenus">
            <Menu.Positioner class="x-conversations-menu-positioner">
              <Menu.Content class="x-conversations-menu">
                <Menu.Item
                  v-for="action in actions"
                  :key="action.key"
                  :value="action.key"
                  :class="['x-conversations-menu-item', { danger: action.danger }]"
                >
                  <component :is="action.icon" v-if="action.icon" />
                  <Menu.ItemText>{{ action.label }}</Menu.ItemText>
                </Menu.Item>
              </Menu.Content>
            </Menu.Positioner>
          </Teleport>
        </Menu.Root>
      </li>
      <li v-if="items.length === 0" class="x-conversations-empty"><slot name="empty">暂无对话</slot></li>
    </ul>
  </nav>
</template>

<style lang="scss" scoped>
.x-conversations {
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: calc((var(--dsh-pane-toolbar-height, 52px) - var(--dsh-pane-control-height, 40px)) / 2)
    var(--dsh-pane-gutter, 12px) var(--dsh-pane-gutter, 12px);
}

.x-conversations-creation {
  height: var(--dsh-pane-control-height, 40px);
  min-height: var(--dsh-pane-control-height, 40px);
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 8px;
  margin-bottom: 10px;
  padding: 0 12px;
  border: 1px solid color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 22%, transparent);
  border-radius: 10px;
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 15%, transparent);
  color: var(--text-accent);
  cursor: pointer;
  touch-action: manipulation;
  font: inherit;
  font-size: var(--marvo-type-13);
  font-weight: 500;
  transition: background 0.2s;

  &:hover:not(:disabled) {
    background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 23%, transparent);
  }
  &:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }
}

.x-conversations-list {
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  overflow-y: auto;
  margin: 0;
  padding: 0;
  list-style: none;
}

.x-conversations-item {
  min-height: var(--dsh-pane-control-height, 40px);
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 12px;
  border-radius: 10px;
  color: var(--text-secondary);
  cursor: pointer;
  touch-action: manipulation;
  transition:
    background 0.18s,
    color 0.18s;

  &:hover:not(.disabled),
  &.active {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
  &.disabled {
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.x-conversations-label {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--marvo-type-13);
}

.x-conversations-status {
  width: 16px;
  height: 16px;
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--text-tertiary);
  font-size: var(--marvo-type-12);
}

.x-conversations-status-attention,
.x-conversations-status-retry {
  color: #d97706;
}

.x-conversations-status-running {
  color: var(--text-accent);
}

.x-conversations-status-error {
  color: var(--text-danger);
}

.x-conversations-more {
  width: 28px;
  height: 28px;
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: none;
  border-radius: 7px;
  background: transparent;
  color: var(--text-tertiary);
  cursor: pointer;
  opacity: 0;
  font-size: var(--marvo-type-17);

  .x-conversations-item:hover &,
  .x-conversations-item.active &,
  &:focus-visible {
    opacity: 0.7;
  }
  &:hover {
    opacity: 1;
    background: var(--bg-tertiary);
  }
}

.x-conversations-empty {
  padding: 28px 12px;
  color: var(--text-muted);
  text-align: center;
  font-size: var(--marvo-type-13);
}
.x-conversations-loading {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.x-conversations-loading span {
  height: 36px;
  border-radius: 9px;
  background: var(--bg-tertiary);
  animation: x-conversations-pulse 1.4s ease-in-out infinite;
}

@media (hover: none), (max-width: 768px) {
  .x-conversations-creation,
  .x-conversations-item {
    min-height: 44px;
  }
  .x-conversations-more {
    width: 40px;
    height: 40px;
    opacity: 0.7;
  }

  :global(.x-conversations-menu-item) {
    min-height: 44px;
  }
}

@keyframes x-conversations-pulse {
  50% {
    opacity: 0.45;
  }
}

:global(.x-conversations-menu-positioner) {
  z-index: 120;
}
:global(.x-conversations-menu) {
  min-width: 132px;
  padding: 4px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  outline: none;
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
}
:global(.x-conversations-menu-item) {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
  padding: 0 10px;
  border-radius: 7px;
  outline: none;
  color: var(--text-primary);
  cursor: pointer;
  font-size: var(--marvo-type-13);
}
:global(.x-conversations-menu-item[data-highlighted]) {
  background: var(--bg-hover);
}
:global(.x-conversations-menu-item.danger) {
  color: var(--text-danger);
}
</style>
