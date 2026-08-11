import { expect, request, type Page } from '@playwright/test'

const backendURL = 'http://127.0.0.1:15090'
const approvedTestDeviceID = 'marvo-playwright-approved-device'

export async function approveDevice(page: Page, deviceName: string) {
  await page.addInitScript((localDeviceID) => {
    localStorage.setItem('marvo_local_device_id', localDeviceID)
  }, approvedTestDeviceID)

  const tokenResponsePromise = page.waitForResponse(
    (response) => response.url().includes('/api/auth/token') && response.request().method() === 'GET',
  )
  await page.goto('/login')
  const tokenResponse = await tokenResponsePromise
  const token = (await tokenResponse.json()) as { status?: string }
  if (token.status === 'approved') {
    await expect(page).toHaveURL('http://127.0.0.1:15080/')
    await stopActiveAgentRuns(page)
    return
  }

  await page.getByPlaceholder('设备名称').fill(deviceName)
  await page.getByRole('button', { name: '申请访问' }).click()
  await expect(page.getByRole('heading', { name: '等待审批' })).toBeVisible()

  const admin = await request.newContext({ baseURL: backendURL })
  try {
    const verify = await admin.post('/api/auth/verify', { data: { password: 'e2e-admin-password' } })
    expect(verify.ok()).toBeTruthy()
    const { challenge_token: challengeToken } = await verify.json()
    const login = await admin.post('/api/auth', { data: { challenge_token: challengeToken } })
    expect(login.ok()).toBeTruthy()
    const pending = await admin.get('/api/admin/requests')
    const requests = (await pending.json()).requests as Array<{ id: string; device_name: string }>
    const target = requests.find((item) => item.device_name === deviceName)
    expect(target).toBeTruthy()
    const approval = await admin.post(`/api/admin/requests/${target!.id}/approve`)
    expect(approval.ok()).toBeTruthy()
  } finally {
    await admin.dispose()
  }

  await expect(page).toHaveURL('http://127.0.0.1:15080/', { timeout: 12_000 })
  await stopActiveAgentRuns(page)
}

async function stopActiveAgentRuns(page: Page) {
  const statusResponse = await page.request.get('/api/agent/session/status')
  expect(statusResponse.ok()).toBeTruthy()
  const statuses = (await statusResponse.json()) as Record<string, { type?: string }>
  for (const [sessionID, status] of Object.entries(statuses)) {
    if (status.type !== 'busy' && status.type !== 'retry') continue
    const abortResponse = await page.request.post(`/api/agent/session/${encodeURIComponent(sessionID)}/abort`)
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

export async function createLongAgentSession(page: Page, label: string) {
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  for (let index = 0; index < 18; index++) {
    const prompt = await page.request.post(`/api/agent/session/${session.id}/prompt_async`, {
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
  const aborted = await page.request.post(`/api/agent/session/${session.id}/abort`)
  expect(aborted.ok()).toBeTruthy()
  return session.id
}
