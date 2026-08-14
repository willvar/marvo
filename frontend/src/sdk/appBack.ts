import { onBeforeUnmount, onMounted } from 'vue'
import type { Router } from 'vue-router'
import { callMarvo, installNativeBackHandler } from './nativeApp'
import { isMarvoAndroidApp } from './appEnvironment'
import { currentUserID, workspaceRoute } from './workspace'

const EXIT_CONFIRMATION_WINDOW_MS = 2_000
const EXIT_CONFIRMATION_MESSAGE = '再按一次返回键退出 Marvo'

type BackHandler = () => boolean

interface RegisteredHandler {
  id: number
  priority: number
  handler: BackHandler
}

const handlers = new Map<number, RegisteredHandler>()
let nextHandlerID = 0

function registerAppBackHandler(handler: BackHandler, priority = 0) {
  const id = ++nextHandlerID
  handlers.set(id, { id, priority, handler })
  return () => handlers.delete(id)
}

export function useAppBackHandler(handler: BackHandler, priority = 0) {
  let unregister: (() => void) | undefined
  onMounted(() => {
    unregister = registerAppBackHandler(handler, priority)
  })
  onBeforeUnmount(() => unregister?.())
}

function closeTransientControl() {
  const openControl = document.querySelector(
    [
      '[data-scope="menu"][data-part="positioner"][data-state="open"]',
      '[data-scope="select"][data-part="positioner"][data-state="open"]',
      '[data-scope="popover"][data-part="positioner"][data-state="open"]',
      '[role="listbox"]',
    ].join(','),
  )
  if (!openControl) return false
  const target = document.activeElement instanceof HTMLElement ? document.activeElement : document.body
  target.dispatchEvent(
    new KeyboardEvent('keydown', {
      key: 'Escape',
      code: 'Escape',
      bubbles: true,
      cancelable: true,
    }),
  )
  return true
}

function runRegisteredHandlers() {
  const ordered = [...handlers.values()].sort((left, right) => right.priority - left.priority || right.id - left.id)
  for (const entry of ordered) {
    try {
      if (entry.handler()) return true
    } catch {
      // One page handler must not prevent the remaining back chain from running.
    }
  }
  return false
}

function navigateToParent(router: Router) {
  const route = router.currentRoute.value
  if (route.name === 'user-login' && route.query.mode === 'admin') {
    void router.replace(workspaceRoute('/login'))
    return true
  }
  if (['user-note', 'user-agent', 'user-trash'].includes(String(route.name))) {
    void router.replace(workspaceRoute())
    return true
  }
  if (['user-space-info', 'user-agent-settings', 'user-security'].includes(String(route.name))) {
    void router.replace(workspaceRoute('/admin'))
    return true
  }
  if (route.name === 'user-devices') {
    void router.replace(workspaceRoute())
    return true
  }
  return false
}

function navigateFromNativeEvent(router: Router, raw: unknown) {
  if (typeof raw !== 'string') return
  let target: URL
  try {
    target = new URL(raw, window.location.origin)
  } catch {
    return
  }
  const userID = currentUserID()
  const root = userID ? `/user/${userID}` : ''
  if (target.origin !== window.location.origin || !root) return
  if (target.pathname !== root && !target.pathname.startsWith(`${root}/`)) return
  void router.push(`${target.pathname}${target.search}${target.hash}`)
}

export function installAppNavigation(router: Router) {
  let exitConfirmationUntil = 0

  const resetExitConfirmation = () => {
    exitConfirmationUntil = 0
  }

  const handleWorkspaceExit = () => {
    const userID = currentUserID()
    const atWorkspaceRoot =
      router.currentRoute.value.name === 'user-home' ||
      (userID !== '' && window.location.pathname === workspaceRoute('', userID))
    if (!atWorkspaceRoot || !isMarvoAndroidApp() || typeof window.marvo?.call !== 'function') {
      resetExitConfirmation()
      return false
    }

    const now = performance.now()
    if (exitConfirmationUntil !== 0 && now <= exitConfirmationUntil) {
      resetExitConfirmation()
      void callMarvo('exitApp').catch(() => undefined)
      return true
    }

    exitConfirmationUntil = now + EXIT_CONFIRMATION_WINDOW_MS
    void callMarvo('toast', { message: EXIT_CONFIRMATION_MESSAGE, duration: 'short' }).catch(() => undefined)
    return true
  }

  const removeBackHandler = installNativeBackHandler(() => {
    if (runRegisteredHandlers() || closeTransientControl() || navigateToParent(router)) {
      resetExitConfirmation()
      return true
    }
    return handleWorkspaceExit()
  })
  const navigationHandler = (event: Event) => {
    resetExitConfirmation()
    navigateFromNativeEvent(router, (event as CustomEvent<unknown>).detail)
  }
  const visibilityHandler = () => {
    if (document.hidden) resetExitConfirmation()
  }
  const removeRouteReset = router.afterEach(resetExitConfirmation)
  window.addEventListener('marvo:navigate', navigationHandler)
  document.addEventListener('visibilitychange', visibilityHandler)
  return () => {
    removeBackHandler()
    removeRouteReset()
    window.removeEventListener('marvo:navigate', navigationHandler)
    document.removeEventListener('visibilitychange', visibilityHandler)
  }
}
