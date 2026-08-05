<script setup lang="ts">
import { CheckCircleOutlined, CloseCircleOutlined, WarningOutlined } from '@ant-design/icons-vue'
import type { AgentQuestionAnswerItem } from '../agentTimeline'

defineProps<{
  status: 'answered' | 'dismissed' | 'failed'
  items: AgentQuestionAnswerItem[]
  message?: string
  compact?: boolean
}>()
</script>

<template>
  <section :class="['x-question-summary', `x-question-summary-${status}`, { compact }]">
    <div class="x-question-summary-heading">
      <CheckCircleOutlined v-if="status === 'answered'" />
      <CloseCircleOutlined v-else-if="status === 'dismissed'" />
      <WarningOutlined v-else />
      <strong>{{ status === 'answered' ? '已回答' : status === 'dismissed' ? '已取消提问' : '提问未完成' }}</strong>
    </div>
    <div v-if="status === 'answered' && items.length" class="x-question-summary-items">
      <div v-for="(item, index) in items" :key="index" class="x-question-summary-item">
        <span>{{ item.question }}</span>
        <b>{{ item.answers.join('、') || '未填写' }}</b>
      </div>
    </div>
    <p v-else-if="message">{{ message }}</p>
  </section>
</template>

<style lang="scss" scoped>
.x-question-summary {
  margin-bottom: 12px;
  padding: 9px 12px;
  border: 1px solid var(--border-light);
  border-radius: 10px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
}

.x-question-summary:last-child {
  margin-bottom: 0;
}

.x-question-summary-heading {
  display: flex;
  align-items: center;
  gap: 7px;

  > :first-child {
    color: var(--text-accent);
  }

  strong {
    color: var(--text-primary);
  }
}

.x-question-summary-dismissed .x-question-summary-heading > :first-child {
  color: var(--text-muted);
}

.x-question-summary-failed .x-question-summary-heading > :first-child {
  color: var(--text-danger);
}

.x-question-summary-items {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 8px;
}

.x-question-summary > p {
  margin: 8px 0 0;
}

.x-question-summary-item {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;

  span {
    min-width: 0;
    color: var(--text-muted);
    overflow-wrap: break-word;
  }

  b {
    color: var(--text-primary);
    font-weight: 500;
    overflow-wrap: break-word;
  }
}

.compact {
  margin-bottom: 10px;
  padding: 8px 10px;
  font-size: var(--marvo-type-11);
}

.compact:last-child {
  margin-bottom: 0;
}
</style>
