import { expect, test } from '@playwright/test'
import { approveDevice, platformContext, workspacePath } from './helpers'

test('Android 原生层会收到用户最终生效的明暗主题', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  let darkMode = true

  await page.route('**/api/user/*/theme', (route) =>
    route.fulfill({
      json: {
        darkMode,
        fontSize: 14,
        contentFontSize: 15,
        contentLineHeight: 1.8,
        contentWidth: 'full',
        accentColor: '#4f46e5',
      },
    }),
  )

  await approveDevice(page, 'Playwright Android theme bridge')
  await page.addInitScript(() => {
    const messages: string[] = []
    const browserUserAgent = navigator.userAgent
    Object.defineProperty(navigator, 'userAgent', {
      configurable: true,
      get: () => `${browserUserAgent} MarvoAndroid/0.1.3`,
    })
    const native = {
      onmessage: null as null | ((event: { data: string }) => void),
      postMessage(raw: string) {
        const request = JSON.parse(raw) as { id: string; method: string; payload?: { style?: string } }
        if (request.method === 'statusBar' && request.payload?.style) messages.push(request.payload.style)
        queueMicrotask(() => native.onmessage?.({ data: JSON.stringify({ id: request.id, ok: true, result: null }) }))
      },
    }
    Object.assign(window, { __marvoThemeMessages: messages, __marvoNative: native })
  })
  await page.reload()
  await expect.poll(() => page.evaluate(() => document.documentElement.dataset.colorScheme)).toBe('dark')
  await expect
    .poll(() =>
      page.evaluate(() => (window as typeof window & { __marvoThemeMessages: string[] }).__marvoThemeMessages),
    )
    .toContain('dark')

  await page.goto(workspacePath('/login?mode=admin'))
  await expect.poll(() => page.evaluate(() => document.documentElement.dataset.colorScheme)).toBe('dark')
  await page.goto(workspacePath())
  await expect.poll(() => page.evaluate(() => document.documentElement.dataset.colorScheme)).toBe('dark')

  darkMode = false
  await page.reload()
  await expect.poll(() => page.evaluate(() => document.documentElement.dataset.colorScheme)).toBe('light')
  await expect
    .poll(() =>
      page.evaluate(() => (window as typeof window & { __marvoThemeMessages: string[] }).__marvoThemeMessages),
    )
    .toContain('light')

  await page.goto(workspacePath('/login?mode=admin'))
  await expect.poll(() => page.evaluate(() => document.documentElement.dataset.colorScheme)).toBe('light')
})

