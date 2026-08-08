import { notifyUnauthorized } from './unauthorized'

const BASE = (() => {
  try {
    return (import.meta.env.VITE_API_BASE || '').replace(/\/$/, '')
  } catch {
    return ''
  }
})()

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly data: any,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

interface RequestOptions {
  params?: Record<string, string | number>
  headers?: Record<string, string>
  signal?: AbortSignal
}

async function request(method: string, url: string, body?: unknown, opts?: RequestOptions) {
  const fullUrl = `${BASE}${url}${queryString(opts?.params)}`

  const init: RequestInit = {
    method,
    credentials: 'include',
    headers: opts?.headers,
    signal: opts?.signal,
  }

  if (body instanceof FormData || body instanceof Blob) {
    init.body = body
  } else if (body !== undefined) {
    init.headers = { ...opts?.headers, 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }

  const res = await fetch(fullUrl, init)

  if (res.status === 401) {
    notifyUnauthorized()
  }

  const contentType = res.headers.get('content-type') || ''
  let data: any = null
  if (contentType.includes('application/json')) {
    data = await res.json().catch(() => null)
  } else if (res.status !== 204) {
    data = await res.text().catch(() => '')
  }
  if (!res.ok) {
    const message =
      typeof data === 'object' && data?.error
        ? String(data.error)
        : typeof data === 'string' && data
          ? data
          : res.statusText
    throw new ApiError(res.status, data, message || `HTTP ${res.status}`)
  }
  return { data }
}

function queryString(params?: Record<string, string | number>) {
  if (!params) return ''
  const entries = Object.entries(params).filter(([, v]) => v !== undefined && v !== '')
  if (entries.length === 0) return ''
  const qs = entries.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&')
  return `?${qs}`
}

export const api = {
  get: (url: string, opts?: { params?: Record<string, string | number> }) => request('GET', url, undefined, opts),
  post: (url: string, body?: unknown) => request('POST', url, body),
  put: (url: string, body?: unknown) => request('PUT', url, body),
  patch: (url: string, body?: unknown) => request('PATCH', url, body),
  delete: (url: string, body?: unknown) => request('DELETE', url, body),
  raw: (method: string, url: string, body: Blob, opts?: RequestOptions) => request(method, url, body, opts),
}
