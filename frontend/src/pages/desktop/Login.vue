<script setup lang="ts">
import { useAuthStore } from '../../stores/auth'
import { useRoute, useRouter } from 'vue-router'
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import { api, currentUserID, isMarvoAndroidApp, userLoginRoute, workspaceRoute } from '../../sdk'
import { setUserRouteTitleName } from '../../router'
import MarvoMark from '../../components/MarvoMark.vue'
import { LeftOutlined, LoginOutlined, SafetyCertificateOutlined, SendOutlined } from '@ant-design/icons-vue'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const userID = currentUserID()
const adminMode = computed(() => route.query.mode === 'admin')
const userName = ref('')
const spaceTitle = computed(() => (userName.value ? `您正在访问 ${userName.value} 的空间` : '您正在访问此空间'))
const deviceName = ref('')
const loading = ref(true)
const identityLoading = ref(true)
const error = ref('')
const password = ref('')
const challengeToken = ref('')
const verificationCode = ref('')
const isAndroidApp = isMarvoAndroidApp()
let pollTimer: ReturnType<typeof setInterval> | null = null
let polling = false

async function loadUserIdentity() {
  try {
    const { data } = await api.get('/api/identity')
    userName.value = typeof data.name === 'string' ? data.name.trim() : ''
  } catch {
    userName.value = ''
  } finally {
    identityLoading.value = false
  }
  setUserRouteTitleName(userID, userName.value)
}

function androidDeviceName() {
  if (!isAndroidApp) return ''
  try {
    return localStorage.getItem('marvo_android_device_name')?.trim() || 'Marvo Android'
  } catch {
    return 'Marvo Android'
  }
}

function startPolling() {
  if (pollTimer) return
  pollTimer = setInterval(async () => {
    if (polling || auth.applyStatus !== 'pending') return
    polling = true
    try {
      await auth.check({ throwOnError: true })
      error.value = ''
      if (auth.isAuthenticated) await enterWorkspace()
    } catch {
      error.value = '暂时无法连接 Marvo，将继续重试'
    } finally {
      polling = false
    }
  }, 3000)
}

onMounted(async () => {
  const identityRequest = loadUserIdentity()
  if (adminMode.value) {
    await identityRequest
    loading.value = false
    return
  }
  try {
    await Promise.all([identityRequest, auth.check({ throwOnError: true })])
  } catch {
    await identityRequest
    error.value = '暂时无法连接 Marvo，请检查网络后重试'
    loading.value = false
    return
  }
  if (auth.isAuthenticated) {
    await enterWorkspace()
    return
  }
  if (auth.applyStatus === 'pending') {
    startPolling()
  } else if (isAndroidApp) {
    deviceName.value = androidDeviceName()
    try {
      await auth.apply(deviceName.value)
      if (auth.isAuthenticated) {
        await enterWorkspace()
        return
      }
      startPolling()
    } catch {
      error.value = '自动提交设备申请失败，可以稍后重试'
    }
  }
  loading.value = false
})

onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
})

async function apply() {
  loading.value = true
  error.value = ''
  try {
    await auth.apply(deviceName.value)
    if (auth.isAuthenticated) await enterWorkspace()
    else if (auth.applyStatus === 'pending') startPolling()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '申请提交失败'
  } finally {
    loading.value = false
  }
}

async function verifyPassword() {
  if (!password.value || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const { data } = await api.post('/api/auth/verify', { password: password.value })
    if (data.authenticated) {
      await enterUserAdmin()
      return
    }
    challengeToken.value = data.challenge_token
    verificationCode.value = ''
  } catch {
    error.value = '密码错误或用户不可用'
  } finally {
    loading.value = false
  }
}

async function enterUserAdmin() {
  const requested = typeof route.query.next === 'string' ? route.query.next : ''
  const adminRoot = workspaceRoute('/admin')
  await router.replace(requested === adminRoot || requested.startsWith(`${adminRoot}/`) ? requested : adminRoot)
}

async function enterWorkspace() {
  const requested = typeof route.query.next === 'string' ? route.query.next : ''
  const root = workspaceRoute()
  if (!requested) {
    await router.replace(root)
    return
  }
  const destination = router.resolve(requested)
  const destinationUserID = Array.isArray(destination.params.userId)
    ? destination.params.userId[0]
    : destination.params.userId
  const allowedRoutes = new Set(['user-home', 'user-note', 'user-agent', 'user-activity', 'user-trash'])
  const allowed = destinationUserID === userID && allowedRoutes.has(String(destination.name))
  await router.replace(allowed ? requested : root)
}

