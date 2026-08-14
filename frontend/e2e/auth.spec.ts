import { expect, test, type Page } from '@playwright/test'
import { platformContext, totpCode } from './helpers'

async function openAdminNavigation(page: Page) {
  const trigger = page.getByRole('button', { name: '打开后台导航' })
  if (await trigger.isVisible()) {
    await trigger.click()
    const navigation = page.getByRole('navigation', { name: '后台导航' })
    await expect(navigation).toBeVisible()
    return navigation
  }
  return page.locator('.admin-sidebar-nav')
}

test('非法用户 ID 不会渲染用户后台或请求无作用域管理接口', async ({ page }) => {
  const unscopedUserAdminRequests: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path === '/api/admin/me' || path === '/api/admin/brand' || path === '/api/admin/space') {
      unscopedUserAdminRequests.push(path)
    }
  })

  await page.goto('/user/78164094-cb60-441a-af24-be5df372dc26/admin/settings')

  await expect(page).toHaveURL('/admin/login')
  await expect(page.getByRole('heading', { name: 'Marvo Admin' })).toBeVisible()
  expect(unscopedUserAdminRequests).toEqual([])
})

test('用户可在后台管理密码与可选身份验证器', async ({ page }, testInfo) => {
  const platform = await platformContext()
  const password = 'totp-security-e2e-password'
  const changedPassword = 'totp-security-e2e-password-changed'
  try {
    const userName = `安全设置 ${testInfo.project.name}`
    const created = await platform.post('/api/admin/users', {
      data: { name: userName, password },
    })
    expect(created.ok()).toBeTruthy()
    const user = (await created.json()).user as { id: string }

    await page.goto(`/user/${user.id}/login?mode=admin`)
    const loginHeading = page.getByRole('heading', { name: `您正在访问 ${userName} 的空间` })
    await expect(loginHeading).toBeVisible()
    await expect(loginHeading).toHaveCSS('white-space', 'nowrap')
    const [loginCardBounds, loginHeadingBounds] = await Promise.all([
      page.locator('.user-space-login-card').boundingBox(),
      loginHeading.boundingBox(),
    ])
    expect(loginCardBounds).not.toBeNull()
    expect(loginHeadingBounds).not.toBeNull()
    expect(loginCardBounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width - 20)
    expect(loginHeadingBounds!.height).toBeLessThanOrEqual(40)
    await expect(page).toHaveTitle(`用户后台登录 · ${userName} · Marvo`)
    await page.getByPlaceholder('用户密码').fill(password)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page).toHaveURL(`/user/${user.id}/admin`)
    await expect(page).toHaveTitle(`设备审批 · ${userName} · Marvo`)
    await expect(page.locator('.admin-sidebar-brand')).toContainText('Marvo')
    await expect(page.getByRole('heading', { name: '访问机制' })).toBeVisible()
    await expect(page.getByText('后台身份与工作区设备凭据相互独立')).toBeVisible()
    await expect
      .poll(() =>
        page.locator('.device-auth-explainer').evaluate((explainer) => {
          const tabs = document.querySelector('.admin-tabs')
          return !!tabs && !!(explainer.compareDocumentPosition(tabs) & Node.DOCUMENT_POSITION_FOLLOWING)
        }),
      )
      .toBe(true)
    const initialNavigation = await openAdminNavigation(page)
    await expect(
      page.locator('.admin-sidebar:visible .marvo-mark, .admin-mobile-nav-panel:visible .marvo-mark'),
    ).toBeVisible()
    await expect(initialNavigation.getByRole('link', { name: '智能体设置' })).toHaveAttribute(
      'href',
      `/user/${user.id}/admin/agent`,
    )
    const navigationClose = page.getByRole('button', { name: '关闭后台导航' })
    if (await navigationClose.isVisible()) await navigationClose.click()

    const account = page.locator('.admin-header-user')
    await expect(account).toHaveAttribute('aria-label', userName)
    await expect(account.locator('.admin-header-user-name')).toHaveText(userName)
    if (testInfo.project.name === 'chromium-landscape') {
      await expect(account.locator('.admin-header-user-name')).toBeVisible()
    } else {
      await account.click()
      await expect(page.locator('.admin-header-dropdown-identity')).toHaveText(userName)
      await account.click()
    }

    const spaceNavigation = await openAdminNavigation(page)
    await spaceNavigation.getByRole('link', { name: '空间信息' }).click()
    await expect(page).toHaveURL(`/user/${user.id}/admin/settings`)
    await expect(page).toHaveTitle(`空间信息 · ${userName} · Marvo`)
    await expect(page.getByRole('heading', { name: '空间占用' })).toBeVisible()
    await expect(page.locator('.user-space-usage-value')).toContainText(/B|KiB|MiB|GiB|TiB/)
    await expect(page.getByText('当前未设置空间容量上限')).toBeVisible()
    const brandField = page.getByLabel('品牌名称')
    await expect(brandField).toHaveValue('Marvo')
    await brandField.fill(`知识空间 ${testInfo.project.name}`)
    await page.getByRole('button', { name: '保存', exact: true }).click()
    await expect(page.getByRole('status')).toHaveText('品牌名称已保存')
    await expect(page.locator('.admin-sidebar-brand')).toContainText('Marvo')

    const securityNavigation = await openAdminNavigation(page)
    await securityNavigation.getByRole('link', { name: '安全设置' }).click()
    await expect(page).toHaveURL(`/user/${user.id}/admin/security`)
    await expect(page).toHaveTitle(`安全设置 · ${userName} · Marvo`)
    const authenticatorCard = page
      .locator('.security-settings-card')
      .filter({ has: page.getByRole('heading', { name: '身份验证器' }) })
    await expect(authenticatorCard.getByText('未绑定', { exact: true })).toBeVisible()

    await authenticatorCard.getByLabel('确认当前密码').fill(password)
    const setupResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/user/${user.id}/admin/security/totp`) &&
        !response.url().endsWith('/confirm') &&
        response.request().method() === 'POST',
    )
    await authenticatorCard.getByRole('button', { name: '生成绑定二维码' }).click()
    const setup = (await (await setupResponse).json()).totp_setup as { secret: string; uri: string }

    const uri = new URL(setup.uri)
    expect(uri.protocol).toBe('otpauth:')
    expect(uri.hostname).toBe('totp')
    expect(uri.searchParams.get('secret')).toBe(setup.secret)
    expect(uri.searchParams.get('issuer')).toBe('Marvo')
    expect(uri.searchParams.get('algorithm')).toBe('SHA1')
    expect(uri.searchParams.get('digits')).toBe('6')
    expect(uri.searchParams.get('period')).toBe('30')

    const qrCode = authenticatorCard.getByRole('img', { name: '身份验证器设置二维码' })
    await expect(qrCode).toBeVisible()
    await expect(qrCode.locator('svg')).toBeVisible()
    await expect(authenticatorCard.getByText(setup.secret, { exact: true })).toBeVisible()
    const bounds = await qrCode.boundingBox()
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width)

    const currentCode = totpCode(setup.secret)
    await authenticatorCard.getByLabel('6 位验证码').fill(currentCode)
    await authenticatorCard.getByRole('button', { name: '确认绑定' }).click()
    await expect(authenticatorCard.getByText('已绑定', { exact: true })).toBeVisible()

    const passwordCard = page
      .locator('.security-settings-card')
      .filter({ has: page.getByRole('heading', { name: '登录密码' }) })
    await passwordCard.getByLabel('当前密码').fill(password)
    await passwordCard.getByLabel('新密码', { exact: true }).fill(changedPassword)
    await passwordCard.getByLabel('确认新密码').fill(changedPassword)
    await passwordCard.getByRole('button', { name: '修改密码' }).click()
    await expect(passwordCard.getByRole('status')).toHaveText('密码已修改')
    await expect(authenticatorCard.getByText('已绑定', { exact: true })).toBeVisible()

    await authenticatorCard.getByLabel('确认当前密码').fill(changedPassword)
    await authenticatorCard.getByLabel('当前 6 位验证码').fill(currentCode)
    await authenticatorCard.getByRole('button', { name: '解绑身份验证器' }).click()
    await expect(authenticatorCard.getByText('未绑定', { exact: true })).toBeVisible()

    await account.click()
    await page.getByRole('button', { name: '退出登录' }).click()
    await expect(page).toHaveURL(new RegExp(`/user/${user.id}/login\\?`))
    await page.getByPlaceholder('用户密码').fill(changedPassword)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page).toHaveURL(`/user/${user.id}/admin`)

    const currentNavigation = await openAdminNavigation(page)
    const navigationLinks = currentNavigation.locator('a')
    const firstLinkBounds = await navigationLinks.nth(0).boundingBox()
    const secondLinkBounds = await navigationLinks.nth(1).boundingBox()
    expect(firstLinkBounds).not.toBeNull()
    expect(secondLinkBounds).not.toBeNull()
    expect(secondLinkBounds!.y - (firstLinkBounds!.y + firstLinkBounds!.height)).toBeGreaterThanOrEqual(6)
    if (await navigationClose.isVisible()) await navigationClose.click()

    await page.getByRole('button', { name: '进入工作区' }).click()
    const authorizationDialog = page.getByRole('dialog', { name: '授权当前设备' })
    await expect(authorizationDialog).toBeVisible()
    await expect(authorizationDialog).toContainText(
      '如果这是临时或公用设备，使用完后请返回后台撤回当前设备授权，并及时退出后台登录。',
    )
    const firstWorkspacePromise = page.context().waitForEvent('page')
    await authorizationDialog.getByRole('button', { name: '授权并进入' }).click()
    const firstWorkspace = await firstWorkspacePromise
    await expect(firstWorkspace).toHaveURL(`/user/${user.id}`)
    await expect(page).toHaveURL(`/user/${user.id}/admin`)
    await expect(authorizationDialog).toBeHidden()
    await firstWorkspace.close()

    await page.getByRole('button', { name: /已批准设备/ }).click()
    const localDeviceID = await page.evaluate(() => localStorage.getItem('marvo_local_device_id'))
    expect(localDeviceID).toBeTruthy()
    const currentDeviceRow = page.locator(`tbody tr[data-device-id="${localDeviceID}"]`)
    await expect(currentDeviceRow).toBeVisible()
    await currentDeviceRow.getByRole('button', { name: '编辑', exact: true }).click()
    await currentDeviceRow.getByLabel('设备名称').fill(`当前设备 ${testInfo.project.name}`)
    await currentDeviceRow.getByRole('button', { name: '保存', exact: true }).click()
    await expect(currentDeviceRow).toContainText(`当前设备 ${testInfo.project.name}`)

    const workspaceEntry = page.getByRole('link', { name: '进入工作区' })
    await expect(workspaceEntry).toHaveAttribute('target', '_blank')
    await expect(workspaceEntry).toHaveAttribute('rel', 'noopener noreferrer')
    const secondWorkspacePromise = page.context().waitForEvent('page')
    await workspaceEntry.click()
    const secondWorkspace = await secondWorkspacePromise
    await expect(secondWorkspace).toHaveURL(`/user/${user.id}`)
    await expect(page).toHaveURL(`/user/${user.id}/admin`)
    await expect(authorizationDialog).toBeHidden()
    await secondWorkspace.close()
  } finally {
    await platform.dispose()
  }
})
