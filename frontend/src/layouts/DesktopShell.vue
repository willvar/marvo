<script setup lang="ts">
import { Dialog } from '@ark-ui/vue/dialog'
import QrcodeVue from 'qrcode.vue'
import { useAuthStore } from '../stores/auth'
import { useNoteStore } from '../stores/note'
import AgentFloating from '../components/AgentFloating.vue'
import MarvoMark from '../components/MarvoMark.vue'
import {
  on,
  connect,
  disconnect,
  api,
  ApiError,
  normalizeTheme,
  setColorSchemePreference,
  prepareNoteForAgent,
  DEFAULT_CONTENT_FONT_SIZE,
  DEFAULT_FONT_FAMILY,
  DEFAULT_FONT_SIZE,
  DEFAULT_ACCENT_COLOR,
  isMarvoAndroidApp,
  type ThemeFile,
  currentUserID,
  useAppBackHandler,
  userLoginRoute,
  workspaceRoute,
} from '../sdk'
import { setUserRouteBrand } from '../router'
import { useRoute, useRouter } from 'vue-router'
import { ref, onMounted, onBeforeUnmount, computed, nextTick, watch } from 'vue'
import {
  DeleteOutlined,
  AndroidOutlined,
  CloseOutlined,
  DownloadOutlined,
  LeftOutlined,
  MenuOutlined,
  MobileOutlined,
  DisconnectOutlined,
  ReloadOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons-vue'

const auth = useAuthStore()
const noteStore = useNoteStore()
const router = useRouter()
const route = useRoute()
const androidApp = isMarvoAndroidApp()
const loading = ref(true)
const connectionUnavailable = ref(false)
const retryingConnection = ref(false)
let workspaceInitialized = false
const creating = ref(false)
const searchQuery = ref('')
const searchResults = ref<any[]>([])
const searchError = ref('')
const noteMutationBlocked = ref(false)
type NoteSaveState = 'saved' | 'draft' | 'saving' | 'conflict' | 'error'
const noteSaveStatus = ref<{ state: NoteSaveState; label: string; error: string } | null>(null)
const brandName = ref('Marvo')
const usesPlatformBrand = computed(() => brandName.value === 'Marvo')
const androidOpen = ref(false)
const androidMode = ref<'choice' | 'download' | 'bind'>('choice')
const androidReleaseLoading = ref(false)
const androidReleaseAvailable = ref(false)
const androidReleaseVersion = ref('')
const androidReleaseError = ref('')
const androidDownloadURL = computed(() => new URL('/api/app/android/apk', window.location.origin).toString())

const SIDER_COLLAPSED_STORAGE_KEY = 'marvo.ui.noteListCollapsed'

function storedSiderCollapsed() {
  if (typeof window === 'undefined') return false
  try {
    return window.localStorage.getItem(SIDER_COLLAPSED_STORAGE_KEY) === 'true'
  } catch {
    return false
  }
}

const isCompact = ref(typeof window !== 'undefined' && window.innerWidth <= 900)
const siderCollapsed = ref(isCompact.value || storedSiderCollapsed())
const titleInputRef = ref<HTMLInputElement>()
const editingTitle = ref('')
const editingTitleActive = ref(false)
const headerError = ref('')
let searchSequence = 0
let searchTimer: ReturnType<typeof setTimeout> | null = null

const themeFile = ref<ThemeFile>({})

function onResize() {
  const compact = window.innerWidth <= 900
  if (compact === isCompact.value) return
  isCompact.value = compact
  siderCollapsed.value = compact ? true : storedSiderCollapsed()
}

function setSiderCollapsed(collapsed: boolean) {
  siderCollapsed.value = collapsed
  if (isCompact.value) return
  try {
    window.localStorage.setItem(SIDER_COLLAPSED_STORAGE_KEY, String(collapsed))
  } catch {
    /* The current view still works when browser storage is unavailable. */
  }
}

function loadTheme() {
  api
    .get('/api/theme')
    .then(({ data }) => {
      const tf = normalizeTheme(data)
      themeFile.value = tf
      applyTheme(tf)
    })
    .catch(() => {
      themeFile.value = {}
      applyTheme({})
    })
}

async function loadBrand() {
  const userID = currentUserID()
  try {
    const { data } = await api.get('/api/brand')
    const name = typeof data.brand?.name === 'string' && data.brand.name.trim() ? data.brand.name.trim() : 'Marvo'
    brandName.value = name
    setUserRouteBrand(userID, name)
  } catch {
    brandName.value = 'Marvo'
    setUserRouteBrand(userID, 'Marvo')
  }
}

function applyTheme(tf: ThemeFile) {
  setColorSchemePreference(tf.darkMode ?? 'system')

  const fontFamily = tf.fontFamily || DEFAULT_FONT_FAMILY
  const fontSize = typeof tf.fontSize === 'number' ? tf.fontSize : DEFAULT_FONT_SIZE
  const contentFontSize = typeof tf.contentFontSize === 'number' ? tf.contentFontSize : DEFAULT_CONTENT_FONT_SIZE
  const contentLineHeight = typeof tf.contentLineHeight === 'number' ? tf.contentLineHeight : 1.8
  const contentWidth = tf.contentWidth ?? 'full'
  const accentColor = tf.accentColor || DEFAULT_ACCENT_COLOR
  const radius = typeof tf.radius === 'number' ? tf.radius : 8

  document.documentElement.style.setProperty('--marvo-font-family', fontFamily)
  document.documentElement.style.setProperty('--marvo-font-size', `${fontSize}px`)
  document.documentElement.style.setProperty('--marvo-content-font-size', `${contentFontSize / DEFAULT_FONT_SIZE}rem`)
  document.documentElement.style.setProperty('--marvo-content-line-height', String(contentLineHeight))
  document.documentElement.style.setProperty(
    '--marvo-content-width',
    contentWidth === 'full' ? 'none' : `${contentWidth}px`,
  )
  document.documentElement.style.setProperty('--marvo-accent-color', accentColor)
  document.documentElement.style.setProperty('--marvo-radius', `${radius}px`)
}

const sortedNotes = computed(() => {
  const arr = [...noteStore.notes]
  arr.sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
  return arr
})

const currentNoteTitle = computed(() => {
  const value = route.params.title
  return typeof value === 'string' ? value : ''
})

const currentNoteInfo = computed(() => {
  if (!currentNoteTitle.value) return null
  if (noteStore.currentNote?.note.title === currentNoteTitle.value) return noteStore.currentNote.note
  return noteStore.notes.find((note) => note.title === currentNoteTitle.value) || null
})

const headerTitle = computed(() => {
  if (currentNoteInfo.value) return currentNoteInfo.value.title
  if (route.name === 'user-agent') return '智能体对话'
  if (route.name === 'user-trash') return '回收站'
  return ''
})
function openAgentPage() {
  void router.push(workspaceRoute('/agent'))
}

async function openAndroidEntry() {
  androidMode.value = 'choice'
  androidReleaseError.value = ''
  androidOpen.value = true
  androidReleaseLoading.value = true
  try {
    const { data } = await api.get('/api/app/android/release')
    androidReleaseAvailable.value = true
    androidReleaseVersion.value = typeof data.version_name === 'string' ? data.version_name : ''
  } catch (error) {
    androidReleaseAvailable.value = false
    androidReleaseVersion.value = ''
    if (!(error instanceof ApiError && error.status === 404)) {
      androidReleaseError.value = 'Android 版本信息加载失败，请稍后重试'
    }
  } finally {
    androidReleaseLoading.value = false
  }
}

function updateAndroidOpen(open: boolean) {
  androidOpen.value = open
}

function completeAndroidClose() {
  if (androidOpen.value) return
  androidMode.value = 'choice'
  androidReleaseError.value = ''
}

useAppBackHandler(() => {
  if (editingTitleActive.value) {
    cancelRename()
    return true
  }
  if (androidOpen.value) {
    if (androidMode.value !== 'choice') androidMode.value = 'choice'
    else androidOpen.value = false
    return true
  }
  if (isCompact.value && !siderCollapsed.value) {
    setSiderCollapsed(true)
    return true
  }
  return false
}, 60)

async function initializeWorkspace() {
  if (retryingConnection.value) return
  retryingConnection.value = true
  try {
    await auth.check({ throwOnError: true })
    if (!auth.isAuthenticated) {
      await router.replace(userLoginRoute())
      return
    }
    if (!workspaceInitialized) {
      loadTheme()
      connect()
      await Promise.all([loadBrand(), noteStore.fetchNotes()])
      workspaceInitialized = true
    }
    connectionUnavailable.value = false
  } catch {
    connectionUnavailable.value = true
  } finally {
    loading.value = false
    retryingConnection.value = false
  }
}

function retryWorkspaceWhenOnline() {
  if (connectionUnavailable.value) void initializeWorkspace()
}

onMounted(() => {
  onResize()
  window.addEventListener('resize', onResize)
  window.addEventListener('online', retryWorkspaceWhenOnline)
  void initializeWorkspace()
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  window.removeEventListener('online', retryWorkspaceWhenOnline)
  disconnect()
})

on('theme_changed', () => loadTheme())
on('brand_changed', () => void loadBrand())

async function openNote(title: string) {
  if (isCompact.value) siderCollapsed.value = true
  if (currentNoteTitle.value === title) return
  await router.push(workspaceRoute(`/note/${encodeURIComponent(title)}`))
  searchQuery.value = ''
}

watch(
  () => route.fullPath,
  () => {
    noteMutationBlocked.value = false
    if (isCompact.value) siderCollapsed.value = true
  },
)

watch(noteMutationBlocked, (blocked) => {
  if (blocked) cancelRename()
})

async function doSearch(q: string) {
  if (searchTimer) clearTimeout(searchTimer)
  const sequence = ++searchSequence
  if (!q.trim()) {
    searchResults.value = []
    return
  }
  searchTimer = setTimeout(async () => {
    try {
      const results = await noteStore.search(q)
      if (sequence === searchSequence) searchResults.value = results.slice(0, 10)
    } catch {
      if (sequence === searchSequence) searchResults.value = []
    }
  }, 180)
}

function handleSearchInput() {
  searchError.value = ''
  void doSearch(searchQuery.value)
}

const exactNote = computed(() =>
  searchQuery.value ? noteStore.notes.find((note) => note.title === searchQuery.value.trim()) : undefined,
)

async function createSearchedNote(name: string) {
  if (creating.value) return
  const title = name.trim().normalize('NFC')
  if (!title) return
  const validationError = validateNoteTitle(title)
  if (validationError) {
    searchError.value = validationError
    return
  }

  creating.value = true
  searchError.value = ''
  try {
    const note = await noteStore.createNote(title)
    await router.push(workspaceRoute(`/note/${encodeURIComponent(note.note.title)}`))
    searchQuery.value = ''
    searchResults.value = []
  } catch (error) {
    searchError.value = error instanceof Error ? error.message : '新建笔记失败'
  } finally {
    creating.value = false
  }
}

function validateNoteTitle(title: string) {
  if (Array.from(title).length > 200) return '标题最多 200 个字符'
  if (title.startsWith('.')) return '标题不能以“.”开头'
  if (
    Array.from(title).some((character) => {
      const code = character.codePointAt(0) || 0
      return '/\\:*?"<>|'.includes(character) || code < 32 || (code >= 127 && code <= 159)
    })
  ) {
    return '标题不能包含 / \\ : * ? " < > | 或控制字符'
  }
  return ''
}

function handleSearchEnter() {
  if (!searchQuery.value) return
  if (exactNote.value) openNote(exactNote.value.title)
  else void createSearchedNote(searchQuery.value.trim())
}

function fmtTime(t: string) {
  const d = new Date(t)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffDays = Math.floor(diffMs / 86400000)
  if (diffDays === 0) return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  if (diffDays < 7) return `${diffDays}天前`
  if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}月${d.getDate()}日`
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`
}

