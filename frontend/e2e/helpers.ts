import { createHmac } from 'node:crypto'
import { expect, request, type APIRequestContext, type Locator, type Page } from '@playwright/test'

const backendURL = 'http://127.0.0.1:15090'
const approvedTestDeviceID = 'marvo-playwright-approved-device'
const platformPassword = 'e2e-admin-password'
const userPassword = 'e2e-user-password'
const userName = 'Playwright 用户空间'
let testUserID = ''

interface PlatformUser {
  id: string
  name: string
  status: 'setup' | 'active' | 'disabled'
}

export function workspacePath(path = '') {
  if (!testUserID) throw new Error('approveDevice must run before workspacePath')
  const suffix = !path || path === '/' ? '' : path.startsWith('/') ? path : `/${path}`
  return `/user/${testUserID}${suffix}`
}

export function workspaceURL(path = '') {
  return `http://127.0.0.1:15080${workspacePath(path)}`
}

export function workspaceAPI(path: string) {
  if (!testUserID) throw new Error('approveDevice must run before workspaceAPI')
  if (!path.startsWith('/api/')) throw new Error(`not an API path: ${path}`)
  return `/api/user/${testUserID}${path.slice('/api'.length)}`
}

export function workspaceAPIRegex(path: string) {
  if (!path.startsWith('/api/')) throw new Error(`not an API path: ${path}`)
  return `/api/user/[^/]+${path.slice('/api'.length)}`
}

export async function expectDialogTextRetainedDuringClose(
  page: Page,
  dialog: Locator,
  expectedText: string,
  close: () => Promise<void>,
) {
  const key = `dialog-${Date.now()}-${Math.random()}`
  await dialog.evaluate(
    (element, { expectedText: expected, key: stateKey }) => {
      const state = window as Window & { __marvoDialogCloseChecks?: Record<string, boolean> }
      const container = element.closest('[data-part="positioner"]') || element
      state.__marvoDialogCloseChecks ||= {}
      state.__marvoDialogCloseChecks[stateKey] = false
      const observer = new MutationObserver(() => {
        if (container.isConnected && !container.textContent?.includes(expected)) {
          state.__marvoDialogCloseChecks![stateKey] = true
        }
      })
      observer.observe(container, { childList: true, characterData: true, subtree: true })
      window.setTimeout(() => observer.disconnect(), 500)
    },
    { expectedText, key },
  )

  await close()
  await expect(dialog).toBeHidden()
  await page.waitForTimeout(180)
  const changed = await page.evaluate((stateKey) => {
    const state = window as Window & { __marvoDialogCloseChecks?: Record<string, boolean> }
    const result = state.__marvoDialogCloseChecks?.[stateKey] ?? false
    if (state.__marvoDialogCloseChecks) delete state.__marvoDialogCloseChecks[stateKey]
    return result
  }, key)
  expect(changed).toBe(false)
}

export async function platformContext() {
  const admin = await request.newContext({ baseURL: backendURL })
  const verify = await admin.post('/api/platform/auth/verify', { data: { password: platformPassword } })
  expect(verify.ok()).toBeTruthy()
  const { challenge_token: challengeToken } = await verify.json()
  const login = await admin.post('/api/platform/auth', { data: { challenge_token: challengeToken } })
  expect(login.ok()).toBeTruthy()
  return admin
}

async function ensureTestUser(admin: APIRequestContext) {
  const listed = await admin.get('/api/admin/users')
  expect(listed.ok()).toBeTruthy()
  const users = ((await listed.json()).users ?? []) as PlatformUser[]
  let user = users.find((candidate) => candidate.name === userName)
  if (!user) {
    const created = await admin.post('/api/admin/users', {
      data: { name: userName, password: userPassword },
    })
    expect(created.ok()).toBeTruthy()
    user = (await created.json()).user as PlatformUser
  }
  if (user.status === 'disabled') {
    const enabled = await admin.put(`/api/admin/users/${user.id}/status`, { data: { disabled: false } })
    expect(enabled.ok()).toBeTruthy()
    user = (await enabled.json()).user as PlatformUser
  }
  testUserID = user.id
  return user
}

