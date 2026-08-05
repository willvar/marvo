<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { CloseOutlined, ExclamationCircleFilled, FileOutlined, LoadingOutlined } from '@ant-design/icons-vue'
import type { XAttachmentItem } from './types'

const props = withDefaults(
  defineProps<{
    items: XAttachmentItem[]
    removable?: boolean
    disabled?: boolean
    compact?: boolean
    overflow?: 'scroll' | 'wrap'
  }>(),
  {
    removable: true,
    disabled: false,
    compact: false,
    overflow: 'scroll',
  },
)

const emit = defineEmits<{
  remove: [item: XAttachmentItem]
}>()

const objectUrls = ref(new Map<string, string>())

function isImage(item: XAttachmentItem) {
  return !!item.mime?.startsWith('image/')
}

function safeImageUrl(item: XAttachmentItem) {
  const source = item.url || objectUrls.value.get(item.key) || ''
  return /^(blob:|data:image\/|https?:\/\/)/i.test(source) ? source : ''
}

function syncObjectUrls() {
  const active = new Set(props.items.map((item) => item.key))
  for (const [key, url] of objectUrls.value) {
    if (active.has(key)) continue
    URL.revokeObjectURL(url)
    objectUrls.value.delete(key)
  }
  for (const item of props.items) {
    if (!isImage(item) || !item.file || item.url || objectUrls.value.has(item.key)) continue
    objectUrls.value.set(item.key, URL.createObjectURL(item.file))
  }
}

watch(() => props.items, syncObjectUrls, { immediate: true, deep: true })

onBeforeUnmount(() => {
  for (const url of objectUrls.value.values()) URL.revokeObjectURL(url)
  objectUrls.value.clear()
})

function formatSize(size?: number) {
  if (typeof size !== 'number') return ''
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(size < 10240 ? 1 : 0)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}
</script>

<template>
  <div
    v-if="items.length"
    :class="['x-attachments', `x-attachments-${overflow}`, { 'x-attachments-compact': compact }]"
  >
    <article
      v-for="item in items"
      :key="item.key"
      :class="['x-attachment-card', item.status && `x-attachment-${item.status}`]"
      :title="item.statusText || item.name"
    >
      <div class="x-attachment-preview" aria-hidden="true">
        <img v-if="isImage(item) && safeImageUrl(item)" :src="safeImageUrl(item)" alt="" />
        <FileOutlined v-else />
        <span v-if="item.status === 'preparing'" class="x-attachment-status"><LoadingOutlined spin /></span>
        <span v-else-if="item.status === 'error'" class="x-attachment-status"><ExclamationCircleFilled /></span>
      </div>
      <div class="x-attachment-info">
        <span class="x-attachment-name">{{ item.name }}</span>
        <span v-if="item.statusText || formatSize(item.size)" class="x-attachment-description">
          {{ item.statusText || formatSize(item.size) }}
        </span>
      </div>
      <button
        v-if="removable"
        class="x-attachment-remove"
        type="button"
        :disabled="disabled"
        :aria-label="`移除附件 ${item.name}`"
        :title="`移除 ${item.name}`"
        @click="emit('remove', item)"
      >
        <CloseOutlined />
      </button>
    </article>
  </div>
</template>

<style lang="scss" scoped>
.x-attachments {
  width: 100%;
  display: flex;
  gap: 8px;
  padding-bottom: 2px;
}

.x-attachments-scroll {
  overflow-x: auto;
}

.x-attachments-wrap {
  flex-wrap: wrap;
}

.x-attachment-card {
  position: relative;
  width: 224px;
  min-width: 224px;
  height: 64px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 30px 7px 7px;
  overflow: hidden;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-card);
  color: var(--text-primary);
  transition:
    border-color 0.16s,
    background 0.16s;

  &:hover {
    border-color: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 35%, var(--border-primary));
  }
}

.x-attachment-preview {
  position: relative;
  width: 48px;
  height: 48px;
  flex: none;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  background: var(--bg-tertiary);
  color: var(--text-tertiary);
  font-size: var(--marvo-type-20);

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
}

.x-attachment-status {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--bg-card) 78%, transparent);
  color: var(--text-accent);
  font-size: var(--marvo-type-16);
}

.x-attachment-error {
  border-color: color-mix(in srgb, var(--text-danger) 45%, var(--border-primary));

  .x-attachment-status,
  .x-attachment-description {
    color: var(--text-danger);
  }
}

.x-attachment-info {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.x-attachment-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--marvo-type-13);
  font-weight: 500;
}
.x-attachment-description {
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}

.x-attachment-remove {
  position: absolute;
  top: 5px;
  right: 5px;
  width: 22px;
  height: 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: color-mix(in srgb, var(--bg-card) 88%, transparent);
  color: var(--text-muted);
  cursor: pointer;
  opacity: 0;
  transition:
    opacity 0.16s,
    color 0.16s,
    background 0.16s;

  .x-attachment-card:hover &,
  &:focus-visible {
    opacity: 1;
  }
  &:hover:not(:disabled) {
    color: var(--text-primary);
    background: var(--bg-tertiary);
  }
  &:disabled {
    cursor: not-allowed;
    opacity: 0.35;
  }
}

.x-attachments-compact {
  .x-attachment-card {
    width: 190px;
    min-width: 190px;
    height: 54px;
    padding: 6px 28px 6px 6px;
  }
  .x-attachment-preview {
    width: 40px;
    height: 40px;
  }
  .x-attachment-name {
    font-size: var(--marvo-type-12);
  }
}

@media (hover: none), (max-width: 768px) {
  .x-attachment-remove {
    opacity: 1;
  }
}
</style>