function cancelRename() {
  editingTitleActive.value = false
}

function startEditTitle() {
  if (!currentNoteInfo.value || noteMutationBlocked.value) return
  editingTitle.value = currentNoteInfo.value.title
  editingTitleActive.value = true
  nextTick(() => titleInputRef.value?.focus())
}

async function confirmTitle() {
  if (!editingTitleActive.value) return
  const oldTitle = currentNoteTitle.value
  const newTitle = editingTitle.value.trim().normalize('NFC')
  editingTitleActive.value = false
  if (!newTitle || newTitle === oldTitle || !noteStore.currentNote) return
  headerError.value = ''
  const validationError = validateNoteTitle(newTitle)
  if (validationError) {
    headerError.value = validationError
    return
  }
  try {
    await prepareNoteForAgent(oldTitle)
    const moved = await noteStore.renameNote(oldTitle, newTitle, noteStore.currentNote.instance_token)
    await router.replace(workspaceRoute(`/note/${encodeURIComponent(moved.note.title)}`))
  } catch (error) {
    headerError.value = error instanceof Error ? error.message : '标题修改失败'
  }
}
</script>

<template>
  <div v-if="loading" class="page-loading"><span class="page-loading-spinner" /></div>
  <section v-else-if="connectionUnavailable" class="dsh-connection-state" role="status">
    <span class="dsh-connection-icon"><DisconnectOutlined aria-hidden="true" /></span>
    <h1>暂时无法连接 Marvo</h1>
    <p>设备授权状态没有改变。请检查网络后重试，恢复连接后会继续进入当前空间。</p>
    <button
      class="admin-btn admin-btn-primary"
      type="button"
      :disabled="retryingConnection"
      @click="initializeWorkspace"
    >
      <ReloadOutlined aria-hidden="true" />{{ retryingConnection ? '正在重试…' : '重新连接' }}
    </button>
  </section>
  <div v-else class="dsh" :class="{ 'sider-collapsed': siderCollapsed }">
    <aside class="dsh-sider" :class="{ collapsed: siderCollapsed }">
      <div class="dsh-brand">
        <RouterLink
          class="dsh-logo"
          :to="workspaceRoute()"
          :title="`返回 ${brandName} 首页`"
          :aria-label="`返回 ${brandName} 首页`"
          @click="isCompact && setSiderCollapsed(true)"
        >
          <MarvoMark v-if="usesPlatformBrand" class="dsh-logo-mark" />
          <span class="dsh-logo-label">{{ brandName }}</span>
        </RouterLink>
        <button class="dsh-sider-toggle" title="收起列表" @click="setSiderCollapsed(true)">
          <LeftOutlined />
        </button>
      </div>

      <div class="dsh-search">
        <input
          v-model="searchQuery"
          class="dsh-search-inp"
          placeholder="搜索或新建..."
          maxlength="200"
          @input="handleSearchInput"
          @keydown.enter="handleSearchEnter"
        />
        <div v-if="searchError" class="dsh-search-error" role="alert">{{ searchError }}</div>
      </div>

      <nav class="dsh-nav">
        <template v-if="searchQuery">
          <div class="dsh-nav-item dsh-nav-create" @click="handleSearchEnter">
            <span class="dsh-nav-title">{{ exactNote ? `打开「${exactNote.title}」` : `新建「${searchQuery}」` }}</span>
          </div>
          <div v-if="searchResults.length === 0 && !exactNote" class="dsh-nav-empty">未找到相关笔记</div>
          <a
            v-for="r in searchResults"
            :key="r.title"
            :class="['dsh-nav-item', { active: r.title === currentNoteTitle }]"
            @click="openNote(r.title)"
          >
            <span class="dsh-nav-title">{{ r.title }}</span>
            <span v-if="r.tags?.length" class="dsh-nav-excerpt">{{ r.tags.join(' · ') }}</span>
          </a>
        </template>
        <template v-else>
          <div v-if="noteStore.notes.length === 0" class="dsh-nav-empty">还没有笔记</div>
          <a
            v-for="n in sortedNotes"
            :key="n.title"
            :class="['dsh-nav-item', { active: n.title === currentNoteTitle }]"
            @click="openNote(n.title)"
          >
            <span class="dsh-nav-title">{{ n.title }}</span>
            <span class="dsh-nav-meta">
              <span v-if="n.tags?.length" class="dsh-nav-tags">
                <span v-for="tag in n.tags.slice(0, 2)" :key="tag" class="dsh-nav-tag">{{ tag }}</span>
              </span>
              <span class="dsh-nav-time">{{ fmtTime(n.updated_at) }}</span>
            </span>
          </a>
        </template>
      </nav>

      <div class="dsh-footer">
        <button class="dsh-footer-button" @click="router.push(workspaceRoute('/trash'))">
          <DeleteOutlined aria-hidden="true" />
          回收站
        </button>
        <a class="dsh-footer-button" :href="workspaceRoute('/admin')" target="_blank" rel="noopener noreferrer">
          <SafetyCertificateOutlined aria-hidden="true" />
          管理后台
        </a>
      </div>
    </aside>

    <button v-if="siderCollapsed" class="dsh-edge-toggle" @click="setSiderCollapsed(false)" title="展开列表">
      <MenuOutlined />
    </button>

    <div v-if="!siderCollapsed && isCompact" class="dsh-overlay" @click="setSiderCollapsed(true)" />

    <main class="dsh-main">
      <header class="dsh-header">
        <div class="dsh-header-left">
          <template v-if="headerTitle && !editingTitleActive">
            <button
              v-if="currentNoteInfo"
              type="button"
              :class="['dsh-header-title', 'is-clickable', { 'is-disabled': noteMutationBlocked }]"
              :aria-disabled="noteMutationBlocked"
              :title="noteMutationBlocked ? '请先处理当前内容冲突' : '重命名笔记'"
              :disabled="noteMutationBlocked"
              @click="startEditTitle"
            >
              {{ headerTitle }}
            </button>
            <span v-else class="dsh-header-title">{{ headerTitle }}</span>
          </template>
          <template v-else-if="editingTitleActive">
            <input
              ref="titleInputRef"
              v-model="editingTitle"
              class="dsh-header-title-input"
              @keydown.enter="confirmTitle"
              @blur="confirmTitle"
              @keydown.escape="cancelRename"
            />
          </template>
          <span
            v-if="noteSaveStatus && currentNoteInfo"
            :class="['dsh-header-save-status', `state-${noteSaveStatus.state}`]"
            aria-live="polite"
            >{{ noteSaveStatus.label }}</span
          >
          <span
            v-if="noteSaveStatus?.error && currentNoteInfo"
            class="dsh-header-save-error"
            role="alert"
            :title="noteSaveStatus.error"
            >{{ noteSaveStatus.error }}</span
          >
          <span v-if="headerError" class="dsh-header-error" :title="headerError">{{ headerError }}</span>
        </div>
        <div class="dsh-header-actions">
          <button
            v-if="route.name === 'user-home' && !androidApp"
            class="dsh-header-action dsh-header-app"
            type="button"
            title="APP"
            aria-label="APP"
            @click="openAndroidEntry"
          >
            <MobileOutlined aria-hidden="true" />
            <span>APP</span>
          </button>
          <button
            v-if="route.name !== 'user-agent'"
            class="dsh-header-action dsh-header-agent"
            type="button"
            title="智能体"
            aria-label="智能体"
            @click="openAgentPage"
          >
            <RobotOutlined aria-hidden="true" />
            <span>智能体</span>
          </button>
        </div>
      </header>
      <div class="dsh-content">
        <router-view v-slot="{ Component }">
          <component
            :is="Component"
            @note-mutation-blocked="noteMutationBlocked = $event"
            @note-save-status="noteSaveStatus = $event"
          />
        </router-view>
      </div>
    </main>

    <AgentFloating />
  </div>

  <Dialog.Root
    :open="androidOpen"
    lazy-mount
    unmount-on-exit
    @exit-complete="completeAndroidClose"
    @update:open="updateAndroidOpen"
  >
    <Teleport to="body">
      <Dialog.Backdrop class="dialog-backdrop" />
      <Dialog.Positioner class="dialog-positioner">
        <Dialog.Content class="dialog-panel android-entry-dialog">
          <div class="dialog-header">
            <div>
              <Dialog.Title>Android APP</Dialog.Title>
              <Dialog.Description>
                {{
                  androidMode === 'choice'
                    ? '下载安装或将 APP 绑定到当前用户空间'
                    : androidMode === 'download'
                      ? '扫码下载或在当前设备直接下载'
                      : '使用 APP 扫描二维码'
                }}
              </Dialog.Description>
            </div>
            <Dialog.CloseTrigger class="dialog-close" aria-label="关闭 Android APP">
              <CloseOutlined />
            </Dialog.CloseTrigger>
          </div>
          <div class="dialog-body android-entry-body">
            <template v-if="androidMode === 'choice'">
              <div class="android-entry-options">
                <button
                  v-if="androidReleaseAvailable"
                  class="android-entry-option"
                  type="button"
                  @click="androidMode = 'download'"
                >
                  <span class="android-entry-option-icon"><DownloadOutlined aria-hidden="true" /></span>
                  <strong>下载 APK</strong>
                  <small>扫码或直接下载{{ androidReleaseVersion ? ` ${androidReleaseVersion}` : '' }} 通用安装包</small>
                </button>
                <button v-else class="android-entry-option" type="button" disabled>
                  <span class="android-entry-option-icon"><DownloadOutlined aria-hidden="true" /></span>
                  <strong>{{ androidReleaseLoading ? '正在检查版本' : '暂未开放下载' }}</strong>
                  <small>{{ androidReleaseLoading ? '请稍候…' : '平台管理员尚未发布安装包' }}</small>
                </button>
                <button class="android-entry-option" type="button" @click="androidMode = 'bind'">
                  <span class="android-entry-option-icon"><AndroidOutlined aria-hidden="true" /></span>
                  <strong>登录 APP</strong>
                  <small>生成当前用户空间的绑定二维码</small>
                </button>
              </div>
              <p v-if="androidReleaseError" class="android-entry-error" role="alert">{{ androidReleaseError }}</p>
            </template>

            <template v-else-if="androidMode === 'download'">
              <div class="android-entry-qr">
                <div class="android-entry-qr-frame">
                  <QrcodeVue :value="androidDownloadURL" :size="220" level="M" />
                </div>
                <strong>使用 Android 设备扫码下载</strong>
                <p>二维码将打开当前发布版本的 APK 下载地址。</p>
              </div>
              <div class="dialog-footer android-entry-footer">
                <button class="admin-btn" type="button" @click="androidMode = 'choice'">返回</button>
                <a class="admin-btn admin-btn-primary" href="/api/app/android/apk" download>直接下载</a>
              </div>
            </template>

            <template v-else>
              <div class="android-entry-qr">
                <div class="android-entry-qr-frame">
                  <QrcodeVue :value="currentUserID()" :size="220" level="M" />
                </div>
                <strong>在 Marvo APP 中扫描</strong>
                <p>扫描后，APP 会作为新设备提交申请，请在用户后台完成审批。</p>
                <code>{{ currentUserID() }}</code>
              </div>
              <div class="dialog-footer android-entry-footer">
                <button class="admin-btn" type="button" @click="androidMode = 'choice'">返回</button>
                <Dialog.CloseTrigger class="admin-btn admin-btn-primary">完成</Dialog.CloseTrigger>
              </div>
            </template>
          </div>
        </Dialog.Content>
      </Dialog.Positioner>
    </Teleport>
  </Dialog.Root>
