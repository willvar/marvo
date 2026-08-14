<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Field } from '@ark-ui/vue/field'
import { FileUpload } from '@ark-ui/vue/file-upload'
import { Checkbox } from '@ark-ui/vue/checkbox'
import {
  AndroidOutlined,
  CheckOutlined,
  CheckCircleOutlined,
  CloseOutlined,
  CloudUploadOutlined,
  DownloadOutlined,
  FileZipOutlined,
  LoadingOutlined,
} from '@ant-design/icons-vue'
import { api, ApiError } from '../../sdk'
import { useRouter } from 'vue-router'
import { XFullscreenTextarea } from '../../components/x'

interface AndroidRelease {
  version_code: number
  version_name: string
  required: boolean
  message: string
}

const maximumAPKBytes = 256 * 1024 * 1024
const router = useRouter()
const current = ref<AndroidRelease | null>(null)
const loading = ref(true)
const loadError = ref('')
const files = ref<File[]>([])
const message = ref('')
const required = ref(false)
const publishing = ref(false)
const publishError = ref('')
const published = ref(false)

const selectedAPK = computed(() => files.value[0] ?? null)
const canPublish = computed(() => !!selectedAPK.value && !publishing.value)

onMounted(loadRelease)

async function loadRelease() {
  loading.value = true
  loadError.value = ''
  try {
    const { data } = await api.get('/api/admin/android/release')
    current.value = data
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      current.value = null
    } else if (error instanceof ApiError && error.status === 401) {
      await router.replace('/admin/login')
    } else {
      loadError.value = 'Android 版本信息加载失败，请稍后重试'
    }
  } finally {
    loading.value = false
  }
}

function updateFiles(next: File[]) {
  files.value = next
  published.value = false
  publishError.value = ''
}

