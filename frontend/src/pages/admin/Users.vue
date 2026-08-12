<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Dialog } from '@ark-ui/vue/dialog'
import { Field } from '@ark-ui/vue/field'
import {
  CheckOutlined,
  CloseOutlined,
  CopyOutlined,
  DownloadOutlined,
  KeyOutlined,
  LinkOutlined,
  PlusOutlined,
  StopOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import { api, ApiError } from '../../sdk'
import { useRouter } from 'vue-router'
import { useRetainedDialog } from '../../composables/useRetainedDialog'

interface PlatformUser {
  id: string
  name: string
  status: 'setup' | 'active' | 'disabled'
  totp_configured: boolean
  created_at: string
  updated_at: string
}

interface LegacyMigration {
  available: boolean
  note_count: number
  has_trash: boolean
  has_settings: boolean
  has_devices: boolean
  has_agent_state: boolean
  migrated_to?: string
}

const router = useRouter()
const users = ref<PlatformUser[]>([])
const loading = ref(true)
const loadError = ref('')
const createOpen = ref(false)
const createName = ref('')
const createPassword = ref('')
const creating = ref(false)
const createError = ref('')
const actionDialog = useRetainedDialog<{ kind: 'status' | 'credentials' | 'migration'; user: PlatformUser }>()
const { open: actionOpen, payload: action } = actionDialog
const actionPassword = ref('')
const actionBusy = ref(false)
const actionError = ref('')
const copiedUserID = ref('')
const legacy = ref<LegacyMigration | null>(null)

const passwordLength = (value: string) => Array.from(value).length
const createValid = computed(() => createName.value.trim() !== '' && passwordLength(createPassword.value) >= 12)

onMounted(loadUsers)

async function loadUsers() {
  loading.value = true
  loadError.value = ''
  try {
    const [usersResponse, migrationResponse] = await Promise.all([
      api.get('/api/admin/users'),
      api.get('/api/admin/legacy-migration'),
    ])
    users.value = usersResponse.data.users ?? []
    legacy.value = migrationResponse.data.legacy ?? null
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      await router.replace('/admin/login')
      return
    }
    loadError.value = '用户列表加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

function beginCreate() {
  createName.value = ''
  createPassword.value = ''
  createError.value = ''
  createOpen.value = true
}

function updateCreateOpen(open: boolean) {
  if (!open && !creating.value) createOpen.value = false
}

function completeCreateClose() {
  if (createOpen.value) return
  createName.value = ''
  createPassword.value = ''
  createError.value = ''
}

async function createUser() {
  if (!createValid.value || creating.value) return
  creating.value = true
  createError.value = ''
  try {
    const { data } = await api.post('/api/admin/users', {
      name: createName.value.trim(),
      password: createPassword.value,
    })
    users.value = [...users.value, data.user]
    createOpen.value = false
  } catch (error) {
    createError.value = error instanceof Error ? error.message : '创建失败'
  } finally {
    creating.value = false
  }
}

function beginStatus(user: PlatformUser) {
  actionPassword.value = ''
  actionError.value = ''
  actionDialog.show({ kind: 'status', user })
}

function beginCredentials(user: PlatformUser) {
  actionPassword.value = ''
  actionError.value = ''
  actionDialog.show({ kind: 'credentials', user })
}

function beginMigration(user: PlatformUser) {
  actionPassword.value = ''
  actionError.value = ''
  actionDialog.show({ kind: 'migration', user })
}

function updateActionOpen(open: boolean) {
  actionDialog.updateOpen(open, !actionBusy.value)
}

function completeActionClose() {
  if (!actionDialog.clearAfterExit()) return
  actionPassword.value = ''
  actionError.value = ''
}

async function confirmAction() {
  const current = action.value
  if (!current || actionBusy.value) return
  if (current.kind === 'credentials' && passwordLength(actionPassword.value) < 12) {
    actionError.value = '新密码至少 12 个字符'
    return
  }
  actionBusy.value = true
  actionError.value = ''
  try {
    if (current.kind === 'migration') {
      await api.post(`/api/admin/users/${current.user.id}/migrate-legacy`)
      legacy.value = legacy.value ? { ...legacy.value, migrated_to: current.user.id } : null
    } else {
      const { data } =
        current.kind === 'status'
          ? await api.put(`/api/admin/users/${current.user.id}/status`, {
              disabled: current.user.status !== 'disabled',
            })
          : await api.post(`/api/admin/users/${current.user.id}/credentials`, {
              password: actionPassword.value,
            })
      users.value = users.value.map((user) => (user.id === data.user.id ? data.user : user))
    }
    actionDialog.close()
  } catch (error) {
    actionError.value = error instanceof Error ? error.message : '操作失败'
  } finally {
    actionBusy.value = false
  }
}

function workspaceURL(user: PlatformUser) {
  return `${window.location.origin}/user/${user.id}`
}

async function copyWorkspaceURL(user: PlatformUser) {
  await navigator.clipboard.writeText(workspaceURL(user))
  copiedUserID.value = user.id
  setTimeout(() => {
    if (copiedUserID.value === user.id) copiedUserID.value = ''
  }, 1500)
}

function statusLabel(status: PlatformUser['status']) {
  if (status === 'active') return '可用'
  if (status === 'disabled') return '已停用'
  return '可用'
}

function fmt(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}.${pad(date.getMonth() + 1)}.${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}
</script>

<template>
  <section class="platform-users">
    <div class="platform-users-toolbar">
      <div>
        <h1>用户空间</h1>
        <p>只有平台管理员可以创建用户；每个用户独立管理自己的设备和内容。</p>
      </div>
      <button class="admin-btn admin-btn-primary" @click="beginCreate">
        <PlusOutlined aria-hidden="true" />新建用户
      </button>
    </div>

    <div v-if="legacy?.available" class="platform-legacy" role="status">
      <DownloadOutlined aria-hidden="true" />
      <div>
        <strong v-if="legacy.migrated_to">旧版数据已经安全迁移</strong>
        <strong v-else>检测到可迁移的旧版单用户数据</strong>
        <p v-if="legacy.migrated_to">
          原目录仍然保留；目标用户为
          {{ users.find((user) => user.id === legacy?.migrated_to)?.name || legacy.migrated_to }}。
        </p>
        <p v-else>
          包含 {{ legacy.note_count }} 篇笔记{{
            legacy.has_agent_state ? '及智能体会话与凭据' : ''
          }}，可选择一个用户无覆盖迁移。
        </p>
      </div>
    </div>

    <div v-if="loading" class="page-loading" style="min-height: 200px"><span class="page-loading-spinner" /></div>
    <div v-else-if="loadError" class="admin-empty" role="alert">{{ loadError }}</div>
    <div v-else-if="users.length === 0" class="admin-empty">还没有用户</div>
    <table v-else class="admin-table">
      <thead>
        <tr>
          <th>用户</th>
          <th>状态</th>
          <th>创建时间</th>
          <th class="platform-user-actions-heading">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="user in users" :key="user.id">
          <td>
            <div class="platform-user-name"><UserOutlined aria-hidden="true" />{{ user.name }}</div>
            <code class="platform-user-id">{{ user.id }}</code>
          </td>
          <td>
            <span :class="['platform-user-status', user.status]">{{ statusLabel(user.status) }}</span>
          </td>
          <td>{{ fmt(user.created_at) }}</td>
          <td>
            <div class="btn-group platform-user-actions">
              <a class="admin-btn" :href="workspaceURL(user)" target="_blank" rel="noopener noreferrer">
                <LinkOutlined aria-hidden="true" />打开
              </a>
              <button class="admin-btn" @click="copyWorkspaceURL(user)">
                <CopyOutlined aria-hidden="true" />{{ copiedUserID === user.id ? '已复制' : '复制链接' }}
              </button>
              <button class="admin-btn" @click="beginCredentials(user)">
                <KeyOutlined aria-hidden="true" />重置凭据
              </button>
              <button
                v-if="legacy?.available && !legacy.migrated_to && user.status !== 'disabled'"
                class="admin-btn"
                @click="beginMigration(user)"
              >
                <DownloadOutlined aria-hidden="true" />迁移旧数据
              </button>
              <button
                :class="['admin-btn', { 'admin-btn-danger': user.status !== 'disabled' }]"
                @click="beginStatus(user)"
              >
                <StopOutlined v-if="user.status !== 'disabled'" aria-hidden="true" />
                <CheckOutlined v-else aria-hidden="true" />
                {{ user.status === 'disabled' ? '启用' : '停用' }}
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <Dialog.Root
      :open="createOpen"
      lazy-mount
      unmount-on-exit
      @exit-complete="completeCreateClose"
      @update:open="updateCreateOpen"
    >
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content class="dialog-panel" style="max-width: 460px">
            <div class="dialog-header">
              <Dialog.Title>新建用户</Dialog.Title>
              <Dialog.CloseTrigger class="dialog-close" :disabled="creating"><CloseOutlined /></Dialog.CloseTrigger>
            </div>
            <form class="dialog-body platform-user-form" @submit.prevent="createUser">
              <Field.Root>
                <Field.Label>显示名称</Field.Label>
                <Field.Input v-model="createName" class="login-password" maxlength="100" autofocus />
              </Field.Root>
              <Field.Root>
                <Field.Label>初始密码</Field.Label>
                <Field.Input
                  v-model="createPassword"
                  class="login-password"
                  type="password"
                  autocomplete="new-password"
                />
                <Field.HelperText>至少 12 个字符。用户可直接登录，之后自行选择是否绑定身份验证器。</Field.HelperText>
              </Field.Root>
              <p v-if="createError" class="login-error" role="alert">{{ createError }}</p>
              <div class="dialog-footer">
                <Dialog.CloseTrigger class="admin-btn" :disabled="creating">取消</Dialog.CloseTrigger>
                <button class="admin-btn admin-btn-primary" type="submit" :disabled="creating || !createValid">
                  <PlusOutlined aria-hidden="true" />{{ creating ? '创建中...' : '创建用户' }}
                </button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>

    <Dialog.Root
      :open="actionOpen"
      lazy-mount
      unmount-on-exit
      @exit-complete="completeActionClose"
      @update:open="updateActionOpen"
    >
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content v-if="action" class="dialog-panel" style="max-width: 460px">
            <div class="dialog-header">
              <Dialog.Title>{{
                action.kind === 'credentials'
                  ? '重置用户凭据'
                  : action.kind === 'migration'
                    ? '迁移旧版数据'
                    : action.user.status === 'disabled'
                      ? '启用用户'
                      : '停用用户'
              }}</Dialog.Title>
              <Dialog.CloseTrigger class="dialog-close" :disabled="actionBusy"><CloseOutlined /></Dialog.CloseTrigger>
            </div>
            <form class="dialog-body platform-user-form" @submit.prevent="confirmAction">
              <p class="admin-confirm-copy">
                <template v-if="action.kind === 'credentials'">
                  为「{{ action.user.name }}」设置新密码并解绑其身份验证器。所有现有管理会话将立即失效。
                </template>
                <template v-else-if="action.kind === 'migration'">
                  把旧版单用户笔记、回收站、设置、已批准设备以及智能体会话和凭据迁移到「{{
                    action.user.name
                  }}」。迁移不会覆盖不同内容，也不会删除原目录；执行前请确认旧版服务已经停止。
                </template>
                <template v-else-if="action.user.status === 'disabled'"
                  >启用「{{ action.user.name }}」的用户空间？</template
                >
                <template v-else
                  >停用「{{ action.user.name }}」？其管理会话和工作区访问将立即失效，数据不会删除。</template
                >
              </p>
              <Field.Root v-if="action.kind === 'credentials'">
                <Field.Label>新密码</Field.Label>
                <Field.Input
                  v-model="actionPassword"
                  class="login-password"
                  type="password"
                  autocomplete="new-password"
                  autofocus
                />
              </Field.Root>
              <p v-if="actionError" class="login-error" role="alert">{{ actionError }}</p>
              <div class="dialog-footer">
                <Dialog.CloseTrigger class="admin-btn" :disabled="actionBusy">取消</Dialog.CloseTrigger>
                <button
                  :class="[
                    'admin-btn',
                    action.kind === 'status' && action.user.status !== 'disabled'
                      ? 'admin-btn-danger'
                      : 'admin-btn-primary',
                  ]"
                  type="submit"
                  :disabled="actionBusy"
                >
                  <KeyOutlined v-if="action.kind === 'credentials'" aria-hidden="true" />
                  <DownloadOutlined v-else-if="action.kind === 'migration'" aria-hidden="true" />
                  <span>{{ actionBusy ? '处理中...' : '确认' }}</span>
                </button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>
  </section>
</template>

<style scoped lang="scss">
.platform-users-toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
  h1 {
    margin: 0 0 4px;
    font-size: var(--marvo-type-20);
  }
  p {
    margin: 0;
    color: var(--text-secondary);
  }
}
.platform-legacy {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin: 0 0 20px;
  padding: 12px 14px;
  border: 1px solid color-mix(in srgb, var(--marvo-accent-color) 28%, var(--border-primary));
  border-radius: var(--marvo-radius);
  background: color-mix(in srgb, var(--marvo-accent-color) 7%, var(--bg-primary));
  > svg {
    flex: none;
    margin-top: 3px;
    color: var(--marvo-accent-color);
  }
  p {
    margin: 3px 0 0;
    color: var(--text-secondary);
  }
}
.platform-user-name {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 4px;
  font-weight: 600;
}
.platform-user-id {
  color: var(--text-tertiary);
  font-size: var(--marvo-type-12);
}
.platform-user-status {
  display: inline-flex;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  &.active {
    color: var(--marvo-accent-color);
  }
  &.disabled {
    color: var(--text-danger);
  }
}
.platform-user-actions {
  flex-wrap: wrap;
}
@media (min-width: 1200px) {
  .platform-user-actions-heading {
    width: 490px;
  }
  .platform-user-actions {
    flex-wrap: nowrap;
  }
}
.platform-user-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
@media (max-width: 800px) {
  .platform-users-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
