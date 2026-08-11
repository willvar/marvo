<script setup lang="ts">
import { ref, onBeforeUnmount, onMounted } from 'vue'
import { api, ApiError } from '../../sdk'
import { useRouter } from 'vue-router'
import { Dialog } from '@ark-ui/vue/dialog'
import {
  CheckOutlined,
  ClockCircleOutlined,
  CloseOutlined,
  SafetyCertificateOutlined,
  StopOutlined,
} from '@ant-design/icons-vue'

interface DeviceInfo {
  user_agent: string
  platform: string
  language: string
  screen: string
  pixel_ratio: string
  timezone: string
  cores: number
  touch_points: number
  color_depth: number
  gpu_vendor: string
  gpu_renderer: string
  gpu_texture_size: number
  gpu_renderbuffer_size: number
  gpu_cube_map_size: number
  gpu_viewport_dims: string
  gpu_vertex_texture_units: number
  gpu_combined_texture_units: number
  gpu_varying_vectors: number
  gpu_fragment_uniform_vectors: number
  gpu_shading_lang_version: string
  wgpu_architecture: string
  wgpu_device: string
  wgpu_description: string
  ip_address: string
}

interface PendingRequest {
  id: string
  local_device_id: string
  device_name: string
  device_info: DeviceInfo
  created_at: string
}

interface ApprovedDevice {
  id: string
  local_device_id: string
  device_name: string
  device_info: DeviceInfo
  approved_at: string
}

const tab = ref<'pending' | 'approved'>('pending')
const requests = ref<PendingRequest[]>([])
const requestsLoading = ref(true)
const requestsError = ref('')
const devices = ref<ApprovedDevice[]>([])
const devicesLoading = ref(true)
const devicesError = ref('')
const detail = ref<DeviceInfo | null>(null)
const revokeTarget = ref<ApprovedDevice | null>(null)
const revokeTargetName = ref('')
const revoking = ref(false)
const revokeError = ref('')
const router = useRouter()
let requestsInFlight = false
let devicesInFlight = false
let refreshTimer: ReturnType<typeof setInterval> | null = null

function handleLoadError(e: unknown, target: typeof requestsError) {
  if (e instanceof ApiError && e.status === 401) {
    void router.replace('/admin/login')
    return
  }
  target.value = '加载失败，请稍后重试'
}

async function loadRequests() {
  if (requestsInFlight) return
  requestsInFlight = true
  try {
    const { data } = await api.get('/api/admin/requests')
    requests.value = data.requests ?? []
    requestsError.value = ''
  } catch (e) {
    handleLoadError(e, requestsError)
  } finally {
    requestsInFlight = false
    requestsLoading.value = false
  }
}

async function loadDevices() {
  if (devicesInFlight) return
  devicesInFlight = true
  try {
    const { data } = await api.get('/api/admin/devices')
    devices.value = data.devices ?? []
    devicesError.value = ''
  } catch (e) {
    handleLoadError(e, devicesError)
  } finally {
    devicesInFlight = false
    devicesLoading.value = false
  }
}

function refreshLists() {
  void loadRequests()
  void loadDevices()
}

onMounted(() => {
  refreshLists()
  refreshTimer = setInterval(refreshLists, 3000)
  window.addEventListener('focus', refreshLists)
})

onBeforeUnmount(() => {
  if (refreshTimer) clearInterval(refreshTimer)
  window.removeEventListener('focus', refreshLists)
})

async function approve(id: string) {
  await api.post(`/api/admin/requests/${id}/approve`)
  loadRequests()
  loadDevices()
}

async function reject(id: string) {
  await api.post(`/api/admin/requests/${id}/reject`)
  loadRequests()
}

function requestRevoke(device: ApprovedDevice) {
  revokeError.value = ''
  revokeTargetName.value = device.device_name || '未命名设备'
  revokeTarget.value = device
}

function updateRevokeOpen(open: boolean) {
  if (!open && !revoking.value) revokeTarget.value = null
}

async function confirmRevoke() {
  const target = revokeTarget.value
  if (!target || revoking.value) return

  revoking.value = true
  revokeError.value = ''
  try {
    await api.delete(`/api/admin/devices/${target.local_device_id}`)
    devices.value = devices.value.filter((device) => device.local_device_id !== target.local_device_id)
    revokeTarget.value = null
    void loadDevices()
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      void router.replace('/admin/login')
      return
    }
    revokeError.value = '撤回失败，请稍后重试'
  } finally {
    revoking.value = false
  }
}

