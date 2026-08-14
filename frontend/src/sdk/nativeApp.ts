import { isMarvoAndroidApp } from './appEnvironment'

type MarvoBridgeErrorCode =
  | 'INVALID_ARGUMENT'
  | 'BRIDGE_UNAVAILABLE'
  | 'UNTRUSTED_ORIGIN'
  | 'PERMISSION_DENIED'
  | 'USER_CANCELLED'
  | 'UNSUPPORTED'
  | 'IO_ERROR'

export interface MarvoBridgeError {
  code: MarvoBridgeErrorCode
  message: string
  details?: unknown
}

interface MarvoEnvironment {
  appName: string
  appVersion: string
  buildNumber: string
  platform: 'android'
  systemVersion: string
  webEngineVersion: string
  bridgeVersion: string
}

interface MarvoCapabilities {
  toast: boolean
  statusBar: boolean
  haptic: boolean
  saveImage: boolean
  shareText: boolean
  shareFile: boolean
  backToHome: boolean
  exitApp: boolean
  update: boolean
  filePicker: boolean
  cameraCapture: boolean
  videoCapture: boolean
  audioCapture: boolean
  camera: boolean
  microphone: boolean
  downloadHttp: boolean
  downloadData: boolean
  downloadBlob: boolean
}

export interface MarvoMethodMap {
  toast: [{ message: string; duration?: 'short' | 'long' }, null]
  statusBar: [{ style: 'dark' | 'light' }, null]
  env: [undefined, MarvoEnvironment]
  capabilities: [undefined, MarvoCapabilities]
  haptic: [undefined, null]
  saveImage: [{ data: string; filename: string }, null]
  share: [{ text?: string; file?: { data: string; filename: string; mimeType: string } }, null]
  backToHome: [undefined, null]
  exitApp: [undefined, null]
  checkUpdate: [undefined, 'noUpdate' | 'available' | 'failed']
}

interface MarvoBridge {
  ready(): Promise<void>
  call<K extends keyof MarvoMethodMap>(
    method: K,
    ...args: MarvoMethodMap[K][0] extends undefined ? [] | [undefined] : [MarvoMethodMap[K][0]]
  ): Promise<MarvoMethodMap[K][1]>
  call(method: string, payload?: unknown): Promise<unknown>
  back(): boolean
}

interface NativeBridgeEvent {
  data?: unknown
}

interface NativeMessageBridge {
  postMessage(message: string): void
  onmessage?: ((event: NativeBridgeEvent) => void) | null
}

interface MarvoTransport {
  send(raw: string): Promise<string>
}

declare global {
  interface Window {
    marvo?: MarvoBridge
    __marvoNative?: NativeMessageBridge
    __marvoTransport?: MarvoTransport
    __marvoHandleBack?: () => boolean
    __marvoTakeDownloadFilename?: () => string | null
  }
}

const unavailable = (message: string): MarvoBridgeError => ({
  code: 'BRIDGE_UNAVAILABLE',
  message,
})

let sequence = 0
let compatibilityTransport: MarvoTransport | undefined

function installDownloadHintCapture() {
  if (!isMarvoAndroidApp() || typeof window.__marvoTakeDownloadFilename === 'function') return
  const hints: Array<{ filename: string; createdAt: number }> = []
  document.addEventListener(
    'click',
    (event) => {
      const anchor = event
        .composedPath()
        .find(
          (entry): entry is HTMLAnchorElement => entry instanceof HTMLAnchorElement && entry.hasAttribute('download'),
        )
      const filename = anchor?.getAttribute('download')?.trim()
      if (!filename) return
      hints.unshift({ filename, createdAt: Date.now() })
      hints.splice(12)
    },
    true,
  )
  Object.defineProperty(window, '__marvoTakeDownloadFilename', {
    configurable: false,
    value: () => {
      const oldestAllowed = Date.now() - 10_000
      while (hints.length > 0 && hints[hints.length - 1]!.createdAt < oldestAllowed) hints.pop()
      return hints.shift()?.filename || null
    },
  })
}

installDownloadHintCapture()

function hasTransport(value: unknown): value is MarvoTransport {
  return Boolean(value && typeof (value as MarvoTransport).send === 'function')
}