</template>

<style lang="scss" scoped>
@mixin truncate {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dsh-connection-state {
  box-sizing: border-box;
  width: min(520px, calc(100% - 32px));
  min-height: 100%;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 32px 20px;
  color: var(--text-primary);
  text-align: center;

  h1 {
    margin: 18px 0 8px;
    font-size: var(--marvo-type-20);
  }

  p {
    margin: 0 0 22px;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-13);
    line-height: 1.7;
  }
}

.dsh-connection-icon {
  width: 56px;
  height: 56px;
  display: grid;
  place-items: center;
  border-radius: 18px;
  background: color-mix(in srgb, var(--text-accent) 10%, transparent);
  color: var(--text-accent);
  font-size: var(--marvo-type-24);
}

.dsh {
  --dsh-shell-header-height: 52px;
  --dsh-pane-gutter: 12px;
  --dsh-pane-toolbar-height: 52px;
  --dsh-pane-control-height: 40px;
  display: flex;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary);
}

.dsh-sider {
  width: clamp(240px, 22vw, 300px);
  min-width: clamp(240px, 22vw, 300px);
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg-secondary);
  border-right: 1px solid var(--border-primary);
  transition:
    margin-left 0.25s cubic-bezier(0.4, 0, 0.2, 1),
    opacity 0.25s ease;
  overflow: hidden;
  &.collapsed {
    margin-left: clamp(-300px, -22vw, -240px);
    opacity: 0;
    pointer-events: none;
  }
}