function decodeBase32(value: string) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const character of value.toUpperCase().replace(/=+$/, '')) {
    const index = alphabet.indexOf(character)
    if (index < 0) throw new Error('invalid base32 TOTP secret')
    bits += index.toString(2).padStart(5, '0')
  }
  const bytes: number[] = []
  for (let index = 0; index + 8 <= bits.length; index += 8) {
    bytes.push(Number.parseInt(bits.slice(index, index + 8), 2))
  }
  return Buffer.from(bytes)
}

export function totpCode(secret: string) {
  const counter = Math.floor(Date.now() / 1000 / 30)
  const payload = Buffer.alloc(8)
  payload.writeBigUInt64BE(BigInt(counter))
  const digest = createHmac('sha1', decodeBase32(secret)).update(payload).digest()
  const offset = digest[digest.length - 1] & 0x0f
  const value =
    (((digest[offset] & 0x7f) << 24) | (digest[offset + 1] << 16) | (digest[offset + 2] << 8) | digest[offset + 3]) %
    1_000_000
  return String(value).padStart(6, '0')
}

async function loginUserAdministrator(
  admin: APIRequestContext,
  userID: string,
  password = userPassword,
  resetCredentials = true,
) {
  // Resetting only the management credentials gives isolated tests a known
  // password-only login. Approved device credentials are intentionally not
  // affected by this operation.
  if (resetCredentials) {
    const reset = await admin.post(`/api/admin/users/${userID}/credentials`, {
      data: { password },
    })
    expect(reset.ok()).toBeTruthy()
  }
  const verify = await admin.post(`/api/user/${userID}/auth/verify`, {
    data: { password },
  })
  expect(verify.ok()).toBeTruthy()
  const loginState = (await verify.json()) as {
    authenticated?: boolean
    challenge_token: string
  }
  expect(loginState.authenticated).toBe(true)
}

export async function approvedDeviceContext(
  admin: APIRequestContext,
  userID: string,
  password: string,
  localDeviceID: string,
) {
  await loginUserAdministrator(admin, userID, password, false)
  const device = await request.newContext({ baseURL: backendURL })
  const applied = await device.post(`/api/user/${userID}/auth/apply`, {
    data: {
      local_device_id: localDeviceID,
      device_name: localDeviceID,
      device_info: { user_agent: 'Playwright isolation client' },
    },
  })
  expect(applied.ok()).toBeTruthy()
  const application = (await applied.json()) as { request_id?: string; status?: string }
  expect(application.status).toBe('pending')
  expect(application.request_id).toBeTruthy()
  const approved = await admin.post(`/api/user/${userID}/admin/requests/${application.request_id}/approve`)
  expect(approved.ok()).toBeTruthy()
  const token = await device.get(`/api/user/${userID}/auth/token`, {
    params: { local_device_id: localDeviceID },
  })
  expect(token.ok()).toBeTruthy()
  expect((await token.json()).status).toBe('approved')
  return device
}

export async function authenticateUserAdministrator(page: Page) {
  const admin = await platformContext()
  const user = await ensureTestUser(admin)
  try {
    const reset = await admin.post(`/api/admin/users/${user.id}/credentials`, {
      data: { password: userPassword },
    })
    expect(reset.ok()).toBeTruthy()
  } finally {
    await admin.dispose()
  }
  const verify = await page.request.post(workspaceAPI('/api/auth/verify'), {
    data: { password: userPassword },
  })
  expect(verify.ok()).toBeTruthy()
  const loginState = (await verify.json()) as { authenticated?: boolean }
  expect(loginState.authenticated).toBe(true)
}

