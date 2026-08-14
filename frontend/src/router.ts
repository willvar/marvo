import { createRouter, createWebHistory, type RouteLocationNormalizedLoaded } from 'vue-router'
import { USER_ROUTE_BASE } from './sdk/workspace'

const staleAssetReloadKey = 'marvo.staleAssetReload'
const userTitleNames = new Map<string, string>()
const userRouteBrands = new Map<string, string>()

function routeParameter(value: unknown) {
  if (Array.isArray(value)) return typeof value[0] === 'string' ? value[0] : ''
  return typeof value === 'string' ? value : ''
}

function routeTitleParts(route: RouteLocationNormalizedLoaded) {
  switch (route.name) {
    case 'platform-login':
      return ['平台登录']
    case 'platform-users':
      return ['用户管理']
    case 'platform-android':
      return ['Android APP']
    case 'user-login':
      return [route.query.mode === 'admin' ? '用户后台登录' : '设备访问']
    case 'user-devices': {
      const userName = userTitleNames.get(routeParameter(route.params.userId))
      return userName ? ['设备审批', userName] : ['设备审批']
    }
    case 'user-space-info': {
      const userName = userTitleNames.get(routeParameter(route.params.userId))
      return userName ? ['空间信息', userName] : ['空间信息']
    }
    case 'user-agent-settings': {
      const userName = userTitleNames.get(routeParameter(route.params.userId))
      return userName ? ['智能体设置', userName] : ['智能体设置']
    }
    case 'user-security': {
      const userName = userTitleNames.get(routeParameter(route.params.userId))
      return userName ? ['安全设置', userName] : ['安全设置']
    }
    case 'user-home':
      return ['工作区']
    case 'user-note':
      return [routeParameter(route.params.title) || '笔记']
    case 'user-agent':
      return ['智能体']
    case 'user-trash':
      return ['回收站']
    default:
      return []
  }
}

function routeBrand(route: RouteLocationNormalizedLoaded) {
  if (['user-home', 'user-note', 'user-agent', 'user-trash'].includes(String(route.name))) {
    return userRouteBrands.get(routeParameter(route.params.userId)) || 'Marvo'
  }
  return 'Marvo'
}

function applyRouteTitle(route: RouteLocationNormalizedLoaded) {
  const parts = routeTitleParts(route)
  const brand = routeBrand(route)
  document.title = parts.length === 0 ? brand : [...parts, brand].join(' · ')
}

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
      name: 'platform-login',
      component: () => import('./pages/admin/Login.vue'),
    },
    {
      path: '/admin',
      component: () => import('./layouts/AdminLayout.vue'),
      children: [
        { path: '', name: 'platform-users', component: () => import('./pages/admin/Users.vue') },
        {
          path: 'android',
          name: 'platform-android',
          component: () => import('./pages/admin/AndroidRelease.vue'),
        },
      ],
    },
    {
      path: `${USER_ROUTE_BASE}/login`,
      name: 'user-login',
      component: () => import('./pages/desktop/Login.vue'),
    },
    {
      path: `${USER_ROUTE_BASE}/admin`,
      component: () => import('./layouts/AdminLayout.vue'),
      children: [
        { path: '', name: 'user-devices', component: () => import('./pages/admin/Devices.vue') },
        {
          path: 'settings',
          name: 'user-space-info',
          component: () => import('./pages/admin/SpaceInfo.vue'),
        },
        {
          path: 'agent',
          name: 'user-agent-settings',
          component: () => import('./pages/admin/AgentSettings.vue'),
        },
        {
          path: 'security',
          name: 'user-security',
          component: () => import('./pages/admin/SecuritySettings.vue'),
        },
      ],
    },
    {
      path: USER_ROUTE_BASE,
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

export function setUserRouteTitleName(userID: string, name: string) {
  const normalized = name.trim()
  if (normalized) userTitleNames.set(userID, normalized)
  else userTitleNames.delete(userID)
  if (routeParameter(router.currentRoute.value.params.userId) === userID) {
    applyRouteTitle(router.currentRoute.value)
  }
}

export function setUserRouteBrand(userID: string, brand: string) {
  const normalized = brand.trim()
  if (normalized) userRouteBrands.set(userID, normalized)
  else userRouteBrands.delete(userID)
  if (routeParameter(router.currentRoute.value.params.userId) === userID) {
    applyRouteTitle(router.currentRoute.value)
  }
}

router.afterEach((to) => applyRouteTitle(to))

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