.dsh-brand {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--dsh-shell-header-height);
  min-height: var(--dsh-shell-header-height);
  padding: 0 16px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-primary);
}
.dsh-logo {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 40px;
  max-width: calc(100% - 48px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--marvo-type-18);
  font-weight: 800;
  letter-spacing: 0.04em;
  color: var(--text-primary);
  text-decoration: none;
  border-radius: 6px;
  transition: opacity 0.15s;
  &:hover {
    opacity: 0.72;
  }
  &:focus-visible {
    outline: 2px solid var(--text-accent);
    outline-offset: 2px;
  }
}
.dsh-logo-mark {
  width: 24px;
  height: 24px;
  flex: 0 0 24px;
  color: #5848f5;
}
.dsh-logo-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.dsh-sider-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border: none;
  background: transparent;
  border-radius: 8px;
  cursor: pointer;
  color: var(--text-tertiary);
  transition:
    background 0.15s,
    color 0.15s;
  &:hover {
    background: rgba(0, 0, 0, 0.08);
    color: var(--text-primary);
  }
  :root[data-color-scheme='dark'] & {
    &:hover {
      background: rgba(255, 255, 255, 0.1);
    }
  }
}

.dsh-search {
  height: var(--dsh-pane-toolbar-height);
  padding: calc((var(--dsh-pane-toolbar-height) - var(--dsh-pane-control-height)) / 2) var(--dsh-pane-gutter);
  flex-shrink: 0;
}
.dsh-search-inp {
  width: 100%;
  height: var(--dsh-pane-control-height);
  padding: 0 12px;
  box-sizing: border-box;
  border: none;
  border-radius: 20px;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-13);
  outline: none;
  transition: box-shadow 0.15s;
  &:focus {
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 20%, transparent);
  }
  &::placeholder {
    color: var(--text-muted);
  }
}
.dsh-search-error {
  padding: 6px 8px 0;
  color: var(--text-danger);
  font-size: var(--marvo-type-11);
  line-height: 1.35;
}