test('APP 返回协议按浮层、业务子页和工作区根页逐层处理', async ({ page }) => {
  await approveDevice(page, 'Playwright Android back protocol')

  expect(await page.evaluate(() => (window as any).__marvoHandleBack())).toBe(false)

  await page.getByRole('button', { name: 'APP', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Android APP' })).toBeVisible()
  expect(await page.evaluate(() => (window as any).__marvoHandleBack())).toBe(true)
  await expect(page.getByRole('dialog', { name: 'Android APP' })).toBeHidden()

  await page.goto(workspacePath('/trash'))
  await expect(page.locator('.trash-page')).toBeVisible()
  expect(await page.evaluate(() => (window as any).__marvoHandleBack())).toBe(true)
  await expect(page).toHaveURL(new RegExp(`${workspacePath()}$`))
  expect(await page.evaluate(() => (window as any).__marvoHandleBack())).toBe(false)

  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByTitle('展开列表').click()
  await expect(page.locator('.dsh-sider')).not.toHaveClass(/collapsed/)
  expect(await page.evaluate(() => (window as any).__marvoHandleBack())).toBe(true)
  await expect(page.locator('.dsh-sider')).toHaveClass(/collapsed/)
})

test('Android APP 仅在首页连续返回两次时通过原生桥退出任务', async ({ page }) => {
  await approveDevice(page, 'Playwright Android double back exit')
  await page.addInitScript(() => {
    const methods: string[] = []
    const browserUserAgent = navigator.userAgent
    Object.defineProperty(navigator, 'userAgent', {
      configurable: true,
      get: () => `${browserUserAgent} MarvoAndroid/0.1.3`,
    })
    const native = {
      onmessage: null as null | ((event: { data: string }) => void),
      postMessage(raw: string) {
        const request = JSON.parse(raw) as { id: string; method: string }
        methods.push(request.method)
        queueMicrotask(() => native.onmessage?.({ data: JSON.stringify({ id: request.id, ok: true, result: null }) }))
      },
    }
    Object.assign(window, { __marvoNativeMethods: methods, __marvoNative: native })
  })
  await page.reload()
  await expect(page.locator('.dsh')).toBeVisible()

  const nativeMethods = () =>
    page.evaluate(() => (window as typeof window & { __marvoNativeMethods: string[] }).__marvoNativeMethods)

  expect(await page.evaluate(() => window.marvo?.back())).toBe(true)
  await expect.poll(nativeMethods).toContain('toast')

  await page.waitForTimeout(2_100)
  expect(await page.evaluate(() => window.marvo?.back())).toBe(true)
  await expect.poll(async () => (await nativeMethods()).filter((method) => method === 'toast').length).toBe(2)
  expect((await nativeMethods()).filter((method) => method === 'exitApp')).toHaveLength(0)

  expect(await page.evaluate(() => window.marvo?.back())).toBe(true)
  await expect.poll(nativeMethods).toContain('exitApp')
})

test('认证检查断网不会把已批准设备重定向到登录页', async ({ page }) => {
  await approveDevice(page, 'Playwright Android offline auth')
  const workspace = workspacePath()
  await page.route('**/api/user/*/auth/token*', (route) => route.abort('internetdisconnected'))
  await page.reload()

  await expect(page).toHaveURL(new RegExp(`${workspace}$`))
  await expect(page.getByRole('heading', { name: '暂时无法连接 Marvo' })).toBeVisible()
  await expect(page.getByText('设备授权状态没有改变')).toBeVisible()
})

test('认证检查临时限流不会被误判为设备授权失效', async ({ page }) => {
  await approveDevice(page, 'Playwright Android throttled auth')
  const workspace = workspacePath()
  await page.route('**/api/user/*/auth/token*', (route) =>
    route.fulfill({ status: 429, contentType: 'application/json', body: '{"error":"try later"}' }),
  )
  await page.reload()

  await expect(page).toHaveURL(new RegExp(`${workspace}$`))
  await expect(page.getByRole('heading', { name: '暂时无法连接 Marvo' })).toBeVisible()
})

test('平台管理员可以选择并发布通用 Android APK', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })

  await page.route('**/api/admin/android/release', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"not published"}' })
      return
    }
    expect(route.request().method()).toBe('PUT')
    const payload = route.request().postDataBuffer()?.toString('utf8') || ''
    expect(payload).toContain('Marvo-1.2.0.apk')
    expect(payload).toContain('首个 Android 版本')
    expect(payload).toContain('true')
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        release: {
          version_code: 12,
          version_name: '1.2.0',
          required: true,
          message: '首个 Android 版本',
        },
        published_at: '2026-08-14T00:00:00Z',
        apk_size: 1024,
      }),
    })
  })

  await page.goto('/admin/login')
  await page.getByPlaceholder('请输入密码').fill('e2e-admin-password')
  await page.getByRole('button', { name: '进入' }).click()
  await page.getByRole('link', { name: 'Android APP' }).click()
  await expect(page).toHaveURL('/admin/android')
  await expect(page).toHaveTitle('Android APP · Marvo')
  await expect(page.getByRole('button', { name: '展开编辑' })).toBeVisible()
  const releaseNotes = page.getByLabel('更新说明')
  await expect(releaseNotes).toHaveCSS('box-sizing', 'border-box')
  await expect(releaseNotes).toHaveCSS('border-radius', '8px')
  const releaseNotesBounds = await releaseNotes.boundingBox()
  const releaseFormBounds = await page.locator('.android-release-form').boundingBox()
  expect(releaseNotesBounds).not.toBeNull()
  expect(releaseFormBounds).not.toBeNull()
  expect(Math.abs(releaseNotesBounds!.width - releaseFormBounds!.width)).toBeLessThanOrEqual(1)
  const requiredUpdate = page.getByRole('checkbox', { name: /要求旧版本立即更新/ })
  const requiredIndicator = page.locator('.android-release-required-indicator')
  await expect(requiredUpdate).not.toBeChecked()
  await expect(requiredIndicator).toBeHidden()
  await page.getByText('要求旧版本立即更新').click()
  await expect(requiredUpdate).toBeChecked()
  await expect(requiredIndicator).toBeVisible()

  await page.locator('input[type="file"]').setInputFiles({
    name: 'Marvo-1.2.0.apk',
    mimeType: 'application/vnd.android.package-archive',
    buffer: Buffer.from('playwright APK placeholder'),
  })
  await page.getByLabel('更新说明').fill('首个 Android 版本')
  await page.getByRole('button', { name: '发布 APK' }).click()

  await expect(page.getByRole('status')).toHaveText('新版本已发布')
  await expect(page.getByText('1.2.0', { exact: true })).toBeVisible()
  await expect(page.getByText('版本代码 12')).toBeVisible()
  await expect(page.getByText('强制更新', { exact: true })).toBeVisible()
})

