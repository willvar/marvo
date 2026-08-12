<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Field } from '@ark-ui/vue/field'
import { QrcodeSvg } from 'qrcode.vue'
import {
  CheckOutlined,
  CloseOutlined,
  CopyOutlined,
  KeyOutlined,
  SafetyCertificateOutlined,
  StopOutlined,
} from '@ant-design/icons-vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError, userLoginRoute } from '../../sdk'

const route = useRoute()
const router = useRouter()
const loading = ref(true)
const totpConfigured = ref(false)
const pageError = ref('')

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const changingPassword = ref(false)
const passwordError = ref('')
const passwordSaved = ref(false)

const setupPassword = ref('')
const setup = ref<{ secret: string; uri: string } | null>(null)
const setupCode = ref('')
const setupBusy = ref(false)
const setupError = ref('')
const copied = ref(false)

const removalPassword = ref('')
const removalCode = ref('')
const removalBusy = ref(false)
const removalError = ref('')

const newPasswordError = computed(() => {
  if (!newPassword.value) return ''
  if (Array.from(newPassword.value).length < 12) return '新密码至少 12 个字符'
  if (newPassword.value.length > 1024) return '新密码过长'
  return ''
})
const confirmationError = computed(() => {
  if (!confirmPassword.value || confirmPassword.value === newPassword.value) return ''
  return '两次输入的新密码不一致'
})
const passwordFormValid = computed(
  () =>
    !!currentPassword.value &&
    !!newPassword.value &&
    !!confirmPassword.value &&
    !newPasswordError.value &&
    !confirmationError.value,
)
const setupCodeValid = computed(() => /^\d{6}$/.test(setupCode.value))
const removalCodeValid = computed(() => /^\d{6}$/.test(removalCode.value))

function handleUnauthorized(error: unknown) {
  if (!(error instanceof ApiError) || error.status !== 401) return false
  void router.replace(userLoginRoute({ admin: true, next: route.fullPath }))
  return true
}

