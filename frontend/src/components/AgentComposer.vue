<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { FileUpload, useFileUpload, type FileUploadFileChangeDetails } from '@ark-ui/vue/file-upload'
import { PaperClipOutlined, PictureOutlined } from '@ant-design/icons-vue'
import { loadAgentDraft, saveAgentDraft, type AgentFilePartInput } from '../sdk'
import { XAttachments, XSender, type XAttachmentItem } from './x'

const props = withDefaults(
  defineProps<{
    sending?: boolean
    stopping?: boolean
    blocked?: boolean
    disabled?: boolean
    compact?: boolean
    draftKey?: string | null
    placeholder?: string
    submitMessage: (text: string, files: AgentFilePartInput[]) => Promise<void>
    stopMessage?: () => Promise<void> | void
  }>(),
  {
    sending: false,
    stopping: false,
    blocked: false,
    disabled: false,
    compact: false,
    draftKey: null,
    placeholder: '输入消息...',
    stopMessage: undefined,
  },
)

const emit = defineEmits<{
  error: [message: string]
}>()

const input = ref('')
const attachmentFiles = ref<File[]>([])
const preparing = ref(false)
const preparingKey = ref('')
const attachmentFailures = ref<Record<string, string>>({})
const selectionNotice = ref('')
const uploadId = `agent-composer-${useId().replace(/:/g, '-')}`
let active = true

onBeforeUnmount(() => {
  active = false
})

const busy = computed(() => props.sending || props.stopping || preparing.value)
const canSend = computed(
  () => !props.blocked && !props.disabled && !busy.value && (!!input.value.trim() || attachmentFiles.value.length > 0),
)
const resolvedPlaceholder = computed(() =>
  props.blocked
    ? '请先回应智能体的请求...'
    : props.disabled
      ? '正在加载对话...'
      : props.stopping
        ? '正在停止…'
        : props.sending
          ? '智能体正在处理，可点击停止'
          : preparing.value
            ? '正在读取附件...'
            : props.placeholder,
)
const attachmentItems = computed<XAttachmentItem[]>(() =>
  attachmentFiles.value.map((file) => {
    const key = fileKey(file)
    const failure = attachmentFailures.value[key]
    return {
      key,
      name: file.name,
      size: file.size,
      mime: file.type || 'application/octet-stream',
      file,
      status: failure ? 'error' : preparingKey.value === key ? 'preparing' : 'ready',
      statusText: failure || (preparingKey.value === key ? '正在准备…' : undefined),
    }
  }),
)

watch(
  () => props.draftKey,
  (key) => {
    input.value = loadAgentDraft(key)
  },
  { immediate: true },
)

watch(input, (value) => saveAgentDraft(props.draftKey, value))

function fileKey(file: File) {
  return `${file.name}-${file.size}-${file.lastModified}-${file.type}`
}

function sameFile(left: File, right: File) {
  return left.name === right.name && left.size === right.size && left.type === right.type
}

function addFiles(files: File[]) {
  if (files.length === 0 || busy.value || props.blocked || props.disabled) return
  const next = [...attachmentFiles.value]
  let duplicateCount = 0
  for (const file of files) {
    if (next.some((existing) => sameFile(existing, file))) {
      duplicateCount++
      continue
    }
    next.push(file)
  }
  attachmentFiles.value = next
  selectionNotice.value = duplicateCount > 0 ? `已忽略 ${duplicateCount} 个重复附件` : ''
}

function removeAttachment(item: XAttachmentItem) {
  if (busy.value || props.disabled) return
  attachmentFiles.value = attachmentFiles.value.filter((file) => fileKey(file) !== item.key)
  const failures = { ...attachmentFailures.value }
  delete failures[item.key]
  attachmentFailures.value = failures
  selectionNotice.value = ''
}

const imagePicker = useFileUpload(
  computed(() => ({
    id: `${uploadId}-images`,
    accept: 'image/*' as const,
    maxFiles: Number.MAX_SAFE_INTEGER,
    disabled: busy.value || props.blocked || props.disabled,
    onFileChange: (details: FileUploadFileChangeDetails) => {
      if (details.acceptedFiles.length === 0) return
      addFiles(details.acceptedFiles)
      imagePicker.value.clearFiles()
    },
    onFileReject: () => emit('error', '只能添加图片文件'),
  })),
)

const filePicker = useFileUpload(
  computed(() => ({
    id: `${uploadId}-files`,
    maxFiles: Number.MAX_SAFE_INTEGER,
    disabled: busy.value || props.blocked || props.disabled,
    onFileChange: (details: FileUploadFileChangeDetails) => {
      if (details.acceptedFiles.length === 0) return
      addFiles(details.acceptedFiles)
      filePicker.value.clearFiles()
    },
    onFileReject: () => emit('error', '无法添加附件'),
  })),
)

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      if (typeof reader.result !== 'string') {
        reject(new Error('无法读取附件'))
        return
      }
      const mime = file.type || 'application/octet-stream'
      resolve(file.type ? reader.result : reader.result.replace(/^data:;/, `data:${mime};`))
    }
    reader.onerror = () => reject(new Error('无法读取附件'))
    reader.onabort = () => reject(new Error('附件读取已取消'))
    reader.readAsDataURL(file)
  })
}