function showInfo(info: DeviceInfo) {
  detail.value = info
}

function updateDetailOpen(open: boolean) {
  if (!open) detail.value = null
}

function fmt(t: string) {
  const date = new Date(t)
  if (Number.isNaN(date.getTime())) return '-'

  const pad = (value: number) => String(value).padStart(2, '0')
  const day = [date.getFullYear(), pad(date.getMonth() + 1), pad(date.getDate())].join('.')
  const time = [pad(date.getHours()), pad(date.getMinutes()), pad(date.getSeconds())].join(':')
  return `${day} ${time}`
}
</script>

<template>
  <div>
    <div class="admin-tabs">
      <button :class="['admin-tab', { active: tab === 'pending' }]" @click="tab = 'pending'">
        <ClockCircleOutlined aria-hidden="true" />
        <span>待审批</span><span class="admin-tab-count">{{ requests.length }}</span>
      </button>
      <button :class="['admin-tab', { active: tab === 'approved' }]" @click="tab = 'approved'">
        <SafetyCertificateOutlined aria-hidden="true" />
        <span>已批准设备</span><span class="admin-tab-count">{{ devices.length }}</span>
      </button>
    </div>

    <!-- Pending -->
    <template v-if="tab === 'pending'">
      <div v-if="requestsLoading" class="page-loading" style="min-height: 200px">
        <span class="page-loading-spinner" />
      </div>
      <div v-else-if="requestsError" class="admin-empty" role="alert">{{ requestsError }}</div>
      <div v-else-if="requests.length === 0" class="admin-empty">没有待审批的设备申请</div>
      <table v-else class="admin-table">
        <thead>
          <tr>
            <th>设备名称</th>
            <th>提交时间</th>
            <th style="width: 184px">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in requests" :key="r.id">
            <td>
              <a @click="showInfo(r.device_info)">{{ r.device_name || '未命名设备' }}</a>
            </td>
            <td>{{ fmt(r.created_at) }}</td>
            <td>
              <div class="btn-group">
                <button class="admin-btn admin-btn-primary" @click="approve(r.id)">
                  <CheckOutlined aria-hidden="true" />批准
                </button>
                <button class="admin-btn" @click="reject(r.id)"><CloseOutlined aria-hidden="true" />拒绝</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </template>

    <!-- Approved -->
    <template v-if="tab === 'approved'">
      <div v-if="devicesLoading" class="page-loading" style="min-height: 200px">
        <span class="page-loading-spinner" />
      </div>
      <div v-else-if="devicesError" class="admin-empty" role="alert">{{ devicesError }}</div>
      <div v-else-if="devices.length === 0" class="admin-empty">没有已批准设备</div>
      <table v-else class="admin-table">
        <thead>
          <tr>
            <th>设备名称</th>
            <th>批准时间</th>
            <th style="width: 100px">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in devices" :key="d.id">
            <td>
              <a @click="showInfo(d.device_info)">{{ d.device_name || '未命名设备' }}</a>
            </td>
            <td>{{ fmt(d.approved_at) }}</td>
            <td>
              <button class="admin-btn admin-btn-danger" @click="requestRevoke(d)">
                <StopOutlined aria-hidden="true" />撤回
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </template>

    <Dialog.Root :open="!!detail" lazy-mount unmount-on-exit @update:open="updateDetailOpen">
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content v-if="detail" class="dialog-panel">
            <div class="dialog-header">
              <Dialog.Title>设备信息</Dialog.Title>
              <Dialog.CloseTrigger class="dialog-close"><CloseOutlined /></Dialog.CloseTrigger>
            </div>
            <div class="dialog-body">
              <dl class="info-grid" style="margin-bottom: 16px">
                <dt>GPU 供应商</dt>
                <dd>{{ detail.gpu_vendor || '-' }}</dd>
                <dt>GPU 渲染器</dt>
                <dd>{{ detail.gpu_renderer || '-' }}</dd>
                <dt>IP 地址</dt>
                <dd>{{ detail.ip_address || '-' }}</dd>
                <dt>UA</dt>
                <dd>
                  <span class="ua-text">{{ detail.user_agent || '-' }}</span>
                </dd>
                <dt>平台</dt>
                <dd>{{ detail.platform || '-' }}</dd>
                <dt>语言</dt>
                <dd>{{ detail.language || '-' }}</dd>
                <dt>屏幕</dt>
                <dd>{{ detail.screen }} @{{ detail.pixel_ratio }}x &middot; {{ detail.color_depth }}bit</dd>
                <dt>时区</dt>
                <dd>{{ detail.timezone || '-' }}</dd>
                <dt>CPU 核心数</dt>
                <dd>{{ detail.cores || '-' }}</dd>
                <dt>触摸点数</dt>
                <dd>{{ detail.touch_points || '-' }}</dd>
              </dl>

              <span class="info-tag blue">WebGL 能力</span>
              <dl class="info-grid" style="margin-bottom: 16px">
                <dt>纹理上限</dt>
                <dd>{{ detail.gpu_texture_size || '-' }}</dd>
                <dt>渲染缓冲上限</dt>
                <dd>{{ detail.gpu_renderbuffer_size || '-' }}</dd>
                <dt>立方体贴图上限</dt>
                <dd>{{ detail.gpu_cube_map_size || '-' }}</dd>
                <dt>视口上限</dt>
                <dd>{{ detail.gpu_viewport_dims || '-' }}</dd>
                <dt>顶点纹理单元</dt>
                <dd>{{ detail.gpu_vertex_texture_units || '-' }}</dd>
                <dt>合并纹理单元</dt>
                <dd>{{ detail.gpu_combined_texture_units || '-' }}</dd>
                <dt>可变向量</dt>
                <dd>{{ detail.gpu_varying_vectors || '-' }}</dd>
                <dt>片段统一向量</dt>
                <dd>{{ detail.gpu_fragment_uniform_vectors || '-' }}</dd>
                <dt>着色语言版本</dt>
                <dd>{{ detail.gpu_shading_lang_version || '-' }}</dd>
              </dl>

              <template v-if="detail.wgpu_device || detail.wgpu_architecture">
                <span class="info-tag purple">WebGPU</span>
                <dl class="info-grid">
                  <dt>架构</dt>
                  <dd>{{ detail.wgpu_architecture || '-' }}</dd>
                  <dt>设备</dt>
                  <dd>{{ detail.wgpu_device || '-' }}</dd>
                  <dt>描述</dt>
                  <dd>{{ detail.wgpu_description || '-' }}</dd>
                </dl>
              </template>
            </div>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>

    <Dialog.Root
      :open="!!revokeTarget"
      lazy-mount
      unmount-on-exit
      :close-on-interact-outside="!revoking"
      @update:open="updateRevokeOpen"
    >
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content class="dialog-panel" style="max-width: 420px">
            <div class="dialog-header">
              <Dialog.Title>撤回设备批准</Dialog.Title>
              <Dialog.CloseTrigger class="dialog-close" :disabled="revoking"><CloseOutlined /></Dialog.CloseTrigger>
            </div>
            <div class="dialog-body">
              <p class="admin-confirm-copy">
                确定撤回「{{ revokeTargetName }}」的访问权限吗？该设备需要重新申请并批准后才能访问。
              </p>
              <p v-if="revokeError" class="admin-confirm-error" role="alert">{{ revokeError }}</p>
              <div class="btn-group admin-confirm-actions">
                <button class="admin-btn" type="button" :disabled="revoking" @click="revokeTarget = null">
                  <CloseOutlined aria-hidden="true" />取消
                </button>
                <button class="admin-btn admin-btn-danger" type="button" :disabled="revoking" @click="confirmRevoke">
                  <StopOutlined aria-hidden="true" />{{ revoking ? '撤回中...' : '确认撤回' }}
                </button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>
  </div>
</template>

<style scoped lang="scss">
.admin-confirm-copy {
  margin: 0;
  color: var(--text-secondary);
  line-height: 1.6;
  overflow-wrap: anywhere;
}

.admin-confirm-error {
  margin: 12px 0 0;
  color: var(--text-danger);
  font-size: var(--marvo-type-13);
}

.admin-confirm-actions {
  justify-content: flex-end;
  margin-top: 24px;
}
</style>
