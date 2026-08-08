<script setup lang="ts">
import { renderMarkdown } from '../sdk'
import { computed } from 'vue'

const props = defineProps<{ content: string; title?: string }>()
const html = computed(() => renderMarkdown(props.content, { title: props.title }))
</script>

<template>
  <div class="note-preview" v-html="html" />
</template>

<style lang="scss">
@use '../styles/tiptap' as *;

.note-preview {
  padding: 24px 32px;
  font-size: var(--marvo-content-font-size, var(--marvo-type-15));
  line-height: var(--marvo-content-line-height, 1.8);
  color: var(--text-primary);
  max-width: var(--marvo-content-width, none);
  margin: 0 auto;
  @include tiptap-styles;
}

@media (max-width: 600px) {
  .note-preview {
    padding: 18px 16px max(28px, env(safe-area-inset-bottom));
  }
}
</style>
