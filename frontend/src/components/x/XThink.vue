<script setup lang="ts">
import { computed, ref } from 'vue'
import { BulbOutlined, LoadingOutlined, RightOutlined } from '@ant-design/icons-vue'
import XTextReveal from './XTextReveal.vue'
import XTextShimmer from './XTextShimmer.vue'

const props = withDefaults(
  defineProps<{
    title?: string
    loadingTitle?: string
    loadingDetail?: string
    loading?: boolean
    defaultExpanded?: boolean
    hasContent?: boolean
    compact?: boolean
  }>(),
  {
    title: '思考过程',
    loadingTitle: '正在思考',
    loadingDetail: '',
    loading: false,
    defaultExpanded: false,
    hasContent: true,
    compact: false,
  },
)

const expanded = ref(props.defaultExpanded)
const loadingDetailText = computed(() => (props.loadingDetail ? ` · ${props.loadingDetail}` : ''))
</script>

<template>
  <section :class="['x-think', { 'x-think-compact': compact }]">
    <button
      class="x-think-status"
      type="button"
      :disabled="!hasContent"
      :aria-expanded="hasContent ? expanded : undefined"
      @click="expanded = !expanded"
    >
      <LoadingOutlined v-if="loading" class="x-think-loading" />
      <BulbOutlined v-else />
      <span class="x-think-label">
        <template v-if="loading">
          <XTextShimmer :text="loadingTitle" />
          <XTextReveal v-if="loadingDetailText" :text="loadingDetailText" :duration="700" :travel="8" />
        </template>
        <template v-else>{{ title }}</template>
      </span>
      <RightOutlined v-if="hasContent" :class="['x-think-arrow', { expanded }]" />
    </button>
    <div v-if="hasContent && expanded" class="x-think-content"><slot /></div>
  </section>
</template>

<style lang="scss" scoped>
.x-think {
  width: 100%;
  margin-bottom: 14px;
}

.x-think-status {
  width: fit-content;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font: inherit;
  font-size: var(--marvo-type-13);
  line-height: 1.5;
}
.x-think-status:disabled {
  cursor: default;
}

.x-think-arrow {
  font-size: var(--marvo-type-10);
  transition: transform 0.2s;
}
.x-think-arrow.expanded {
  transform: rotate(90deg);
}
.x-think-loading {
  color: var(--text-accent);
  animation: x-think-spin 0.9s linear infinite;
}

.x-think-label {
  display: inline-flex;
  min-width: 0;
  align-items: center;
}

.x-think-content {
  width: 100%;
  margin-top: 10px;
  padding-inline-start: 12px;
  border-inline-start: 2px solid var(--border-primary);
  color: var(--text-tertiary);
}

.x-think-compact {
  margin-bottom: 10px;
  .x-think-status {
    font-size: var(--marvo-type-12);
  }
}
@keyframes x-think-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .x-think-loading {
    animation: none;
  }
}
</style>
