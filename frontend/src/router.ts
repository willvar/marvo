import { createRouter, createWebHistory, type RouteLocationNormalizedLoaded } from 'vue-router'
import { USER_ROUTE_BASE } from './sdk/workspace'
import { androidRouteStorageKey, isMarvoAndroidApp } from './sdk/appEnvironment'

const staleAssetReloadKey = 'marvo.staleAssetReload'
const userTitleNames = new Map<string, string>()
const userRouteBrands = new Map<string, string>()

function routeParameter(value: unknown) {
  if (Array.isArray(value)) return typeof value[0] === 'string' ? value[0] : ''
  return typeof value === 'string' ? value : ''
}

function routeTitleParts(route: RouteLocationNormalizedLoaded) {
  switch (route.name) {
    case 'landing':
      return []
    case 'not-found':
      return ['页面未找到']
    case 'platform-login':
      return ['平台登录']
    case 'platform-users':
      return ['用户管理']
    case 'platform-android':
      return ['Android APP']
    case 'user-login': {
      const userName = userTitleNames.get(routeParameter(route.params.userId))
      const area = route.query.mode === 'admin' ? '用户后台登录' : '设备访问'
      return userName ? [area, userName] : [area]
    }
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
    case 'user-connectors': {
      const userName = userTitleNames.get(routeParameter(route.params.userId))
      return userName ? ['活动连接器', userName] : ['活动连接器']
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
    case 'user-activity':
      return ['活动']
    case 'user-schedules':
      return ['自动任务']
    case 'user-trash':
      return ['回收站']
    default:
      return []
  }
}

function routeBrand(route: RouteLocationNormalizedLoaded) {
  if (
    ['user-home', 'user-note', 'user-agent', 'user-activity', 'user-schedules', 'user-trash'].includes(
      String(route.name),
    )
  ) {
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

function recoverStaleAssets(target: string) {
  if (!claimStaleAssetReload(target)) return
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`
  if (target === current) window.location.reload()
  else window.location.assign(target)
}

window.addEventListener('vite:preloadError', (event) => {
  event.preventDefault()
  recoverStaleAssets(`${window.location.pathname}${window.location.search}${window.location.hash}`)
})

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'landing',
      component: () => import('./pages/Landing.vue'),
    },
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
        {
          path: 'connectors',
          name: 'user-connectors',
          component: () => import('./pages/admin/Connectors.vue'),
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
        { path: 'activity', name: 'user-activity', component: () => import('./pages/desktop/Activity.vue') },
        { path: 'schedules', name: 'user-schedules', component: () => import('./pages/desktop/Schedules.vue') },
        { path: 'trash', name: 'user-trash', component: () => import('./pages/desktop/Trash.vue') },
      ],
    },
    {
      path: '/user/:pathMatch(.*)*',
      redirect: '/admin/login',
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('./pages/NotFound.vue'),
    },
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

router.afterEach((to) => {
  applyRouteTitle(to)
  if (!isMarvoAndroidApp()) return
  const userID = routeParameter(to.params.userId)
  if (
    !userID ||
    !['user-home', 'user-note', 'user-agent', 'user-activity', 'user-schedules', 'user-trash'].includes(String(to.name))
  )
    return
  try {
    localStorage.setItem(androidRouteStorageKey(userID), to.fullPath)
  } catch {
    // Route restoration is an enhancement; navigation must not depend on storage.
  }
})

router.onError((error, to) => {
  if (!isStaleDynamicImportError(error)) return
  const target = to.fullPath || `${window.location.pathname}${window.location.search}${window.location.hash}`
  recoverStaleAssets(target)
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
