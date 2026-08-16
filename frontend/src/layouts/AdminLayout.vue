<script setup lang="ts">
import { Dialog } from '@ark-ui/vue/dialog'
import { api, useAppBackHandler, userLoginRoute, workspaceRoute } from '../sdk'
import { useAuthStore } from '../stores/auth'
import { useRouter, useRoute } from 'vue-router'
import { setUserRouteTitleName } from '../router'
import { computed, ref, onMounted, onBeforeUnmount, watch } from 'vue'
import MarvoMark from '../components/MarvoMark.vue'
import {
  CheckOutlined,
  AndroidOutlined,
  CloseOutlined,
  HomeOutlined,
  InfoCircleOutlined,
  LoadingOutlined,
  MenuOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  LockOutlined,
  LeftOutlined,
  UserOutlined,
  LogoutOutlined,
  NotificationOutlined,
} from '@ant-design/icons-vue'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const collapsed = ref(false)
const mobileNavigationOpen = ref(false)
const menuOpen = ref(false)
const dropdownRef = ref<HTMLElement>()
const userName = ref('')
const workspaceAuthorizationOpen = ref(false)
const workspaceAuthorizationKind = ref<'new' | 'pending'>('new')
const workspaceAccessChecking = ref(true)
const workspaceEntryBusy = ref(false)
const workspaceEntryError = ref('')
const userID = computed(() => (typeof route.params.userId === 'string' ? route.params.userId : ''))
const isUserAdmin = computed(() => !!userID.value)
const adminTitle = computed(() => {
  if (!isUserAdmin.value) return route.name === 'platform-android' ? 'Android APP' : '用户管理'
  if (route.name === 'user-space-info') return '空间信息'
  if (route.name === 'user-agent-settings') return '智能体设置'
  if (route.name === 'user-connectors') return '活动连接器'
  if (route.name === 'user-security') return '安全设置'
  return '设备审批'
})
const userAdminRoot = computed(() => workspaceRoute('/admin', userID.value))
const accountName = computed(() => (isUserAdmin.value ? userName.value || '用户管理员' : '平台管理员'))
const navigationItems = computed(() => {
  if (!isUserAdmin.value) {
    return [
      { to: '/admin', routeName: 'platform-users', label: '用户管理', icon: SafetyCertificateOutlined },
      { to: '/admin/android', routeName: 'platform-android', label: 'Android APP', icon: AndroidOutlined },
    ]
  }
  return [
    { to: userAdminRoot.value, routeName: 'user-devices', label: '设备审批', icon: SafetyCertificateOutlined },
    {
      to: `${userAdminRoot.value}/settings`,
      routeName: 'user-space-info',
      label: '空间信息',
      icon: InfoCircleOutlined,
    },
    {
      to: `${userAdminRoot.value}/agent`,
      routeName: 'user-agent-settings',
      label: '智能体设置',
      icon: RobotOutlined,
    },
    {
      to: `${userAdminRoot.value}/connectors`,
      routeName: 'user-connectors',
      label: '活动连接器',
      icon: NotificationOutlined,
    },
    {
      to: `${userAdminRoot.value}/security`,
      routeName: 'user-security',
      label: '安全设置',
      icon: LockOutlined,
    },
  ]
})

function suggestedDeviceName() {
  const userAgent = navigator.userAgent
  if (/iPad/i.test(userAgent)) return 'iPad'
  if (/iPhone/i.test(userAgent)) return 'iPhone'
  if (/Android/i.test(userAgent)) return 'Android 设备'
  if (/Windows/i.test(userAgent)) return 'Windows 设备'
  if (/Macintosh|Mac OS/i.test(userAgent)) return 'Mac'
  if (/Linux/i.test(userAgent)) return 'Linux 设备'
  return '当前设备'
}

function navigateToWorkspace(targetWindow: Window) {
  workspaceAuthorizationOpen.value = false
  targetWindow.location.replace(workspaceRoute('', userID.value))
}