.dsh-nav {
  flex: 1;
  overflow-y: auto;
  padding: 8px var(--dsh-pane-gutter) 16px;
}
.dsh-nav-empty {
  text-align: center;
  color: var(--text-muted);
  font-size: var(--marvo-type-13);
  padding: 32px 0;
}

.dsh-nav-item {
  min-height: var(--dsh-pane-control-height);
  display: flex;
  flex-direction: column;
  padding: 10px 12px;
  border-radius: 8px;
  cursor: pointer;
  text-decoration: none;
  color: var(--text-primary);
  transition: background 0.15s;
  margin-bottom: 2px;
  &:hover {
    background: var(--bg-hover);
  }
  &.active {
    background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 10%, transparent);
    color: var(--text-accent);
    &:hover {
      background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 14%, transparent);
    }
  }
}
.dsh-nav-create {
  color: var(--text-accent);
  font-weight: 500;
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 6%, transparent);
  &:hover {
    background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 10%, transparent);
  }
}

.dsh-nav-title {
  font-size: var(--marvo-type-14);
  font-weight: 500;
  @include truncate;
  line-height: 1.4;
}
.dsh-nav-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 4px;
}
.dsh-nav-tags {
  display: flex;
  gap: 4px;
  overflow: hidden;
}
.dsh-nav-tag {
  font-size: var(--marvo-type-11);
  color: var(--text-tertiary);
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 8%, transparent);
  padding: 1px 6px;
  border-radius: 4px;
  white-space: nowrap;
}
.dsh-nav-time {
  font-size: var(--marvo-type-11);
  color: var(--text-muted);
  white-space: nowrap;
}
.dsh-nav-excerpt {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  margin-top: 4px;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  line-height: 1.45;
}

