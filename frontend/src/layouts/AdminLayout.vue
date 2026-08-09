<script setup lang="ts">
import { api } from '../sdk'
import { useRouter, useRoute } from 'vue-router'
import { ref, onMounted, onBeforeUnmount } from 'vue'
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

function handleLogout() {
  api.post('/api/auth/logout').catch(() => {})
  router.push('/admin/login')
}

function onDocClick(e: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(e.target as Node)) {
    menuOpen.value = false
  }
}

onMounted(() => document.addEventListener('mousedown', onDocClick))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocClick))
</script>

<template>
  <div class="admin-shell">
    <aside :class="['admin-sidebar', { collapsed }]">
      <div class="admin-sidebar-brand">
        <FileTextOutlined />
        <span v-if="!collapsed">Marvo Admin</span>
      </div>

      <nav class="admin-sidebar-nav">
        <router-link to="/admin" :class="{ active: route.path.startsWith('/admin') }">
          <SafetyCertificateOutlined />
          <span v-if="!collapsed">设备审批</span>
        </router-link>
      </nav>

      <button class="admin-sidebar-toggle" @click="collapsed = !collapsed">
        <LeftOutlined />
      </button>
    </aside>

    <div class="admin-main">
      <header class="admin-header">
        <span class="admin-header-title">设备审批</span>
        <div class="admin-header-user" ref="dropdownRef" @click="menuOpen = !menuOpen">
          <div class="admin-header-avatar">
            <UserOutlined />
          </div>
          <span>管理员</span>
          <div v-if="menuOpen" class="admin-header-dropdown" @click.stop>
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