export async function approveDevice(page: Page, deviceName: string) {
  const admin = await platformContext()
  const user = await ensureTestUser(admin)
  await page.addInitScript((localDeviceID) => {
    localStorage.setItem('marvo_local_device_id', localDeviceID)
  }, approvedTestDeviceID)

  const tokenResponsePromise = page.waitForResponse(
    (response) => response.url().includes(`/api/user/${user.id}/auth/token`) && response.request().method() === 'GET',
  )
  await page.goto(workspacePath('/login'))
  const tokenResponse = await tokenResponsePromise
  const token = (await tokenResponse.json()) as { status?: string; request_id?: string }
  if (token.status === 'approved') {
    await expect(page).toHaveURL(workspaceURL())
    await stopActiveAgentRuns(page)
    await admin.dispose()
    return
  }

  if (token.status === 'pending' && token.request_id) {
    try {
      await loginUserAdministrator(admin, user.id)
      const approval = await admin.post(`/api/user/${user.id}/admin/requests/${token.request_id}/approve`)
      expect(approval.ok()).toBeTruthy()
    } finally {
      await admin.dispose()
    }
    await expect(page).toHaveURL(workspaceURL(), { timeout: 12_000 })
    await stopActiveAgentRuns(page)
    return
  }

  await page.getByPlaceholder('设备名称').fill(deviceName)
  await page.getByRole('button', { name: '申请权限' }).click()
  await expect(page.getByRole('heading', { name: `您正在访问 ${user.name} 的空间` })).toBeVisible()
  await expect(page.getByText('用户管理员正在审核您的设备')).toBeVisible()

  try {
    await loginUserAdministrator(admin, user.id)
    const pending = await admin.get(`/api/user/${user.id}/admin/requests`)
    const requests = (await pending.json()).requests as Array<{ id: string; device_name: string }>
    const target = requests.find((item) => item.device_name === deviceName)
    expect(target).toBeTruthy()
    const approval = await admin.post(`/api/user/${user.id}/admin/requests/${target!.id}/approve`)
    expect(approval.ok()).toBeTruthy()
  } finally {
    await admin.dispose()
  }

  await expect(page).toHaveURL(workspaceURL(), { timeout: 12_000 })
  await stopActiveAgentRuns(page)
}

async function stopActiveAgentRuns(page: Page) {
  const statusResponse = await page.request.get(workspaceAPI('/api/agent/session/status'))
  expect(statusResponse.ok()).toBeTruthy()
  const statuses = (await statusResponse.json()) as Record<string, { type?: string }>
  for (const [sessionID, status] of Object.entries(statuses)) {
    if (status.type !== 'busy' && status.type !== 'retry') continue
    const abortResponse = await page.request.post(
      workspaceAPI(`/api/agent/session/${encodeURIComponent(sessionID)}/abort`),
    )
    expect(abortResponse.ok()).toBeTruthy()
  }
}

export async function openSidebar(page: Page) {
  const toggle = page.getByTitle('展开列表')
  if (await toggle.isVisible()) await toggle.click()
}

export async function closeCompactSidebar(page: Page) {
  const overlay = page.locator('.dsh-overlay')
  if (await overlay.isVisible()) await page.getByTitle('收起列表').click()
}

export async function openAgentSessions(page: Page) {
  const trigger = page.getByRole('button', { name: '对话列表', exact: true })
  const desktopSessions = page.locator('.agent-chat-sessions')
  await expect(trigger.or(desktopSessions)).toBeVisible()
  if (await desktopSessions.isVisible()) return false
  const dialog = page.getByRole('dialog', { name: '对话列表' })
  if (!(await dialog.isVisible())) await trigger.click()
  await expect(dialog).toBeVisible()
  return true
}

export async function createLongAgentSession(page: Page, label: string) {
  const created = await page.request.post(workspaceAPI('/api/agent/session'))
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  for (let index = 0; index < 18; index++) {
    const prompt = await page.request.post(workspaceAPI(`/api/agent/session/${session.id}/prompt_async`), {
      data: {
        parts: [
          {
            type: 'text',
            text: `${label} ${index + 1} ${'滚动历史内容 '.repeat(12)}`,
          },
        ],
      },
    })
    expect(prompt.ok()).toBeTruthy()
  }
  const aborted = await page.request.post(workspaceAPI(`/api/agent/session/${session.id}/abort`))
  expect(aborted.ok()).toBeTruthy()
  return session.id
}