.dsh-footer {
  padding: var(--dsh-pane-gutter);
  border-top: 1px solid var(--border-primary);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dsh-footer-button {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 100%;
  min-height: 40px;
  border: 1px solid var(--border-light);
  border-radius: 8px;
  background: var(--bg-primary);
  color: var(--text-accent);
  text-decoration: none;
  cursor: pointer;
  font: inherit;
  font-size: var(--marvo-type-13);
  font-weight: 600;
  &:hover {
    background: var(--bg-hover);
  }
}

.android-entry-dialog {
  max-width: 620px;
}

.android-entry-body {
  padding-top: 8px;
}

.android-entry-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.android-entry-option {
  min-height: 164px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 20px;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: var(--bg-primary);
  color: var(--text-primary);
  text-align: center;
  text-decoration: none;
  cursor: pointer;
  font: inherit;
  transition:
    border-color 0.15s,
    background 0.15s,
    transform 0.15s;
  &:hover:not(:disabled) {
    border-color: var(--text-accent);
    background: color-mix(in srgb, var(--text-accent) 5%, var(--bg-primary));
    transform: translateY(-1px);
  }
  &:disabled {
    color: var(--text-muted);
    cursor: not-allowed;
    opacity: 0.7;
  }
  strong {
    font-size: var(--marvo-type-15);
  }
  small {
    color: var(--text-tertiary);
    font-size: var(--marvo-type-12);
    line-height: 1.45;
  }
}

.android-entry-option-icon {
  width: 42px;
  height: 42px;
  display: grid;
  place-items: center;
  margin-bottom: 3px;
  border-radius: 12px;
  background: color-mix(in srgb, var(--text-accent) 10%, transparent);
  color: var(--text-accent);
  font-size: var(--marvo-type-22);
}

.android-entry-error {
  margin: 12px 0 0;
  color: var(--text-danger);
  text-align: center;
  font-size: var(--marvo-type-12);
}

.android-entry-qr {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  strong {
    margin-top: 16px;
    color: var(--text-primary);
    font-size: var(--marvo-type-15);
  }
  p {
    max-width: 430px;
    margin: 7px 0 12px;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-12);
    line-height: 1.6;
  }
  code {
    padding: 4px 8px;
    border-radius: 6px;
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: var(--marvo-type-11);
  }
}

