<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../../sdk'
import { useRouter } from 'vue-router'
import { FileTextOutlined, LoginOutlined } from '@ant-design/icons-vue'

const router = useRouter()
const password = ref('')
const loading = ref(false)
const error = ref('')

async function handleLogin() {
  if (!password.value || loading.value) return
  error.value = ''
  loading.value = true
  try {
    const { data } = await api.post('/api/platform/auth/verify', { password: password.value })
    await api.post('/api/platform/auth', { challenge_token: data.challenge_token })
    router.push('/admin')
  } catch {
    error.value = '密码错误'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-container">
    <div class="login-card">
      <div class="login-logo">
        <FileTextOutlined />
      </div>
      <h1 class="login-title">Marvo Admin</h1>
      <p class="login-subtitle">系统管理</p>
      <form class="login-form" @submit.prevent="handleLogin">
        <input class="login-password" type="password" v-model="password" placeholder="请输入密码" autofocus />
        <div v-if="error" class="login-error">{{ error }}</div>
        <button class="login-submit" type="submit" :disabled="loading || !password">
          <LoginOutlined aria-hidden="true" />
          <span>{{ loading ? '验证中...' : '进入' }}</span>
        </button>
      </form>
    </div>
  </div>
</template>
