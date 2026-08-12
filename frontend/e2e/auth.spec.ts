import { expect, test } from '@playwright/test'
import { platformContext, totpCode } from './helpers'

test('用户首次绑定身份验证器时显示本地生成的 TOTP 二维码', async ({ page }, testInfo) => {
  const platform = await platformContext()
  const password = 'totp-qrcode-e2e-password'
  try {
    const userName = `二维码验证 ${testInfo.project.name}`
    const created = await platform.post('/api/admin/users', {
      data: { name: userName, password },
    })
    expect(created.ok()).toBeTruthy()
    const user = (await created.json()).user as { id: string }

    await page.goto(`/user/${user.id}/login?mode=admin`)
    await page.getByPlaceholder('用户密码').fill(password)
    const verified = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/user/${user.id}/auth/verify`) && response.request().method() === 'POST',
    )
    await page.getByRole('button', { name: '下一步' }).click()
    const response = await verified
    expect(response.ok()).toBeTruthy()
    const setup = (await response.json()).totp_setup as { secret: string; uri: string }

    const uri = new URL(setup.uri)
    expect(uri.protocol).toBe('otpauth:')
    expect(uri.hostname).toBe('totp')
    expect(uri.searchParams.get('secret')).toBe(setup.secret)
    expect(uri.searchParams.get('issuer')).toBe('Marvo')
    expect(uri.searchParams.get('algorithm')).toBe('SHA1')
    expect(uri.searchParams.get('digits')).toBe('6')
    expect(uri.searchParams.get('period')).toBe('30')

    const qrCode = page.getByRole('img', { name: '身份验证器设置二维码' })
    await expect(qrCode).toBeVisible()
    await expect(qrCode.locator('svg')).toBeVisible()
    await expect(page.getByText(setup.secret, { exact: true })).toBeVisible()
    const bounds = await qrCode.boundingBox()
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width)

    await page.getByPlaceholder('6 位验证码').fill(totpCode(setup.secret))
    await page.getByRole('button', { name: '进入设备管理' }).click()
    await expect(page).toHaveURL(`/user/${user.id}/admin`)
    const account = page.locator('.admin-header-user')
    await expect(account).toHaveAttribute('aria-label', userName)
    await expect(account.locator('.admin-header-user-name')).toHaveText(userName)
    if (testInfo.project.name === 'chromium-landscape') {
      await expect(account.locator('.admin-header-user-name')).toBeVisible()
    } else {
      await account.click()
      await expect(page.locator('.admin-header-dropdown-identity')).toHaveText(userName)
    }
  } finally {
    await platform.dispose()
  }
})