async function toFilePart(file: File): Promise<AgentFilePartInput> {
  return {
    type: 'file',
    mime: file.type || 'application/octet-stream',
    filename: file.name,
    url: await readFileAsDataURL(file),
  }
}

async function submit() {
  const text = input.value.trim()
  const files = [...attachmentFiles.value]
  if ((!text && files.length === 0) || busy.value || props.blocked || props.disabled) return

  preparing.value = true
  try {
    const fileParts: AgentFilePartInput[] = []
    for (const file of files) {
      const key = fileKey(file)
      preparingKey.value = key
      const failures = { ...attachmentFailures.value }
      delete failures[key]
      attachmentFailures.value = failures
      try {
        fileParts.push(await toFilePart(file))
      } catch (cause) {
        const message = cause instanceof Error ? cause.message : '无法读取附件'
        attachmentFailures.value = { ...attachmentFailures.value, [key]: message }
        throw new Error(`无法读取「${file.name}」：${message}`)
      }
      if (!active) return
    }
    if (!active) return
    await props.submitMessage(text, fileParts)
    clear()
  } catch (cause) {
    emit('error', cause instanceof Error ? cause.message : '发送失败，智能体服务可能不可用')
  } finally {
    preparingKey.value = ''
    preparing.value = false
  }
}

async function stop() {
  try {
    await props.stopMessage?.()
  } catch (cause) {
    emit('error', cause instanceof Error ? cause.message : '停止失败，请稍后重试')
  }
}

function clear() {
  input.value = ''
  saveAgentDraft(props.draftKey, '')
  imagePicker.value.clearFiles()
  filePicker.value.clearFiles()
  attachmentFiles.value = []
  attachmentFailures.value = {}
  preparingKey.value = ''
  selectionNotice.value = ''
}

defineExpose({ clear })
</script>

<template>
  <div :class="['agent-composer', { 'agent-composer-compact': compact }]">
    <XSender
      :model-value="input"
      :placeholder="resolvedPlaceholder"
      :disabled="busy || blocked || disabled"
      :loading="sending && !stopping"
      :stopping="stopping"
      :submitting="preparing"
      :submit-disabled="!canSend"
      :compact="compact"
      @update:model-value="input = $event"
      @submit="submit"
      @cancel="stop"
      @paste-files="addFiles"
    >
      <template v-if="attachmentItems.length || selectionNotice" #header>
        <XAttachments
          v-if="attachmentItems.length"
          :items="attachmentItems"
          :disabled="busy || disabled"
          :compact="compact"
          @remove="removeAttachment"
        />
        <p v-if="selectionNotice" class="agent-composer-notice" role="status">{{ selectionNotice }}</p>
      </template>

      <template #prefix>
        <div class="agent-composer-upload-actions">
          <FileUpload.RootProvider :value="imagePicker" class="agent-composer-picker">
            <FileUpload.HiddenInput />
            <FileUpload.Trigger as-child>
              <button
                class="agent-composer-upload-trigger"
                type="button"
                aria-label="添加图片"
                title="添加图片（移动端可拍照或选择相册）"
              >
                <PictureOutlined />
                <span class="agent-composer-upload-label">图片</span>
              </button>
            </FileUpload.Trigger>
          </FileUpload.RootProvider>

          <FileUpload.RootProvider :value="filePicker" class="agent-composer-picker">
            <FileUpload.HiddenInput />
            <FileUpload.Trigger as-child>
              <button class="agent-composer-upload-trigger" type="button" aria-label="添加附件" title="添加附件">
                <PaperClipOutlined />
                <span class="agent-composer-upload-label">附件</span>
              </button>
            </FileUpload.Trigger>
          </FileUpload.RootProvider>
        </div>
      </template>
    </XSender>
  </div>
</template>

<style lang="scss" scoped>
.agent-composer {
  width: 100%;
}

.agent-composer :deep([data-scope='file-upload'][data-part='root']) {
  display: inline-flex;
}

.agent-composer-upload-actions {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}

.agent-composer-notice {
  margin: 2px 2px 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}

.agent-composer-picker {
  display: inline-flex;
}

.agent-composer-upload-trigger {
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  gap: 5px;
  padding: 0 7px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text-tertiary);
  cursor: pointer;
  font: inherit;
  font-size: var(--marvo-type-12);
  transition:
    background 0.16s,
    color 0.16s;

  > :first-child {
    font-size: var(--marvo-type-15);
  }
  &:hover:not(:disabled) {
    color: var(--text-primary);
    background: var(--bg-hover);
  }
  &:disabled {
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.agent-composer-compact .agent-composer-upload-trigger {
  width: 30px;
  padding: 0;

  .agent-composer-upload-label {
    display: none;
  }
}

@media (hover: none), (max-width: 768px) {
  .agent-composer-upload-trigger,
  .agent-composer-compact .agent-composer-upload-trigger {
    width: auto;
    min-width: 40px;
    height: 40px;
    padding-inline: 8px;
  }

  .agent-composer-compact .agent-composer-upload-trigger {
    padding-inline: 0;
  }
}
</style>
