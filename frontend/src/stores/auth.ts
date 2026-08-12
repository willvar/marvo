import { defineStore } from 'pinia'
import { v4 as uuidv4 } from 'uuid'
import { api, currentUserID, setUnauthorizedHandler, userLoginRoute } from '../sdk'

const LOCAL_DEVICE_KEY = 'marvo_local_device_id'

function getLocalDeviceID(): string {
  let id = localStorage.getItem(LOCAL_DEVICE_KEY)
  if (!id) {
    id = uuidv4()
    localStorage.setItem(LOCAL_DEVICE_KEY, id)
  }
  return id
}

function getGPUInfo() {
  const info: Record<string, string | number> = { gpu_vendor: '', gpu_renderer: '' }
  try {
    const canvas = document.createElement('canvas')
    const gl =
      (canvas.getContext('webgl') as WebGLRenderingContext | null) ||
      (canvas.getContext('experimental-webgl') as WebGLRenderingContext | null)
    if (!gl) return info
    const ext = gl.getExtension('WEBGL_debug_renderer_info')
    if (ext) {
      info.gpu_vendor = String(gl.getParameter(ext.UNMASKED_VENDOR_WEBGL) || '')
      info.gpu_renderer = String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) || '')
    }
    info.gpu_texture_size = gl.getParameter(gl.MAX_TEXTURE_SIZE) ?? 0
    info.gpu_renderbuffer_size = gl.getParameter(gl.MAX_RENDERBUFFER_SIZE) ?? 0
    info.gpu_cube_map_size = gl.getParameter(gl.MAX_CUBE_MAP_TEXTURE_SIZE) ?? 0
    info.gpu_viewport_dims = String(gl.getParameter(gl.MAX_VIEWPORT_DIMS) || '')
    info.gpu_vertex_texture_units = gl.getParameter(gl.MAX_VERTEX_TEXTURE_IMAGE_UNITS) ?? 0
    info.gpu_combined_texture_units = gl.getParameter(gl.MAX_COMBINED_TEXTURE_IMAGE_UNITS) ?? 0
    info.gpu_varying_vectors = gl.getParameter(gl.MAX_VARYING_VECTORS) ?? 0
    info.gpu_fragment_uniform_vectors = gl.getParameter(gl.MAX_FRAGMENT_UNIFORM_VECTORS) ?? 0
    info.gpu_shading_lang_version = String(gl.getParameter(gl.SHADING_LANGUAGE_VERSION) || '')
  } catch {
    /* ignore */
  }
  return info
}

async function getWebGPUInfo() {
  try {
    const gpu = (navigator as any).gpu
    if (!gpu) return { wgpu_architecture: '', wgpu_device: '', wgpu_description: '' }
    const adapter = await gpu.requestAdapter()
    if (!adapter) return { wgpu_architecture: '', wgpu_device: '', wgpu_description: '' }
    const info = await adapter.requestAdapterInfo()
    return {
      wgpu_architecture: info.architecture || '',
      wgpu_device: info.device ? stripControlCharacters(info.device) : '',
      wgpu_description: info.description ? stripControlCharacters(info.description) : '',
    }
  } catch {
    return { wgpu_architecture: '', wgpu_device: '', wgpu_description: '' }
  }
}

function stripControlCharacters(value: string) {
  return Array.from(value)
    .filter((character) => {
      const code = character.codePointAt(0) || 0
      return code > 31 && code !== 127
    })
    .join('')
    .trim()
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    userID: '',
    isAuthenticated: false,
    applyStatus: 'idle' as 'idle' | 'pending' | 'approved' | 'rejected',
    requestId: '',
  }),

  actions: {
    async check(options?: { throwOnError?: boolean }) {
      const activeUserID = currentUserID()
      if (!activeUserID) {
        this.userID = ''
        this.isAuthenticated = false
        this.applyStatus = 'idle'
        this.requestId = ''
        return
      }
      if (this.userID !== activeUserID) {
        this.userID = activeUserID
        this.isAuthenticated = false
        this.applyStatus = 'idle'
        this.requestId = ''
      }
      const localDeviceID = getLocalDeviceID()
      try {
        const { data } = await api.get('/api/auth/token', {
          params: { local_device_id: localDeviceID },
        })
        if (data.status === 'approved') {
          this.isAuthenticated = true
          this.applyStatus = 'approved'
          return
        }
        if (data.status === 'pending') {
          this.isAuthenticated = false
          this.applyStatus = 'pending'
          this.requestId = data.request_id
          return
        }
      } catch (cause) {
        this.isAuthenticated = false
        this.applyStatus = 'idle'
        this.requestId = ''
        if (options?.throwOnError) throw cause
        return
      }
      this.isAuthenticated = false
      this.applyStatus = 'idle'
      this.requestId = ''
    },

    async apply(deviceName: string) {
      const localDeviceID = getLocalDeviceID()
      const glInfo = getGPUInfo()
      const wgpuInfo = await getWebGPUInfo()
      const { data } = await api.post('/api/auth/apply', {
        local_device_id: localDeviceID,
        device_name: deviceName || 'Unknown device',
        device_info: {
          user_agent: navigator.userAgent,
          platform: navigator.platform || '',
          language: navigator.language || '',
          screen: `${screen.width}x${screen.height}`,
          pixel_ratio: `${devicePixelRatio}`,
          timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || '',
          cores: navigator.hardwareConcurrency || 0,
          touch_points: navigator.maxTouchPoints || 0,
          color_depth: screen.colorDepth || 0,
          ...glInfo,
          ...wgpuInfo,
        },
      })
      if (data.status === 'approved') {
        this.isAuthenticated = true
        this.applyStatus = 'approved'
        this.requestId = ''
        return
      }
      this.applyStatus = 'pending'
      this.requestId = data.request_id
      this.isAuthenticated = false
    },
  },
})

setUnauthorizedHandler(() => {
  const store = useAuthStore()
  store.isAuthenticated = false
  if (typeof window !== 'undefined') {
    const userID = currentUserID()
    const userAdminRoot = userID ? `/user/${userID}/admin` : ''
    const userAdmin =
      !!userAdminRoot &&
      (window.location.pathname === userAdminRoot || window.location.pathname.startsWith(`${userAdminRoot}/`))
    const target = userID
      ? userLoginRoute({ admin: userAdmin, next: userAdmin ? window.location.pathname : undefined }, userID)
      : '/admin/login'
    if (window.location.pathname !== target) window.location.replace(target)
  }
})
