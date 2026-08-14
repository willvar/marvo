const USER_ID_PATTERN = '[0-9a-f]{20}'
export const USER_ROUTE_BASE = `/user/:userId(${USER_ID_PATTERN})`

const exactUserIDPattern = new RegExp(`^${USER_ID_PATTERN}$`)
const userPathPattern = new RegExp(`^/user/(${USER_ID_PATTERN})(?:/|$)`)
const anyUserPathPattern = /^\/user(?:\/|$)/

function isValidUserID(userID: string) {
  return exactUserIDPattern.test(userID)
}

export function currentUserID(pathname = typeof window === 'undefined' ? '' : window.location.pathname) {
  return pathname.match(userPathPattern)?.[1] || ''
}

export function scopedAPIPath(path: string, userID = currentUserID()) {
  if (!path.startsWith('/api/') || path.startsWith('/api/user/')) return path
  if (path.startsWith('/api/platform/')) return path
  if (path.startsWith('/api/app/')) return path
  if (userID) {
    if (!isValidUserID(userID)) throw new Error('Invalid user ID for scoped API request')
    return `/api/user/${userID}${path.slice('/api'.length)}`
  }
  if (typeof window !== 'undefined' && anyUserPathPattern.test(window.location.pathname)) {
    throw new Error('Cannot send an unscoped API request from an invalid user route')
  }
  return path
}

export function workspaceRoute(path = '', userID = currentUserID()) {
  if (!userID) return '/admin'
  const suffix = path === '' || path === '/' ? '' : path.startsWith('/') ? path : `/${path}`
  return `/user/${userID}${suffix}`
}

export function userLoginRoute(options?: { admin?: boolean; next?: string }, userID = currentUserID()) {
  const base = workspaceRoute('/login', userID)
  if (!options?.admin && !options?.next) return base
  const query = new URLSearchParams()
  if (options.admin) query.set('mode', 'admin')
  if (options.next) query.set('next', options.next)
  return `${base}?${query}`
}