function exposeTransport(transport: MarvoTransport) {
  try {
    Object.defineProperty(window, '__marvoTransport', {
      configurable: true,
      value: transport,
    })
  } catch {
    window.__marvoTransport = transport
  }
  return hasTransport(window.__marvoTransport)
}

function installCompatibilityTransport() {
  if (hasTransport(window.__marvoTransport)) return true
  if (compatibilityTransport) return exposeTransport(compatibilityTransport)

  const native = window.__marvoNative
  if (!native || typeof native.postMessage !== 'function') return false

  const pending = new Map<string, { resolve: (raw: string) => void; reject: (error: unknown) => void }>()
  native.onmessage = (event) => {
    if (typeof event?.data !== 'string') return
    try {
      const response = JSON.parse(event.data) as { id?: unknown }
      if (typeof response.id !== 'string') return
      const request = pending.get(response.id)
      if (!request) return
      pending.delete(response.id)
      request.resolve(event.data)
    } catch {
      // Invalid native responses are rejected by the caller when they are decoded.
    }
  }

  compatibilityTransport = Object.freeze({
    send(raw: string) {
      return new Promise<string>((resolve, reject) => {
        let id: unknown
        try {
          id = (JSON.parse(raw) as { id?: unknown }).id
        } catch (error) {
          reject(error)
          return
        }
        if (typeof id !== 'string' || !id) {
          reject(unavailable('Bridge request is missing an id'))
          return
        }
        pending.set(id, { resolve, reject })
        try {
          native.postMessage(raw)
        } catch (error) {
          pending.delete(id)
          reject(error)
        }
      })
    },
  })
  return exposeTransport(compatibilityTransport)
}

function activeTransport() {
  if (installCompatibilityTransport() && hasTransport(window.__marvoTransport)) {
    return window.__marvoTransport
  }
  throw unavailable('Marvo native bridge is unavailable')
}

const compatibilityBridge = Object.freeze({
  ready() {
    try {
      activeTransport()
      return Promise.resolve()
    } catch (error) {
      return Promise.reject(error)
    }
  },
  async call(method: string, payload?: unknown) {
    if (typeof method !== 'string' || !method) {
      throw { code: 'INVALID_ARGUMENT', message: 'method must be a non-empty string' } satisfies MarvoBridgeError
    }
    const id = `${Date.now().toString(36)}-${(++sequence).toString(36)}`
    const raw = await activeTransport().send(
      JSON.stringify({ id, method, payload: payload === undefined ? null : payload }),
    )
    let response: { ok?: unknown; result?: unknown; error?: MarvoBridgeError }
    try {
      response = JSON.parse(raw) as typeof response
    } catch {
      throw unavailable('Native bridge returned an invalid response')
    }
    if (typeof response.ok !== 'boolean') throw unavailable('Native bridge returned an invalid response')
    if (response.ok) return response.result
    throw response.error || unavailable('Native bridge call failed')
  },
  back() {
    try {
      return window.__marvoHandleBack?.() === true
    } catch {
      return false
    }
  },
}) as MarvoBridge

if (
  typeof window !== 'undefined' &&
  !window.marvo &&
  (isMarvoAndroidApp() || window.__marvoNative || window.__marvoTransport)
) {
  try {
    Object.defineProperty(window, 'marvo', {
      configurable: true,
      enumerable: true,
      value: compatibilityBridge,
    })
  } catch {
    window.marvo = compatibilityBridge
  }
}

export function installNativeBackHandler(handler: () => boolean) {
  window.__marvoHandleBack = handler
  return () => {
    if (window.__marvoHandleBack === handler) delete window.__marvoHandleBack
  }
}

export function callMarvo<K extends keyof MarvoMethodMap>(
  method: K,
  ...args: MarvoMethodMap[K][0] extends undefined ? [] | [undefined] : [MarvoMethodMap[K][0]]
) {
  const bridge = typeof window === 'undefined' ? undefined : window.marvo
  if (!isMarvoAndroidApp() || !bridge) {
    return Promise.reject(unavailable('Marvo native bridge is only available in the Android APP'))
  }
  return bridge.call(method, ...args)
}
