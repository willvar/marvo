import { createRouter, createWebHistory } from 'vue-router'

const staleAssetReloadKey = 'marvo.staleAssetReload'

function isStaleDynamicImportError(error: unknown) {
  const message = error instanceof Error ? error.message : String(error)
  return /failed to fetch dynamically imported module|error loading dynamically imported module|importing a module script failed|failed to load module script|unable to preload css/i.test(
    message,
  )
}

function claimStaleAssetReload(target: string) {
  try {
    if (sessionStorage.getItem(staleAssetReloadKey) === target) return false
    sessionStorage.setItem(staleAssetReloadKey, target)
    return true
  } catch {
    return false
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/admin/login',
      component: () => import('./pages/admin/Login.vue'),
    },
    {
      path: '/admin',
      component: () => import('./layouts/AdminLayout.vue'),
      children: [{ path: '', name: 'platform-users', component: () => import('./pages/admin/Users.vue') }],
    },
    {
      path: '/user/:userId/login',
      name: 'user-login',
      component: () => import('./pages/desktop/Login.vue'),
    },
    {
      path: '/user/:userId/admin',
      component: () => import('./layouts/AdminLayout.vue'),
      children: [{ path: '', name: 'user-devices', component: () => import('./pages/admin/Devices.vue') }],
    },
    {
      path: '/user/:userId',
      component: () => import('./layouts/DesktopShell.vue'),
      children: [
        { path: '', name: 'user-home', component: () => import('./pages/desktop/Home.vue') },
        { path: 'note/:title', name: 'user-note', component: () => import('./pages/desktop/NoteEditor.vue') },
        { path: 'agent', name: 'user-agent', component: () => import('./pages/desktop/AgentChat.vue') },
        { path: 'trash', name: 'user-trash', component: () => import('./pages/desktop/Trash.vue') },
      ],
    },
    { path: '/', redirect: '/admin' },
    { path: '/:pathMatch(.*)*', redirect: '/admin' },
  ],
})

router.onError((error, to) => {
  if (!isStaleDynamicImportError(error)) return
  const target = to.fullPath || `${window.location.pathname}${window.location.search}${window.location.hash}`
  if (!claimStaleAssetReload(target)) return
  window.location.assign(target)
})

void router.isReady().then(
  () => {
    try {
      sessionStorage.removeItem(staleAssetReloadKey)
    } catch {
      // Storage can be disabled; successful navigation still needs no recovery marker.
    }
  },
  () => undefined,
)
