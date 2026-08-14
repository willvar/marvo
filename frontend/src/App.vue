<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const offline = ref(typeof navigator !== 'undefined' && !navigator.onLine)

function syncConnectivity() {
  offline.value = !navigator.onLine
}

onMounted(() => {
  window.addEventListener('online', syncConnectivity)
  window.addEventListener('offline', syncConnectivity)
})

onBeforeUnmount(() => {
  window.removeEventListener('online', syncConnectivity)
  window.removeEventListener('offline', syncConnectivity)
})
</script>

<template>
  <div v-if="offline" class="app-offline-banner" role="status">网络连接已断开，恢复后可继续操作</div>
  <router-view />
</template>