async function verifyTOTP() {
  if (!challengeToken.value || verificationCode.value.length !== 6 || loading.value) return
  loading.value = true
  error.value = ''
  try {
    await api.post('/api/auth', {
      challenge_token: challengeToken.value,
      code: verificationCode.value,
    })
    await enterUserAdmin()
  } catch {
    error.value = '验证码无效或已使用，请输入新的验证码'
  } finally {
    loading.value = false
  }
}

function resetAdminLogin() {
  challengeToken.value = ''
  verificationCode.value = ''
  error.value = ''
}
</script>

<template>
  <div v-if="identityLoading || (loading && !adminMode && auth.applyStatus === 'idle')" class="page-loading">
    <span class="page-loading-spinner" />
  </div>
  <div v-else class="login-container">
    <div class="login-card user-space-login-card">
      <div class="login-logo">
        <MarvoMark />
      </div>
      <h1 class="login-title" :title="spaceTitle">{{ spaceTitle }}</h1>
      <p v-if="adminMode" class="login-subtitle">
        {{ challengeToken ? '输入身份验证器生成的 6 位验证码' : '输入密码进入用户后台' }}
      </p>
      <p v-else class="login-subtitle">
        {{ auth.applyStatus === 'pending' ? '用户管理员正在审核您的设备' : '输入设备名称以申请权限' }}
      </p>
      <div v-if="error" class="login-error">{{ error }}</div>

      <template v-if="adminMode">
        <form v-if="!challengeToken" class="login-form" @submit.prevent="verifyPassword">
          <input
            v-model="password"
            class="login-password"
            type="password"
            autocomplete="current-password"
            placeholder="用户密码"
            autofocus
          />
          <button class="login-submit" type="submit" :disabled="loading || !password">
            <LoginOutlined aria-hidden="true" />
            <span>{{ loading ? '登录中...' : '登录' }}</span>
          </button>
        </form>
        <form v-else class="login-form" @submit.prevent="verifyTOTP">
          <input
            v-model="verificationCode"
            class="login-password"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            pattern="[0-9]{6}"
            placeholder="6 位验证码"
            autofocus
          />
          <button class="login-submit" type="submit" :disabled="loading || verificationCode.length !== 6">
            <SafetyCertificateOutlined aria-hidden="true" />
            <span>{{ loading ? '验证中...' : '进入用户后台' }}</span>
          </button>
          <button type="button" class="admin-btn" :disabled="loading" @click="resetAdminLogin">
            <LeftOutlined aria-hidden="true" />返回输入密码
          </button>
        </form>
      </template>

      <template v-else-if="auth.applyStatus !== 'pending'">
        <form class="login-form" @submit.prevent="apply">
          <input class="login-password" v-model="deviceName" placeholder="设备名称" maxlength="50" autofocus />
          <button class="login-submit" type="submit" :disabled="loading || !deviceName">
            <SendOutlined aria-hidden="true" />
            <span>{{ loading ? '申请中...' : '申请权限' }}</span>
          </button>
        </form>
      </template>

      <div v-if="!adminMode && auth.applyStatus === 'pending'" class="login-pending">
        申请已提交，请等待用户管理员审批...
      </div>
      <button
        v-if="!adminMode"
        type="button"
        class="admin-btn login-admin-entry"
        @click="router.push(userLoginRoute({ admin: true, next: workspaceRoute('/admin') }, userID))"
      >
        <SafetyCertificateOutlined aria-hidden="true" />管理设备
      </button>
    </div>
  </div>
</template>

<style scoped>
.user-space-login-card {
  width: fit-content;
  min-width: min(340px, calc(100vw - 24px));
  max-width: min(720px, calc(100vw - 24px));
}
.login-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.user-space-login-card :is(.login-form, .login-error, .login-pending, .login-admin-entry) {
  width: min(340px, 100%);
  margin-right: auto;
  margin-left: auto;
}
.login-admin-entry {
  width: 100%;
  margin-top: 12px;
  justify-content: center;
}
</style>
