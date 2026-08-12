const userPathPattern = /^\/user\/([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})(?:\/|$)/

export function currentUserID(pathname = typeof window === 'undefined' ? '' : window.location.pathname) {
  return pathname.match(userPathPattern)?.[1] || ''
}

export function scopedAPIPath(path: string, userID = currentUserID()) {
  if (!userID || !path.startsWith('/api/') || path.startsWith('/api/user/')) return path
  if (path.startsWith('/api/platform/')) return path
  return `/api/user/${userID}${path.slice('/api'.length)}`
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
