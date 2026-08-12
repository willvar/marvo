<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Field } from '@ark-ui/vue/field'
import { SaveOutlined, UndoOutlined } from '@ant-design/icons-vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError, userLoginRoute } from '../../sdk'

const DEFAULT_BRAND = 'Marvo'
const route = useRoute()
const router = useRouter()
const loading = ref(true)
const saving = ref(false)
const brandName = ref(DEFAULT_BRAND)
const savedBrandName = ref(DEFAULT_BRAND)
const usedBytes = ref(0)
const capacityBytes = ref<number | null>(null)
const error = ref('')
const saved = ref(false)

const normalizedBrand = computed(() => brandName.value.trim())
const validationError = computed(() => {
  if (!normalizedBrand.value) return '品牌名称不能为空'
  if (Array.from(normalizedBrand.value).length > 100) return '品牌名称最多 100 个字符'
  if (
    Array.from(normalizedBrand.value).some((character) => {
      const codePoint = character.codePointAt(0) ?? 0
      return codePoint < 32 || (codePoint >= 127 && codePoint <= 159)
    })
  ) {
    return '品牌名称不能包含控制字符'
  }
  return ''
})
const dirty = computed(() => brandName.value !== savedBrandName.value)

watch(brandName, () => {
  if (dirty.value) saved.value = false
})

function handleUnauthorized(error: unknown) {
  if (!(error instanceof ApiError) || error.status !== 401) return false
  void router.replace(userLoginRoute({ admin: true, next: route.fullPath }))
  return true
}

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / 1024 ** index
  return `${value >= 10 || index === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`
}

async function loadSpaceInfo() {
  loading.value = true
  error.value = ''
  try {
    const [brandResponse, spaceResponse] = await Promise.all([api.get('/api/admin/brand'), api.get('/api/admin/space')])
    const name = typeof brandResponse.data.brand?.name === 'string' ? brandResponse.data.brand.name : DEFAULT_BRAND
    brandName.value = name
    savedBrandName.value = name
    usedBytes.value = Number.isFinite(spaceResponse.data.space?.used_bytes)
      ? Math.max(0, Number(spaceResponse.data.space.used_bytes))
      : 0
    capacityBytes.value = Number.isFinite(spaceResponse.data.space?.capacity_bytes)
      ? Math.max(0, Number(spaceResponse.data.space.capacity_bytes))
      : null
  } catch (cause) {
    if (!handleUnauthorized(cause)) error.value = '空间信息加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function saveBrand() {
  if (!dirty.value || validationError.value || saving.value) return
  saving.value = true
  error.value = ''
  saved.value = false
  try {
    const { data } = await api.put('/api/admin/brand', { name: normalizedBrand.value })
    const name = typeof data.brand?.name === 'string' ? data.brand.name : normalizedBrand.value
    brandName.value = name
    savedBrandName.value = name
    saved.value = true
    window.setTimeout(() => (saved.value = false), 1800)
  } catch (cause) {
    if (!handleUnauthorized(cause)) error.value = cause instanceof Error ? cause.message : '保存失败，请稍后重试'
  } finally {
    saving.value = false
  }
}

function restoreDefault() {
  brandName.value = DEFAULT_BRAND
  saved.value = false
}

onMounted(loadSpaceInfo)
</script>

<template>
  <section class="user-settings-page">
    <header class="user-settings-heading">
      <h1>空间信息</h1>
      <p>查看当前用户空间的使用情况与前台展示信息。</p>
    </header>

    <div v-if="loading" class="page-loading user-settings-loading"><span class="page-loading-spinner" /></div>
    <div v-else class="user-settings-stack">
      <section class="user-settings-card user-space-usage-card" aria-label="空间占用">
        <div class="user-settings-card-heading">
          <div>
            <h2>空间占用</h2>
            <p>包含笔记、媒体、回收站、用户配置和智能体运行数据。</p>
          </div>
        </div>
        <div class="user-space-usage-value">{{ formatBytes(usedBytes) }}</div>
        <p class="user-space-usage-detail">
          {{ capacityBytes === null ? '当前未设置空间容量上限' : `容量上限 ${formatBytes(capacityBytes)}` }}
        </p>
      </section>

      <form class="user-settings-card" @submit.prevent="saveBrand">
        <div class="user-settings-card-heading">
          <div>
            <h2>前台品牌</h2>
            <p>显示在笔记前台左上角与浏览器标题中。</p>
          </div>
        </div>
        <Field.Root :invalid="!!validationError">
          <Field.Label>品牌名称</Field.Label>
          <Field.Input v-model="brandName" class="login-password" maxlength="100" autocomplete="off" />
          <Field.HelperText>用户后台始终显示平台名 Marvo；自定义名称不会被强制追加 Marvo。</Field.HelperText>
          <Field.ErrorText>{{ validationError }}</Field.ErrorText>
        </Field.Root>
        <p v-if="error" class="login-error" role="alert">{{ error }}</p>
        <p v-if="saved" class="user-settings-success" role="status">品牌名称已保存</p>
        <div class="user-settings-actions">
          <button
            class="admin-btn"
            type="button"
            :disabled="saving || brandName === DEFAULT_BRAND"
            @click="restoreDefault"
          >
            <UndoOutlined aria-hidden="true" />恢复 Marvo
          </button>
          <button class="admin-btn admin-btn-primary" type="submit" :disabled="saving || !dirty || !!validationError">
            <SaveOutlined aria-hidden="true" />{{ saving ? '保存中...' : '保存' }}
          </button>
        </div>
      </form>
    </div>
  </section>
</template>

<style scoped lang="scss">
.user-settings-page {
  width: min(720px, 100%);
}
.user-settings-heading {
  margin-bottom: 20px;
  h1 {
    margin: 0 0 5px;
    color: var(--text-primary);
    font-size: var(--marvo-type-20);
  }
  p {
    margin: 0;
    color: var(--text-secondary);
  }
}
.user-settings-card {
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 22px;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: var(--bg-primary);
}
.user-settings-stack {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.user-space-usage-card {
  gap: 6px;
}
.user-space-usage-value {
  color: var(--text-primary);
  font-size: var(--marvo-type-28);
  font-weight: 650;
  letter-spacing: -0.02em;
}
.user-space-usage-detail {
  margin: 0;
  color: var(--text-tertiary);
  font-size: var(--marvo-type-12);
}
.user-settings-card-heading {
  h2 {
    margin: 0 0 4px;
    color: var(--text-primary);
    font-size: var(--marvo-type-16);
  }
  p {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--marvo-type-13);
  }
}
.user-settings-card :deep([data-scope='field']) {
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.user-settings-card :deep([data-part='label']) {
  color: var(--text-primary);
  font-size: var(--marvo-type-13);
  font-weight: 600;
}
.user-settings-card :deep([data-part='helper-text']) {
  color: var(--text-tertiary);
  font-size: var(--marvo-type-12);
}
.user-settings-card :deep([data-part='error-text']) {
  color: var(--text-danger);
  font-size: var(--marvo-type-12);
}
.user-settings-success {
  margin: 0;
  color: var(--text-accent);
  font-size: var(--marvo-type-13);
}
.user-settings-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.user-settings-loading {
  min-height: 180px;
}
@media (max-width: 560px) {
  .user-settings-card {
    padding: 16px;
  }
  .user-settings-actions {
    align-items: stretch;
    flex-direction: column-reverse;
    .admin-btn {
      justify-content: center;
    }
  }
}
</style>