.android-entry-qr-frame {
  padding: 14px;
  border: 1px solid var(--border-light);
  border-radius: 12px;
  background: #fff;
  line-height: 0;
}

.android-entry-footer {
  margin-top: 18px;
}

@media (max-width: 560px) {
  .android-entry-options {
    grid-template-columns: 1fr;
  }
  .android-entry-option {
    min-height: 132px;
  }
}

.dsh-edge-toggle {
  position: fixed;
  top: calc((var(--dsh-shell-header-height) - 32px) / 2);
  left: 12px;
  z-index: 98;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 1px solid var(--border-secondary);
  background: color-mix(in srgb, var(--bg-primary) 82%, transparent);
  color: var(--text-tertiary);
  border-radius: 8px;
  cursor: pointer;
  box-shadow: var(--shadow-card);
  backdrop-filter: blur(14px) saturate(1.2);
  -webkit-backdrop-filter: blur(14px) saturate(1.2);
  transition:
    background 0.15s,
    color 0.15s,
    border-color 0.15s;
  &:hover {
    background: rgba(0, 0, 0, 0.08);
    color: var(--text-primary);
    border-color: var(--border-light);
  }
  :root[data-color-scheme='dark'] & {
    &:hover {
      background: rgba(255, 255, 255, 0.1);
    }
  }
}