function rejectAPK() {
  published.value = false
  publishError.value = '请选择不超过 256 MB 的 APK 文件'
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '-'
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`
  return `${Math.ceil(value / 1024)} KB`
}

async function publish() {
  const apk = selectedAPK.value
  if (!apk || publishing.value) return
  publishing.value = true
  publishError.value = ''
  published.value = false
  try {
    const form = new FormData()
    form.append('apk', apk, apk.name)
    form.append('message', message.value.trim())
    form.append('required', String(required.value))
    const { data } = await api.put('/api/admin/android/release', form)
    current.value = data.release
    files.value = []
    message.value = ''
    required.value = false
    published.value = true
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      await router.replace('/admin/login')
      return
    }
    if (error instanceof ApiError && error.status === 409) {
      publishError.value = '版本代码必须高于当前已发布版本'
    } else if (error instanceof ApiError && error.status === 413) {
      publishError.value = 'APK 超过 256 MB，无法发布'
    } else if (error instanceof ApiError && error.status === 400) {
      publishError.value = '文件不是有效的 Marvo 正式 APK'
    } else {
      publishError.value = '发布失败，请稍后重试'
    }
  } finally {
    publishing.value = false
  }
}
</script>

<template>
  <section class="android-release-page">
    <div class="android-release-heading">
      <div>
        <h1>Android APP</h1>
        <p>发布所有用户共用的 Android 安装包；APP 内置前端，并在首次启动时绑定用户空间。</p>
      </div>
      <a v-if="current" class="admin-btn" href="/api/app/android/apk" download>
        <DownloadOutlined aria-hidden="true" />下载当前版本
      </a>
    </div>

    <div class="android-release-grid">
      <section class="android-release-card">
        <div class="android-release-card-title">
          <AndroidOutlined aria-hidden="true" />
          <div>
            <h2>当前版本</h2>
            <p>用户前台和已安装 APP 都从这里获取最新版本。</p>
          </div>
        </div>
        <div v-if="loading" class="android-release-loading"><span class="page-loading-spinner" /></div>
        <p v-else-if="loadError" class="login-error" role="alert">{{ loadError }}</p>
        <div v-else-if="current" class="android-release-current">
          <div class="android-release-version">
            <strong>{{ current.version_name }}</strong>
            <span>版本代码 {{ current.version_code }}</span>
          </div>
          <span :class="['android-release-policy', { required: current.required }]">
            {{ current.required ? '强制更新' : '可稍后更新' }}
          </span>
          <p v-if="current.message" class="android-release-message">{{ current.message }}</p>
          <p v-else class="android-release-message is-empty">没有更新说明</p>
        </div>
        <div v-else class="android-release-empty">
          <FileZipOutlined aria-hidden="true" />
          <strong>尚未发布 Android APP</strong>
          <span>上传首个正式 APK 后，用户前台才会开放下载。</span>
        </div>
      </section>

      <section class="android-release-card">
        <div class="android-release-card-title">
          <CloudUploadOutlined aria-hidden="true" />
          <div>
            <h2>发布新版本</h2>
            <p>版本号从 APK 自动读取，版本代码必须高于当前版本。</p>
          </div>
        </div>

        <form class="android-release-form" @submit.prevent="publish">
          <FileUpload.Root
            :accepted-files="files"
            :max-files="1"
            :max-file-size="maximumAPKBytes"
            accept="application/vnd.android.package-archive,.apk"
            :disabled="publishing"
            @file-reject="rejectAPK"
            @update:accepted-files="updateFiles"
          >
            <FileUpload.Dropzone class="android-release-dropzone">
              <FileUpload.HiddenInput />
              <CloudUploadOutlined aria-hidden="true" />
              <strong>{{ selectedAPK ? '已选择安装包' : '拖入 APK 或从设备选择' }}</strong>
              <span>仅接受 Marvo 通用 APK，最大 256 MB</span>
              <FileUpload.Trigger class="admin-btn" type="button" :disabled="publishing">
                <FileZipOutlined aria-hidden="true" />{{ selectedAPK ? '重新选择' : '选择 APK' }}
              </FileUpload.Trigger>
            </FileUpload.Dropzone>
            <FileUpload.ItemGroup v-if="selectedAPK" class="android-release-file-list">
              <FileUpload.Item :file="selectedAPK" class="android-release-file">
                <FileZipOutlined aria-hidden="true" />
                <div>
                  <FileUpload.ItemName />
                  <span>{{ formatBytes(selectedAPK.size) }}</span>
                </div>
                <FileUpload.ItemDeleteTrigger class="android-release-file-remove" aria-label="移除安装包">
                  <CloseOutlined />
                </FileUpload.ItemDeleteTrigger>
              </FileUpload.Item>
            </FileUpload.ItemGroup>
          </FileUpload.Root>

          <Field.Root class="android-release-notes-field">
            <Field.Label class="android-release-notes-label">更新说明</Field.Label>
            <XFullscreenTextarea
              v-model="message"
              title="全屏编辑更新说明"
              class="admin-textarea android-release-notes"
              maxlength="2000"
              placeholder="向用户说明本次更新内容（可选）"
              :disabled="publishing"
            />
          </Field.Root>

          <Checkbox.Root
            class="android-release-required"
            :checked="required"
            :disabled="publishing"
            @update:checked="required = $event === true"
          >
            <Checkbox.HiddenInput />
            <Checkbox.Control class="android-release-required-control">
              <Checkbox.Indicator class="android-release-required-indicator">
                <CheckOutlined />
              </Checkbox.Indicator>
            </Checkbox.Control>
            <Checkbox.Label class="android-release-required-label">
              <strong>要求旧版本立即更新</strong>
              <small>启用后，检测到更高版本的 APP 将不能跳过更新。</small>
            </Checkbox.Label>
          </Checkbox.Root>

          <p v-if="publishError" class="login-error" role="alert">{{ publishError }}</p>
          <p v-if="published" class="android-release-success" role="status">
            <CheckCircleOutlined aria-hidden="true" />新版本已发布
          </p>

          <div class="android-release-actions">
            <button class="admin-btn admin-btn-primary" type="submit" :disabled="!canPublish">
              <LoadingOutlined v-if="publishing" class="admin-workspace-entry-loading" aria-hidden="true" />
              <CloudUploadOutlined v-else aria-hidden="true" />
              {{ publishing ? '发布中...' : '发布 APK' }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped lang="scss">
.android-release-page {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.android-release-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  h1 {
    margin: 0 0 6px;
    color: var(--text-primary);
    font-size: var(--marvo-type-22);
  }
  p {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-13);
  }
}

.android-release-grid {
  display: grid;
  grid-template-columns: minmax(280px, 0.8fr) minmax(420px, 1.2fr);
  gap: 20px;
  align-items: start;
}

.android-release-card {
  padding: 22px;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: var(--bg-primary);
}

.android-release-card-title {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  > svg {
    width: 20px;
    height: 20px;
    margin-top: 2px;
    color: var(--text-accent);
  }
  h2 {
    margin: 0 0 4px;
    color: var(--text-primary);
    font-size: var(--marvo-type-16);
  }
  p {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-12);
    line-height: 1.5;
  }
}

.android-release-loading {
  min-height: 180px;
  display: grid;
  place-items: center;
}

.android-release-current {
  margin-top: 24px;
}

.android-release-version {
  display: flex;
  align-items: baseline;
  gap: 10px;
  strong {
    color: var(--text-primary);
    font-size: var(--marvo-type-28);
  }
  span {
    color: var(--text-muted);
    font-size: var(--marvo-type-12);
  }
}

.android-release-policy {
  display: inline-flex;
  margin-top: 12px;
  padding: 4px 9px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--text-accent) 10%, transparent);
  color: var(--text-accent);
  font-size: var(--marvo-type-12);
  &.required {
    background: color-mix(in srgb, var(--text-danger) 10%, transparent);
    color: var(--text-danger);
  }
}

.android-release-message {
  margin: 18px 0 0;
  padding-top: 16px;
  border-top: 1px solid var(--border-light);
  color: var(--text-secondary);
  font-size: var(--marvo-type-13);
  line-height: 1.7;
  white-space: pre-wrap;
  &.is-empty {
    color: var(--text-muted);
  }
}

.android-release-empty {
  min-height: 180px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--text-muted);
  text-align: center;
  svg {
    width: 34px;
    height: 34px;
  }
  strong {
    color: var(--text-secondary);
    font-size: var(--marvo-type-14);
  }
  span {
    max-width: 300px;
    font-size: var(--marvo-type-12);
    line-height: 1.5;
  }
}

.android-release-form {
  display: flex;
  flex-direction: column;
  gap: 18px;
  margin-top: 22px;
}

.android-release-dropzone {
  min-height: 180px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 22px;
  border: 1px dashed var(--border-secondary);
  border-radius: 10px;
  background: var(--bg-secondary);
  color: var(--text-tertiary);
  text-align: center;
  &[data-dragging] {
    border-color: var(--text-accent);
    background: color-mix(in srgb, var(--text-accent) 7%, var(--bg-secondary));
  }
  > svg {
    width: 30px;
    height: 30px;
    color: var(--text-accent);
  }
  strong {
    color: var(--text-primary);
    font-size: var(--marvo-type-14);
  }
  span {
    margin-bottom: 5px;
    font-size: var(--marvo-type-12);
  }
}

.android-release-file-list {
  margin: 10px 0 0;
  padding: 0;
  list-style: none;
}

.android-release-file {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--border-light);
  border-radius: 8px;
  color: var(--text-secondary);
  > div {
    min-width: 0;
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  [data-part='item-name'] {
    overflow: hidden;
    color: var(--text-primary);
    font-size: var(--marvo-type-13);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  span {
    color: var(--text-muted);
    font-size: var(--marvo-type-11);
  }
}

.android-release-file-remove {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-tertiary);
  cursor: pointer;
  &:hover {
    background: var(--bg-hover);
    color: var(--text-danger);
  }
}

.android-release-notes-field {
  min-width: 0;
}

.android-release-notes-label {
  display: inline-block;
  margin-bottom: 8px;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  font-weight: 600;
}

.android-release-required {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  color: var(--text-primary);
  cursor: pointer;
  &[data-disabled] {
    cursor: not-allowed;
    opacity: 0.6;
  }
}

.android-release-required-control {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  margin-top: 1px;
  border: 1px solid var(--border-secondary);
  border-radius: 5px;
  background: var(--bg-primary);
  color: #fff;
  &[data-state='checked'] {
    border-color: var(--marvo-accent-color, #4f46e5);
    background: var(--marvo-accent-color, #4f46e5);
  }
}

.android-release-required-indicator {
  width: 100%;
  height: 100%;
  display: grid;
  place-items: center;
  font-size: 11px;
  line-height: 1;
  &[hidden] {
    display: none;
  }
}

.android-release-required-label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  strong {
    font-size: var(--marvo-type-13);
  }
  small {
    color: var(--text-tertiary);
    font-size: var(--marvo-type-12);
  }
}

.android-release-success {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 0;
  color: var(--text-success);
  font-size: var(--marvo-type-13);
}

.android-release-actions {
  display: flex;
  justify-content: flex-end;
}

@media (max-width: 980px) {
  .android-release-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 620px) {
  .android-release-heading {
    flex-direction: column;
  }
  .android-release-card {
    padding: 16px;
  }
  .android-release-file-remove {
    width: 40px;
    height: 40px;
  }
}

@media (hover: none) and (pointer: coarse) {
  .android-release-file-remove {
    width: 40px;
    height: 40px;
  }
}
</style>