async function refreshWorkspaceAuthorization() {
  if (!isUserAdmin.value) return
  workspaceAccessChecking.value = true
  try {
    await auth.check({ throwOnError: true })
  } catch {
    // The confirmation flow retries this check and presents failures in its dialog.
  } finally {
    workspaceAccessChecking.value = false
  }
}

function beginWorkspaceEntry() {
  if (!isUserAdmin.value || workspaceAccessChecking.value || workspaceEntryBusy.value) return
  workspaceEntryError.value = ''
  workspaceAuthorizationKind.value = auth.applyStatus === 'pending' ? 'pending' : 'new'
  workspaceAuthorizationOpen.value = true
}

function updateWorkspaceAuthorizationOpen(open: boolean) {
  if (!open && !workspaceEntryBusy.value) {
    workspaceAuthorizationOpen.value = false
  }
}

function completeWorkspaceAuthorizationClose() {
  if (!workspaceAuthorizationOpen.value) workspaceEntryError.value = ''
}

async function confirmWorkspaceAuthorization() {
  if (workspaceEntryBusy.value) return
  const workspaceWindow = window.open('', '_blank')
  if (!workspaceWindow) {
    workspaceEntryError.value = '浏览器阻止了新标签页，请允许弹出窗口后重试'
    return
  }
  workspaceWindow.opener = null
  workspaceWindow.document.title = '正在进入工作区…'
  workspaceEntryBusy.value = true
  workspaceEntryError.value = ''
  try {
    await auth.check({ throwOnError: true })
    if (!auth.isAuthenticated && auth.applyStatus !== 'pending') {
      await auth.apply(suggestedDeviceName())
    }
    if (!auth.isAuthenticated) {
      const requestID = auth.requestId
      if (!requestID) throw new Error('missing device request')
      try {
        await api.post(`/api/admin/requests/${encodeURIComponent(requestID)}/approve`)
      } catch (cause) {
        await auth.check({ throwOnError: true })
        if (!auth.isAuthenticated) throw cause
      }
    }
    if (!auth.isAuthenticated) await auth.check({ throwOnError: true })
    if (!auth.isAuthenticated) throw new Error('device authorization was not activated')
    navigateToWorkspace(workspaceWindow)
  } catch {
    workspaceWindow.close()
    workspaceEntryError.value = '授权当前设备失败，请稍后重试'
  } finally {
    workspaceEntryBusy.value = false
  }
}

async function loadUserIdentity() {
  if (!isUserAdmin.value) return
  try {
    const { data } = await api.get('/api/admin/me')
    userName.value = typeof data.user?.name === 'string' ? data.user.name : ''
    setUserRouteTitleName(userID.value, userName.value)
  } catch {
    userName.value = ''
    setUserRouteTitleName(userID.value, '')
  }
}

function handleLogout() {
  api.post(isUserAdmin.value ? '/api/auth/logout' : '/api/platform/auth/logout').catch(() => {})
  router.push(isUserAdmin.value ? userLoginRoute({ admin: true }, userID.value) : '/admin/login')
}

function onDocClick(e: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(e.target as Node)) {
    menuOpen.value = false
  }
}

useAppBackHandler(() => {
  if (workspaceAuthorizationOpen.value) {
    if (!workspaceEntryBusy.value) workspaceAuthorizationOpen.value = false
    return true
  }
  if (mobileNavigationOpen.value) {
    mobileNavigationOpen.value = false
    return true
  }
  if (menuOpen.value) {
    menuOpen.value = false
    return true
  }
  return false
}, 70)

watch(
  () => route.fullPath,
  () => {
    mobileNavigationOpen.value = false
    menuOpen.value = false
  },
)

onMounted(() => {
  document.addEventListener('mousedown', onDocClick)
  void loadUserIdentity()
  void refreshWorkspaceAuthorization()
})
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocClick))
</script>