async function loadSecurity() {
  loading.value = true
  pageError.value = ''
  try {
    const { data } = await api.get('/api/admin/security')
    totpConfigured.value = data.security?.totp_configured === true
  } catch (cause) {
    if (!handleUnauthorized(cause)) pageError.value = '安全设置加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function changePassword() {
  if (!passwordFormValid.value || changingPassword.value) return
  changingPassword.value = true
  passwordError.value = ''
  passwordSaved.value = false
  try {
    await api.put('/api/admin/security/password', {
      current_password: currentPassword.value,
      new_password: newPassword.value,
    })
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    passwordSaved.value = true
  } catch (cause) {
    if (!handleUnauthorized(cause)) {
      passwordError.value =
        cause instanceof ApiError && cause.data?.error === 'current password is invalid'
          ? '当前密码不正确'
          : '密码修改失败，请稍后重试'
    }
  } finally {
    changingPassword.value = false
  }
}

async function beginTOTPSetup() {
  if (!setupPassword.value || setupBusy.value) return
  setupBusy.value = true
  setupError.value = ''
  try {
    const { data } = await api.post('/api/admin/security/totp', { password: setupPassword.value })
    setup.value = data.totp_setup
    setupCode.value = ''
    setupPassword.value = ''
  } catch (cause) {
    if (!handleUnauthorized(cause)) {
      setupError.value =
        cause instanceof ApiError && cause.data?.error === 'current password is invalid'
          ? '当前密码不正确'
          : '无法生成绑定信息，请稍后重试'
    }
  } finally {
    setupBusy.value = false
  }
}

async function confirmTOTPSetup() {
  if (!setup.value || !setupCodeValid.value || setupBusy.value) return
  setupBusy.value = true
  setupError.value = ''
  try {
    await api.post('/api/admin/security/totp/confirm', { code: setupCode.value })
    totpConfigured.value = true
    setup.value = null
    setupCode.value = ''
  } catch (cause) {
    if (!handleUnauthorized(cause)) setupError.value = '验证码无效或已使用，请输入新的验证码'
  } finally {
    setupBusy.value = false
  }
}

async function copySecret() {
  if (!setup.value) return
  await navigator.clipboard.writeText(setup.value.secret)
  copied.value = true
  window.setTimeout(() => (copied.value = false), 1500)
}

function cancelTOTPSetup() {
  setup.value = null
  setupCode.value = ''
  setupError.value = ''
}

async function removeTOTP() {
  if (!removalPassword.value || !removalCodeValid.value || removalBusy.value) return
  removalBusy.value = true
  removalError.value = ''
  try {
    await api.delete('/api/admin/security/totp', {
      password: removalPassword.value,
      code: removalCode.value,
    })
    totpConfigured.value = false
    removalPassword.value = ''
    removalCode.value = ''
  } catch (cause) {
    if (!handleUnauthorized(cause)) {
      removalError.value =
        cause instanceof ApiError && cause.data?.error === 'current password is invalid'
          ? '当前密码不正确'
          : '密码或验证码不正确'
    }
  } finally {
    removalBusy.value = false
  }
}

onMounted(loadSecurity)
</script>

<template>
  <section class="security-settings-page">
    <header class="security-settings-heading">
      <h1>安全设置</h1>
      <p>管理当前用户自己的后台登录凭据。</p>
    </header>

    <div v-if="loading" class="page-loading security-settings-loading"><span class="page-loading-spinner" /></div>
    <div v-else class="security-settings-stack">
      <p v-if="pageError" class="login-error" role="alert">{{ pageError }}</p>

      <form class="security-settings-card" @submit.prevent="changePassword">
        <div class="security-settings-card-heading">
          <div class="security-settings-card-icon"><KeyOutlined aria-hidden="true" /></div>
          <div>
            <h2>登录密码</h2>
            <p>修改后其他用户后台会话会失效，当前浏览器会继续保持登录。</p>
          </div>
        </div>
        <div class="security-settings-fields">
          <Field.Root>
            <Field.Label>当前密码</Field.Label>
            <Field.Input
              v-model="currentPassword"
              class="login-password"
              type="password"
              autocomplete="current-password"
            />
          </Field.Root>
          <Field.Root :invalid="!!newPasswordError">
            <Field.Label>新密码</Field.Label>
            <Field.Input v-model="newPassword" class="login-password" type="password" autocomplete="new-password" />
            <Field.HelperText>至少 12 个字符。</Field.HelperText>
            <Field.ErrorText>{{ newPasswordError }}</Field.ErrorText>
          </Field.Root>
          <Field.Root :invalid="!!confirmationError">
            <Field.Label>确认新密码</Field.Label>
            <Field.Input v-model="confirmPassword" class="login-password" type="password" autocomplete="new-password" />
            <Field.ErrorText>{{ confirmationError }}</Field.ErrorText>
          </Field.Root>
        </div>
        <p v-if="passwordError" class="login-error" role="alert">{{ passwordError }}</p>
        <p v-if="passwordSaved" class="security-settings-success" role="status">密码已修改</p>
        <div class="security-settings-actions">
          <button class="admin-btn admin-btn-primary" type="submit" :disabled="changingPassword || !passwordFormValid">
            <KeyOutlined aria-hidden="true" />{{ changingPassword ? '修改中...' : '修改密码' }}
          </button>
        </div>
      </form>

      <section class="security-settings-card">
        <div class="security-settings-card-heading security-settings-status-heading">
          <div class="security-settings-card-icon"><SafetyCertificateOutlined aria-hidden="true" /></div>
          <div>
            <h2>身份验证器</h2>
            <p>绑定后，使用密码登录时还需要输入 6 位动态验证码。</p>
          </div>
          <span :class="['security-status', { enabled: totpConfigured }]">
            {{ totpConfigured ? '已绑定' : '未绑定' }}
          </span>
        </div>

        <template v-if="!totpConfigured">
          <form v-if="!setup" class="security-settings-fields" @submit.prevent="beginTOTPSetup">
            <Field.Root>
              <Field.Label>确认当前密码</Field.Label>
              <Field.Input
                v-model="setupPassword"
                class="login-password"
                type="password"
                autocomplete="current-password"
              />
              <Field.HelperText>OTP 是可选功能，不绑定也可以正常使用用户后台。</Field.HelperText>
            </Field.Root>
            <p v-if="setupError" class="login-error" role="alert">{{ setupError }}</p>
            <div class="security-settings-actions">
              <button class="admin-btn admin-btn-primary" type="submit" :disabled="setupBusy || !setupPassword">
                <SafetyCertificateOutlined aria-hidden="true" />{{ setupBusy ? '生成中...' : '生成绑定二维码' }}
              </button>
            </div>
          </form>

          <form v-else class="totp-enrollment" @submit.prevent="confirmTOTPSetup">
            <p>使用身份验证器扫描二维码，再输入当前显示的验证码完成绑定。</p>
            <div class="totp-enrollment-content">
              <div class="totp-qr" role="img" aria-label="身份验证器设置二维码">
                <QrcodeSvg
                  :value="setup.uri"
                  :size="192"
                  :margin="2"
                  level="M"
                  background="#ffffff"
                  foreground="#111111"
                />
              </div>
              <div class="totp-enrollment-fields">
                <div class="totp-secret">
                  <span>手动设置密钥</span>
                  <code>{{ setup.secret }}</code>
                  <button type="button" class="admin-btn" @click="copySecret">
                    <CopyOutlined aria-hidden="true" />{{ copied ? '已复制' : '复制密钥' }}
                  </button>
                </div>
                <Field.Root>
                  <Field.Label>6 位验证码</Field.Label>
                  <Field.Input
                    v-model="setupCode"
                    class="login-password"
                    inputmode="numeric"
                    autocomplete="one-time-code"
                    maxlength="6"
                    pattern="[0-9]{6}"
                  />
                </Field.Root>
              </div>
            </div>
            <p v-if="setupError" class="login-error" role="alert">{{ setupError }}</p>
            <div class="security-settings-actions">
              <button class="admin-btn" type="button" :disabled="setupBusy" @click="cancelTOTPSetup">
                <CloseOutlined aria-hidden="true" />取消
              </button>
              <button class="admin-btn admin-btn-primary" type="submit" :disabled="setupBusy || !setupCodeValid">
                <CheckOutlined aria-hidden="true" />{{ setupBusy ? '验证中...' : '确认绑定' }}
              </button>
            </div>
          </form>
        </template>

        <form v-else class="security-settings-fields" @submit.prevent="removeTOTP">
          <p class="security-settings-copy">解绑后，用户后台将只使用密码登录。此操作不会修改密码。</p>
          <Field.Root>
            <Field.Label>确认当前密码</Field.Label>
            <Field.Input
              v-model="removalPassword"
              class="login-password"
              type="password"
              autocomplete="current-password"
            />
          </Field.Root>
          <Field.Root>
            <Field.Label>当前 6 位验证码</Field.Label>
            <Field.Input
              v-model="removalCode"
              class="login-password"
              inputmode="numeric"
              autocomplete="one-time-code"
              maxlength="6"
              pattern="[0-9]{6}"
            />
          </Field.Root>
          <p v-if="removalError" class="login-error" role="alert">{{ removalError }}</p>
          <div class="security-settings-actions">
            <button
              class="admin-btn admin-btn-danger"
              type="submit"
              :disabled="removalBusy || !removalPassword || !removalCodeValid"
            >
              <StopOutlined aria-hidden="true" />{{ removalBusy ? '解绑中...' : '解绑身份验证器' }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped lang="scss">
.security-settings-page {
  width: min(780px, 100%);
}
.security-settings-heading {
  margin-bottom: 20px;
  h1 {
    margin: 0 0 5px;
    color: var(--text-primary);
    font-size: var(--marvo-type-20);
  }
  p {
    margin: 0;
    color: var(--text-secondary);
  }
}
.security-settings-stack {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.security-settings-card {
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 22px;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: var(--bg-primary);
}
.security-settings-card-heading {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  h2 {
    margin: 0 0 4px;
    color: var(--text-primary);
    font-size: var(--marvo-type-16);
  }
  p {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--marvo-type-13);
  }
}
.security-settings-card-icon {
  display: grid;
  width: 34px;
  height: 34px;
  flex: none;
  place-items: center;
  border-radius: 9px;
  background: color-mix(in srgb, var(--marvo-accent-color) 12%, transparent);
  color: var(--text-accent);
}
.security-settings-status-heading > div:nth-child(2) {
  min-width: 0;
  flex: 1;
}
.security-status {
  display: inline-flex;
  flex: none;
  padding: 3px 9px;
  border-radius: 999px;
  background: var(--bg-tertiary);
  color: var(--text-tertiary);
  font-size: var(--marvo-type-12);
  &.enabled {
    background: color-mix(in srgb, var(--marvo-accent-color) 12%, transparent);
    color: var(--text-accent);
  }
}
.security-settings-fields,
.totp-enrollment-fields {
  display: flex;
  flex-direction: column;
  gap: 15px;
}
.security-settings-card :deep([data-scope='field']) {
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.security-settings-card :deep([data-part='label']) {
  color: var(--text-primary);
  font-size: var(--marvo-type-13);
  font-weight: 600;
}
.security-settings-card :deep([data-part='helper-text']) {
  color: var(--text-tertiary);
  font-size: var(--marvo-type-12);
}
.security-settings-card :deep([data-part='error-text']) {
  color: var(--text-danger);
  font-size: var(--marvo-type-12);
}
.security-settings-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.security-settings-success {
  margin: 0;
  color: var(--text-accent);
  font-size: var(--marvo-type-13);
}
.security-settings-copy,
.totp-enrollment > p {
  margin: 0;
  color: var(--text-secondary);
  font-size: var(--marvo-type-13);
}
.totp-enrollment {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.totp-enrollment-content {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 22px;
}
.totp-qr {
  display: flex;
  width: fit-content;
  max-width: 100%;
  padding: 8px;
  overflow: hidden;
  border: 1px solid var(--border-primary);
  border-radius: var(--marvo-radius);
  background: #fff;
}
.totp-qr :deep(svg) {
  display: block;
  width: min(192px, 100%);
  height: auto;
}
.totp-secret {
  display: flex;
  align-items: flex-start;
  flex-direction: column;
  gap: 7px;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  code {
    max-width: 100%;
    overflow-wrap: anywhere;
    color: var(--text-primary);
    font-size: var(--marvo-type-13);
  }
}
.security-settings-loading {
  min-height: 180px;
}
@media (max-width: 620px) {
  .security-settings-card {
    padding: 16px;
  }
  .totp-enrollment-content {
    grid-template-columns: 1fr;
  }
  .totp-qr {
    margin-inline: auto;
  }
  .security-settings-actions {
    align-items: stretch;
    flex-direction: column-reverse;
    .admin-btn {
      justify-content: center;
    }
  }
}
</style>