test('工作区 Android 入口提供扫码下载、直接下载与登录二维码', async ({ page }) => {
  await page.route('**/api/app/android/release', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ version_code: 12, version_name: '1.2.0', required: false, message: '' }),
    }),
  )
  await approveDevice(page, 'Playwright Android entry')
  await page.getByRole('button', { name: 'APP', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Android APP' })
  await expect(dialog).toBeVisible()
  const download = dialog.getByRole('button', { name: /下载 APK/ })
  await expect(download).toContainText('1.2.0')
  await download.click()
  await expect(dialog.getByText('使用 Android 设备扫码下载')).toBeVisible()
  await expect(dialog.locator('canvas')).toBeVisible()
  await expect(dialog.getByRole('link', { name: '直接下载' })).toHaveAttribute('href', '/api/app/android/apk')
  await dialog.getByRole('button', { name: '返回' }).click()

  await dialog.getByRole('button', { name: /登录 APP/ }).click()
  const userID = workspacePath().split('/')[2]
  expect(userID).toMatch(/^[0-9a-f]{20}$/)
  await expect(dialog.locator('code')).toHaveText(userID)
  await expect(dialog.locator('canvas')).toBeVisible()
  await expect(dialog).not.toContainText('marvo:bind')
  await expect(dialog).not.toContainText('二维码只包含当前用户 ID')
  await expect(dialog).toContainText('作为新设备提交申请')

  const bounds = await dialog.locator('.android-entry-qr-frame').boundingBox()
  expect(bounds).not.toBeNull()
  expect(bounds!.x).toBeGreaterThanOrEqual(0)
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width)
})

test('Android APP 壳内不显示网页端 APP 入口', async ({ page }) => {
  await page.addInitScript(() => {
    const browserUserAgent = navigator.userAgent
    Object.defineProperty(navigator, 'userAgent', {
      configurable: true,
      get: () => `${browserUserAgent} MarvoAndroid/1.0.0`,
    })
  })
  await approveDevice(page, 'Playwright Android shell marker')

  await expect(page.getByRole('button', { name: 'APP', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '智能体', exact: true })).toBeVisible()
  expect(await page.evaluate(() => typeof (window as any).marvo?.back)).toBe('function')
})

test('普通浏览器页面不暴露 Android 原生桥接对象', async ({ page }) => {
  await page.goto('/')
  expect(await page.evaluate(() => typeof (window as any).marvo)).toBe('undefined')
})

test('Android WebView 首次进入会自动申请设备并在审批后进入工作区', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  const platform = await platformContext()
  const password = 'android-binding-e2e-password'
  try {
    const created = await platform.post('/api/admin/users', {
      data: { name: 'Android 自动绑定', password },
    })
    expect(created.ok()).toBeTruthy()
    const user = (await created.json()).user as { id: string }

    const verified = await platform.post(`/api/user/${user.id}/auth/verify`, { data: { password } })
    expect(verified.ok()).toBeTruthy()
    expect((await verified.json()).authenticated).toBe(true)

    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'userAgent', {
        configurable: true,
        get: () => 'Mozilla/5.0 MarvoAndroid/0.1.0',
      })
      localStorage.setItem('marvo_local_device_id', 'android-e2e-device')
      localStorage.setItem('marvo_android_device_name', 'Marvo · Playwright Tablet')
    })
    await page.goto(`/user/${user.id}/login`)
    await expect(page.getByRole('heading', { name: '您正在访问 Android 自动绑定 的空间' })).toBeVisible()
    await expect(page.getByText('用户管理员正在审核您的设备')).toBeVisible()

    const pending = await platform.get(`/api/user/${user.id}/admin/requests`)
    expect(pending.ok()).toBeTruthy()
    const requests = (await pending.json()).requests as Array<{ id: string; device_name: string }>
    const application = requests.find(({ device_name }) => device_name === 'Marvo · Playwright Tablet')
    expect(application).toBeTruthy()
    const approved = await platform.post(`/api/user/${user.id}/admin/requests/${application!.id}/approve`)
    expect(approved.ok()).toBeTruthy()

    await expect(page).toHaveURL(`/user/${user.id}`, { timeout: 12_000 })
    await expect(page.locator('.dsh')).toBeVisible()
  } finally {
    await platform.dispose()
  }
})