.dsh-overlay {
  position: fixed;
  z-index: 99;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.2);
  backdrop-filter: blur(2px);
  -webkit-backdrop-filter: blur(2px);
  :root[data-color-scheme='dark'] & {
    background: rgba(0, 0, 0, 0.5);
  }
}

.dsh-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.dsh-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
  padding: 0 16px;
  height: var(--dsh-shell-header-height);
  min-height: var(--dsh-shell-header-height);
  background: var(--bg-primary);
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-primary);
  transition: padding-left 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}
.sider-collapsed .dsh-header {
  padding-left: 56px;
}

.dsh-header-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex: 1;
}

.dsh-header-title {
  min-width: 0;
  flex: 0 1 auto;
  padding: 4px 8px;
  overflow: hidden;
  border: 0;
  border-radius: 6px;
  appearance: none;
  background: transparent;
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--marvo-type-15);
  font-weight: 600;
  line-height: inherit;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dsh-header-title.is-clickable {
  cursor: pointer;
  transition: background 0.15s;

  &:hover {
    background: var(--bg-hover);
  }

  &:focus-visible {
    outline: 2px solid var(--text-accent);
    outline-offset: 1px;
  }

  &.is-disabled {
    cursor: not-allowed;
    opacity: 0.55;

    &:hover {
      background: transparent;
    }
  }
}

.dsh-header-save-status {
  flex: none;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  white-space: nowrap;
  &.state-conflict,
  &.state-error {
    color: var(--text-danger);
  }
}

.dsh-header-save-error {
  min-width: 0;
  max-width: 220px;
  flex: 0 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--text-danger);
  font-size: var(--marvo-type-11);
  white-space: nowrap;
}

.dsh-header-title-input {
  max-width: 300px;
  height: 30px;
  padding: 0 8px;
  border: 1px solid var(--border-light);
  border-radius: 6px;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-15);
  font-weight: 600;
  outline: none;
  &:focus {
    border-color: var(--text-accent);
  }
}

.dsh-header-error {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-danger);
  font-size: var(--marvo-type-11);
}

.dsh-header-actions {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}

.dsh-header-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 36px;
  padding: 0 11px;
  gap: 7px;
  border: none;
  background: transparent;
  border-radius: 8px;
  cursor: pointer;
  color: var(--text-tertiary);
  flex-shrink: 0;
  font: inherit;
  font-size: var(--marvo-type-13);
  transition:
    background 0.15s,
    color 0.15s;
  &:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
}

.dsh-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

@media (max-width: 900px) {
  .dsh-sider {
    position: fixed;
    z-index: 100;
    top: 0;
    left: 0;
    width: min(86vw, 320px);
    min-width: min(86vw, 320px);
    margin-left: 0;
    opacity: 1;
    pointer-events: auto;
    box-shadow: 0 4px 24px rgba(0, 0, 0, 0.15);
    &.collapsed {
      margin-left: min(-86vw, -320px);
      opacity: 0;
      pointer-events: none;
      box-shadow: none;
    }
  }
  .dsh-edge-toggle {
    left: 12px;
    z-index: 101;
    top: calc((var(--dsh-shell-header-height) - 40px) / 2);
    width: 40px;
    height: 40px;
  }
  .sider-collapsed .dsh-header {
    padding-left: 56px;
  }
  .dsh-overlay {
    display: block;
  }
  .dsh-header-title-input {
    max-width: 160px;
    height: 40px;
  }
  .dsh-header-title.is-clickable {
    min-height: 40px;
    display: inline-flex;
    align-items: center;
  }
  .dsh-header-action {
    min-height: 40px;
  }
}

@media (hover: none) and (pointer: coarse) {
  .dsh-edge-toggle {
    top: calc((var(--dsh-shell-header-height) - 40px) / 2);
    width: 40px;
    height: 40px;
  }
  .dsh-header-title-input,
  .dsh-header-title.is-clickable,
  .dsh-header-action {
    min-height: 40px;
  }
  .dsh-header-title.is-clickable {
    display: inline-flex;
    align-items: center;
  }
}

@media (max-width: 560px) {
  .dsh {
    --dsh-shell-header-height: 48px;
  }
  .dsh-header {
    padding-right: max(10px, env(safe-area-inset-right));
  }
  .dsh-header-title {
    max-width: 42vw;
  }
  .dsh-header-error,
  .dsh-header-save-error {
    display: none;
  }
}
</style>
