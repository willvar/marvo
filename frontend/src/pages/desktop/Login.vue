<script setup lang="ts">
import { useAuthStore } from '../../stores/auth'
import { useRouter } from 'vue-router'
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { FileTextOutlined, SendOutlined } from '@ant-design/icons-vue'

const auth = useAuthStore()
const router = useRouter()
const deviceName = ref('')
const loading = ref(true)
const error = ref('')
let pollTimer: ReturnType<typeof setInterval> | null = null
let polling = false

function startPolling() {
  if (pollTimer) return
  pollTimer = setInterval(async () => {
    if (polling || auth.applyStatus !== 'pending') return
    polling = true
    try {
      await auth.check()
      if (auth.isAuthenticated) await router.replace('/')
    } finally {
      polling = false
    }
  }, 3000)
}

onMounted(async () => {
  await auth.check()
  if (auth.isAuthenticated) {
    router.push('/')
    return
  }
  if (auth.applyStatus === 'pending') startPolling()
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
    if (auth.isAuthenticated) router.push('/')
    else if (auth.applyStatus === 'pending') startPolling()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '申请提交失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div v-if="loading && !auth.applyStatus" class="page-loading">
    <span class="page-loading-spinner" />
  </div>
  <div v-else class="login-container">
    <div class="login-card">
      <div class="login-logo">
        <FileTextOutlined />
      </div>
      <h1 class="login-title">{{ auth.applyStatus === 'pending' ? '等待审批' : 'Marvo' }}</h1>
      <p class="login-subtitle">
        {{ auth.applyStatus === 'pending' ? '管理员正在审核您的设备' : '输入设备名称以申请访问' }}
      </p>
      <div v-if="error" class="login-error">{{ error }}</div>

      <template v-if="auth.applyStatus !== 'pending'">
        <form class="login-form" @submit.prevent="apply">
          <input class="login-password" v-model="deviceName" placeholder="设备名称" autofocus />
          <button class="login-submit" type="submit" :disabled="loading || !deviceName">
            <SendOutlined aria-hidden="true" />
            <span>{{ loading ? '申请中...' : '申请访问' }}</span>
          </button>
        </form>
      </template>

      <div v-if="auth.applyStatus === 'pending'" class="login-pending">申请已提交，请等待管理员审批...</div>
    </div>
  </div>
</template>
