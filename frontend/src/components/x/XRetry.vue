<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { LoadingOutlined } from '@ant-design/icons-vue'
import XButton from './XButton.vue'

interface RetryAction {
  reason: string
  provider: string
  title: string
  message: string
  label: string
  link?: string
}

const props = defineProps<{
  attempt: number
  message: string
  detail?: string
  action?: RetryAction
  next: number
  compact?: boolean
}>()

const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined

const remainingSeconds = computed(() => Math.max(0, Math.ceil((props.next - now.value) / 1000)))
const progressText = computed(() => (remainingSeconds.value > 0 ? `${remainingSeconds.value} 秒后继续` : '正在继续'))
const actionLink = computed(() => {
  const link = props.action?.link?.trim() || ''
  return /^https?:\/\//i.test(link) ? link : ''
})

function openAction() {
  if (!actionLink.value) return
  window.open(actionLink.value, '_blank', 'noopener,noreferrer')
}

onMounted(() => {
  timer = setInterval(() => {
    now.value = Date.now()
  }, 1_000)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <section
    :class="['x-retry', { 'x-retry-compact': compact }]"
    :title="detail || undefined"
    role="status"
    aria-live="polite"
  >
    <LoadingOutlined class="x-retry-icon" aria-hidden="true" />
    <div class="x-retry-copy">
      <strong>正在重试</strong>
      <span>{{ message }} · {{ progressText }} · 第 {{ Math.max(1, attempt) }} 次</span>
      <span v-if="action?.message" class="x-retry-action-message">{{ action.message }}</span>
      <XButton v-if="actionLink" class="x-retry-action" size="small" variant="secondary" @click="openAction">
        {{ action?.label || '查看处理方式' }}
      </XButton>
    </div>
  </section>
</template>

<style lang="scss" scoped>
.x-retry {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 9px;
  margin-bottom: 12px;
  padding: 10px 12px;
  border-radius: 8px;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);

  &::before {
    content: '';
    position: absolute;
    top: 8px;
    bottom: 8px;
    left: 0;
    width: 2px;
    border-radius: 2px;
    background: #d97706;
  }
}

.x-retry-action-message {
  color: var(--text-tertiary);
}

.x-retry-action {
  align-self: flex-start;
  margin-top: 3px;
}

.x-retry-icon {
  flex: none;
  margin-top: 2px;
  color: #d97706;
  font-size: var(--marvo-type-14);
  animation: x-retry-spin 0.9s linear infinite;
}

.x-retry-copy {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;

  strong {
    color: var(--text-primary);
    font-size: var(--marvo-type-13);
  }

  span {
    overflow-wrap: anywhere;
  }
}

.x-retry-compact {
  padding: 8px 10px;
  font-size: var(--marvo-type-11);

  .x-retry-copy strong {
    font-size: var(--marvo-type-12);
  }
}

@keyframes x-retry-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .x-retry-icon {
    animation-duration: 1.8s;
  }
}
</style>
