<script setup lang="ts">
import { api, userLoginRoute, workspaceRoute } from '../sdk'
import { useRouter, useRoute } from 'vue-router'
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import {
  FileTextOutlined,
  SafetyCertificateOutlined,
  LeftOutlined,
  UserOutlined,
  LogoutOutlined,
} from '@ant-design/icons-vue'

const router = useRouter()
const route = useRoute()
const collapsed = ref(false)
const menuOpen = ref(false)
const dropdownRef = ref<HTMLElement>()
const userName = ref('')
const userID = computed(() => (typeof route.params.userId === 'string' ? route.params.userId : ''))
const isUserAdmin = computed(() => !!userID.value)
const adminTitle = computed(() => (isUserAdmin.value ? '设备审批' : '用户管理'))
const adminHome = computed(() => (isUserAdmin.value ? workspaceRoute('/admin', userID.value) : '/admin'))
const accountName = computed(() => (isUserAdmin.value ? userName.value || '用户管理员' : '平台管理员'))

async function loadUserIdentity() {
  if (!isUserAdmin.value) return
  try {
    const { data } = await api.get('/api/admin/me')
    userName.value = typeof data.user?.name === 'string' ? data.user.name : ''
  } catch {
    userName.value = ''
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

onMounted(() => {
  document.addEventListener('mousedown', onDocClick)
  void loadUserIdentity()
})
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocClick))
</script>

<template>
  <div class="admin-shell">
    <aside :class="['admin-sidebar', { collapsed }]">
      <div class="admin-sidebar-brand">
        <FileTextOutlined />
        <span v-if="!collapsed">{{ isUserAdmin ? '用户空间管理' : 'Marvo Admin' }}</span>
      </div>

      <nav class="admin-sidebar-nav">
        <router-link :to="adminHome" class="active">
          <SafetyCertificateOutlined />
          <span v-if="!collapsed">{{ adminTitle }}</span>
        </router-link>
      </nav>

      <button class="admin-sidebar-toggle" @click="collapsed = !collapsed">
        <LeftOutlined />
      </button>
    </aside>

    <div class="admin-main">
      <header class="admin-header">
        <span class="admin-header-title">{{ adminTitle }}</span>
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
      </header>

      <main class="admin-content">
        <router-view />
      </main>
    </div>
  </div>
</template>