<template>
  <div class="admin-shell">
    <aside :class="['admin-sidebar', { collapsed }]">
      <div class="admin-sidebar-brand">
        <MarvoMark />
        <span v-if="!collapsed" class="admin-sidebar-label">{{ isUserAdmin ? 'Marvo' : 'Marvo Admin' }}</span>
      </div>

      <nav class="admin-sidebar-nav">
        <router-link
          v-for="item in navigationItems"
          :key="item.routeName"
          :to="item.to"
          :class="{ active: route.name === item.routeName }"
          :aria-label="item.label"
          :title="item.label"
        >
          <component :is="item.icon" />
          <span v-if="!collapsed" class="admin-sidebar-label">{{ item.label }}</span>
        </router-link>
      </nav>

      <button
        class="admin-sidebar-toggle"
        type="button"
        :aria-label="collapsed ? '展开后台导航' : '收起后台导航'"
        :title="collapsed ? '展开后台导航' : '收起后台导航'"
        @click="collapsed = !collapsed"
      >
        <LeftOutlined />
      </button>
    </aside>

    <div class="admin-main">
      <header class="admin-header">
        <div class="admin-header-heading">
          <button
            class="admin-mobile-nav-trigger"
            type="button"
            aria-label="打开后台导航"
            :aria-expanded="mobileNavigationOpen"
            @click="mobileNavigationOpen = true"
          >
            <MenuOutlined aria-hidden="true" />
          </button>
          <span class="admin-header-title">{{ adminTitle }}</span>
        </div>
        <div class="admin-header-actions">
          <a
            v-if="isUserAdmin && auth.isAuthenticated"
            class="admin-workspace-entry"
            :href="workspaceRoute('', userID)"
            target="_blank"
            rel="noopener noreferrer"
          >
            <HomeOutlined aria-hidden="true" />
            <span>进入工作区</span>
          </a>
          <button
            v-else-if="isUserAdmin"
            class="admin-workspace-entry"
            type="button"
            :disabled="workspaceAccessChecking || workspaceEntryBusy"
            @click="beginWorkspaceEntry"
          >
            <LoadingOutlined v-if="workspaceAccessChecking" class="admin-workspace-entry-loading" aria-hidden="true" />
            <HomeOutlined v-else aria-hidden="true" />
            <span>{{ workspaceAccessChecking ? '检查中...' : '进入工作区' }}</span>
          </button>
          <div
            class="admin-header-user"
            ref="dropdownRef"
            :aria-label="accountName"
            :title="accountName"
            @click="menuOpen = !menuOpen"
          >
            <div class="admin-header-avatar">
              <UserOutlined />
            </div>
            <span class="admin-header-user-name">{{ accountName }}</span>
            <div v-if="menuOpen" class="admin-header-dropdown" @click.stop>
              <div class="admin-header-dropdown-identity">{{ accountName }}</div>
              <button @click="handleLogout">
                <LogoutOutlined />
                退出登录
              </button>
            </div>
          </div>
        </div>
      </header>

      <main class="admin-content">
        <router-view />
      </main>
    </div>
  </div>

  <Dialog.Root :open="mobileNavigationOpen" lazy-mount unmount-on-exit @update:open="mobileNavigationOpen = $event">
    <Teleport to="body">
      <Dialog.Backdrop class="dialog-backdrop admin-mobile-nav-backdrop" />
      <Dialog.Positioner class="admin-mobile-nav-positioner">
        <Dialog.Content class="admin-mobile-nav-panel">
          <div class="admin-mobile-nav-header">
            <div class="admin-mobile-nav-brand">
              <MarvoMark />
              <Dialog.Title>{{ isUserAdmin ? 'Marvo' : 'Marvo Admin' }}</Dialog.Title>
            </div>
            <Dialog.CloseTrigger class="admin-mobile-nav-close" aria-label="关闭后台导航">
              <CloseOutlined aria-hidden="true" />
            </Dialog.CloseTrigger>
          </div>
          <nav class="admin-mobile-nav" aria-label="后台导航">
            <router-link
              v-for="item in navigationItems"
              :key="item.routeName"
              :to="item.to"
              :class="{ active: route.name === item.routeName }"
              @click="mobileNavigationOpen = false"
            >
              <component :is="item.icon" aria-hidden="true" />
              <span>{{ item.label }}</span>
            </router-link>
          </nav>
        </Dialog.Content>
      </Dialog.Positioner>
    </Teleport>
  </Dialog.Root>

  <Dialog.Root
    :open="workspaceAuthorizationOpen"
    lazy-mount
    unmount-on-exit
    :close-on-escape="!workspaceEntryBusy"
    :close-on-interact-outside="!workspaceEntryBusy"
    @exit-complete="completeWorkspaceAuthorizationClose"
    @update:open="updateWorkspaceAuthorizationOpen"
  >
    <Teleport to="body">
      <Dialog.Backdrop class="dialog-backdrop" />
      <Dialog.Positioner class="dialog-positioner">
        <Dialog.Content class="dialog-panel admin-workspace-dialog">
          <div class="dialog-header">
            <div>
              <Dialog.Title>{{
                workspaceAuthorizationKind === 'pending' ? '批准当前设备' : '授权当前设备'
              }}</Dialog.Title>
              <Dialog.Description>进入工作区前需要建立设备访问凭据</Dialog.Description>
            </div>
            <Dialog.CloseTrigger class="dialog-close" aria-label="关闭设备授权" :disabled="workspaceEntryBusy">
              <CloseOutlined />
            </Dialog.CloseTrigger>
          </div>
          <div class="dialog-body">
            <p class="admin-workspace-dialog-copy">
              <template v-if="workspaceAuthorizationKind === 'pending'">
                已找到当前设备的待审批申请。确认后将立即批准并进入工作区。
              </template>
              <template v-else>
                当前设备尚未获得访问权限。确认后将自动申请并批准，之后可直接访问，直至你在后台撤回授权。
              </template>
            </p>
            <div class="admin-workspace-dialog-warning" role="note">
              <SafetyCertificateOutlined aria-hidden="true" />
              <span>如果这是临时或公用设备，使用完后请返回后台撤回当前设备授权，并及时退出后台登录。</span>
            </div>
            <p v-if="workspaceEntryError" class="admin-workspace-dialog-error" role="alert">
              {{ workspaceEntryError }}
            </p>
            <div class="dialog-footer">
              <Dialog.CloseTrigger class="admin-btn" :disabled="workspaceEntryBusy">
                <CloseOutlined aria-hidden="true" />取消
              </Dialog.CloseTrigger>
              <button
                class="admin-btn admin-btn-primary"
                type="button"
                :disabled="workspaceEntryBusy"
                @click="confirmWorkspaceAuthorization"
              >
                <LoadingOutlined v-if="workspaceEntryBusy" class="admin-workspace-entry-loading" aria-hidden="true" />
                <CheckOutlined v-else aria-hidden="true" />
                <span>{{ workspaceEntryBusy ? '授权中...' : '授权并进入' }}</span>
              </button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Positioner>
    </Teleport>
  </Dialog.Root>
</template>

<style scoped lang="scss">
.admin-workspace-dialog {
  max-width: 480px;

  .dialog-body {
    padding-top: 8px;
  }
}

.admin-workspace-dialog-copy {
  margin: 0;
  color: var(--text-secondary);
  line-height: 1.65;
}

.admin-workspace-dialog-warning {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin-top: 16px;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--text-warning) 35%, transparent);
  border-radius: 8px;
  background: color-mix(in srgb, var(--text-warning) 9%, transparent);
  color: var(--text-secondary);
  font-size: var(--marvo-type-13);
  line-height: 1.6;
  svg {
    flex: 0 0 auto;
    margin-top: 3px;
    color: var(--text-warning);
  }
}

.admin-workspace-dialog-error {
  margin: 12px 0 0;
  color: var(--text-danger);
  font-size: var(--marvo-type-13);
}
</style>
