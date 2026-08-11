import { expect, test } from '@playwright/test'
import { approveDevice } from './helpers'

test('1366×768 下 Agent 设置始终显示保存操作区', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright Agent settings viewport')
  await page.goto('/agent')
  await page.getByRole('button', { name: '设置', exact: true }).click()

  const saveButton = page.getByRole('button', { name: '保存设置' })
  await expect(saveButton).toBeVisible()
  const bounds = await saveButton.boundingBox()
  expect(bounds).not.toBeNull()
  expect(bounds!.y).toBeGreaterThanOrEqual(0)
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(768)
})

test('智能体样式设置切换并记忆浮动按钮与内容右侧栏', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright Agent display mode')
  await page.goto('/agent')
  await page.getByRole('button', { name: '设置', exact: true }).click()

  await expect(page.getByRole('tab', { name: '样式' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await page
    .locator('.agent-display-mode-item')
    .filter({ has: page.locator('input[value="sidebar"]') })
    .click()
  await expect(page.getByRole('radio', { name: /^内容右侧栏/ })).toBeChecked()
  await expect(page.getByRole('button', { name: '保存设置' })).toBeEnabled()
  await page.getByRole('tab', { name: '模型' }).click()
  await expect(page.getByRole('heading', { name: '有未保存的设置' })).toBeVisible()
  await page.getByRole('button', { name: '继续编辑' }).click()
  await expect(page.getByRole('tab', { name: '样式' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('radio', { name: /^内容右侧栏/ })).toBeChecked()
  await page.getByRole('tab', { name: '模型' }).click()
  await page.getByRole('button', { name: '保存并切换' }).click()
  await expect(page.getByRole('tab', { name: '模型' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await page.getByRole('button', { name: '取消' }).click()
  await page.goto('/')
  await expect(page.locator('.agent-side-panel')).toBeVisible()
  await expect(page.locator('.agent-fab')).toHaveCount(0)
  await page.reload()
  await expect(page.locator('.agent-side-panel')).toBeVisible()
  await page.setViewportSize({ width: 800, height: 1000 })
  await expect(page.locator('.agent-side-panel')).toHaveCount(0)
  await expect(page.locator('.agent-fab')).toBeVisible()
  await page.setViewportSize({ width: 1366, height: 768 })
  await expect(page.locator('.agent-side-panel')).toBeVisible()

  await page.goto('/agent')
  await expect(page.locator('.agent-side-panel')).toHaveCount(0)
  await page.getByRole('button', { name: '设置', exact: true }).click()
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await page
    .locator('.agent-display-mode-item')
    .filter({ has: page.locator('input[value="floating"]') })
    .click()
  await expect(page.getByRole('radio', { name: /^浮动按钮/ })).toBeChecked()
  await page.getByRole('tab', { name: '模型' }).click()
  await expect(page.getByRole('heading', { name: '有未保存的设置' })).toBeVisible()
  await page.getByRole('button', { name: '放弃并切换' }).click()
  await expect(page.getByRole('tab', { name: '模型' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('tab', { name: '样式' }).click()
  await expect(page.getByRole('radio', { name: /^内容右侧栏/ })).toBeChecked()
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await page
    .locator('.agent-display-mode-item')
    .filter({ has: page.locator('input[value="floating"]') })
    .click()
  await page.getByRole('button', { name: '保存设置' }).click()
  await page.goto('/')
  await expect(page.locator('.agent-fab')).toBeVisible()
  await expect(page.locator('.agent-side-panel')).toHaveCount(0)
})

test('智能体设置可连接 API Key 与 OAuth 提供商并即时刷新模型', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright Agent provider connections')
  await page.goto('/agent')
  await page.getByRole('button', { name: '设置', exact: true }).click()
  await page.getByRole('tab', { name: '提供商' }).click()

  await expect(page.getByText('连接和断开操作立即生效，模型列表会自动刷新，无需另行保存。')).toBeVisible()
  await expect(page.locator('.agent-settings-footer')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toHaveCount(0)

  const providerInput = () => page.getByRole('combobox', { name: '选择提供商' })
  const selectProvider = async (name: string) => {
    const input = providerInput()
    await input.click()
    await input.fill(name)
    const option = page.locator('.provider-picker-item').filter({ hasText: name })
    await expect(option).toBeVisible()
    await option.click()
  }

  await expect(page.locator('.provider-connected-item').filter({ hasText: 'E2E Provider' })).toHaveCount(1)
  await providerInput().click()
  await providerInput().fill('E2E Provider')
  await expect(page.locator('.provider-picker-item').filter({ hasText: 'E2E Provider' })).toHaveCount(0)
  await providerInput().press('Escape')

  await selectProvider('OpenAI')
  await expect(page.getByText('ChatGPT Pro/Plus (browser)', { exact: true })).toHaveCount(0)
  await expect(page.getByText('ChatGPT Pro/Plus (headless)', { exact: true })).toBeVisible()
  await expect(page.getByText('Manually enter API Key', { exact: true })).toBeVisible()

  await selectProvider('E2E API Key Provider')
  await expect(providerInput()).toBeVisible()
  await expect(page.getByRole('button', { name: '返回提供商', exact: true })).toHaveCount(0)
  await expect(page.getByText('API Key', { exact: true })).toHaveCount(1)
  await expect(page.locator('.provider-picker-action')).toHaveCount(0)
  const deployment = page.getByRole('combobox', { name: 'Test deployment' })
  await expect(deployment).toHaveAttribute('data-scope', 'select')
  await deployment.click()
  await page.getByRole('option', { name: /Local/ }).click()
  await page.getByLabel('Test endpoint').fill('http://localhost:9999')
  await page.getByLabel('API Key').fill('e2e-api-key')
  await page.getByRole('button', { name: '连接提供商', exact: true }).click()
  await expect(page.locator('.provider-connected-item').filter({ hasText: 'E2E API Key Provider' })).toHaveCount(1)
  await providerInput().click()
  await providerInput().fill('E2E API Key Provider')
  await expect(page.locator('.provider-picker-item').filter({ hasText: 'E2E API Key Provider' })).toHaveCount(0)
  await providerInput().press('Escape')

  let nativeDialogs = 0
  page.on('dialog', async (dialog) => {
    nativeDialogs++
    await dialog.dismiss()
  })
  await page.getByRole('button', { name: '断开 E2E API Key Provider', exact: true }).click()
  await expect(page.getByRole('heading', { name: '断开提供商' })).toBeVisible()
  await page.getByRole('button', { name: '确认断开', exact: true }).click()
  await expect(providerInput()).toBeVisible()
  expect(nativeDialogs).toBe(0)

  await selectProvider('E2E Device OAuth Provider')
  await page.getByRole('button', { name: '开始授权', exact: true }).click()
  await expect(page.getByText('E2E-CODE', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '打开授权页面' })).toHaveAttribute(
    'href',
    'https://example.com/e2e-device',
  )
  await expect(page.locator('.provider-connected-item').filter({ hasText: 'E2E Device OAuth Provider' })).toHaveCount(1)

  await selectProvider('E2E Code OAuth Provider')
  await page.getByRole('button', { name: '开始授权', exact: true }).click()
  await page.getByLabel('授权码').fill('e2e-oauth-code')
  await page.getByRole('button', { name: '完成连接', exact: true }).click()
  await expect(page.locator('.provider-connected-item').filter({ hasText: 'E2E Code OAuth Provider' })).toHaveCount(1)

  await page.getByRole('tab', { name: '模型' }).click()
  const modelInput = page.getByRole('combobox', { name: '选择智能体模型' })
  await modelInput.click()
  await modelInput.fill('E2E Code OAuth Provider Model')
  await expect(page.locator('.agent-model-item').filter({ hasText: 'E2E Code OAuth Provider Model' })).toBeVisible()

  const saveButton = page.getByRole('button', { name: '保存设置' })
  const bounds = await saveButton.boundingBox()
  expect(bounds).not.toBeNull()
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(768)
})

test('提供商选择器在竖屏中不越界且即时操作无需底部保存', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'chromium-landscape')
  await approveDevice(page, 'Playwright portrait provider settings')
  await page.goto('/agent')
  await page.getByRole('button', { name: '设置', exact: true }).click()
  await page.getByRole('tab', { name: '提供商' }).click()

  const search = page.getByRole('combobox', { name: '选择提供商' })
  const closeButton = page.getByRole('button', { name: '关闭智能体设置' })
  await expect(search).toBeVisible()
  await expect(closeButton).toBeVisible()
  await expect(page.locator('.agent-settings-footer')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toHaveCount(0)

  await search.click()
  await search.fill('E2E API Key Provider')
  const option = page.locator('.provider-picker-item').filter({ hasText: 'E2E API Key Provider' })
  await expect(option).toBeVisible()

  const [searchBounds, optionBounds, closeBounds] = await Promise.all([
    search.boundingBox(),
    option.boundingBox(),
    closeButton.boundingBox(),
  ])
  for (const bounds of [searchBounds, optionBounds, closeBounds]) {
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390)
    expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(844)
  }
})

test('1366×768 触摸缩放浮动 Agent 窗口不会溢出视口顶部', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright floating Agent bounds')
  const floatButton = page.locator('.agent-fab')
  await expect(floatButton).toHaveAccessibleName('打开智能体')
  await expect(floatButton).toHaveAttribute('aria-expanded', 'false')
  await floatButton.click()

  const panel = page.locator('.agent-float-desktop')
  const resizeHandle = panel.locator('.agent-float-resize-handle')
  await expect(panel).toBeVisible()
  await expect(floatButton).toHaveAccessibleName('关闭智能体')
  await expect(floatButton).toHaveAttribute('aria-expanded', 'true')
  await floatButton.click()
  await expect(panel).toBeHidden()
  await expect(floatButton).toHaveAccessibleName('打开智能体')
  await expect(floatButton).toHaveAttribute('aria-expanded', 'false')
  await floatButton.click()
  await expect(panel).toBeVisible()
  await expect.poll(() => panel.evaluate((element) => getComputedStyle(element).transform)).toBe('none')
  const handleBounds = await resizeHandle.boundingBox()
  const initialPanelBounds = await panel.boundingBox()
  expect(handleBounds).not.toBeNull()
  expect(initialPanelBounds).not.toBeNull()
  await expect(resizeHandle).toHaveCSS('width', '44px')
  await expect(resizeHandle).toHaveCSS('height', '44px')
  expect(handleBounds!.width).toBeGreaterThanOrEqual(43.9)
  expect(handleBounds!.height).toBeGreaterThanOrEqual(43.9)
  await expect(resizeHandle).toHaveCSS('touch-action', 'none')

  const client = await page.context().newCDPSession(page)
  await client.send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 })
  const touchX = handleBounds!.x + handleBounds!.width / 2
  const touchY = handleBounds!.y + handleBounds!.height / 2
  await client.send('Input.dispatchTouchEvent', {
    type: 'touchStart',
    touchPoints: [{ x: touchX, y: touchY, radiusX: 8, radiusY: 8 }],
  })
  await client.send('Input.dispatchTouchEvent', {
    type: 'touchMove',
    touchPoints: [{ x: touchX - 80, y: 1, radiusX: 8, radiusY: 8 }],
  })
  await client.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })

  const panelBounds = await panel.boundingBox()
  expect(panelBounds).not.toBeNull()
  expect(panelBounds!.width).toBeGreaterThan(initialPanelBounds!.width)
  expect(panelBounds!.y).toBeGreaterThanOrEqual(15)
  expect(panelBounds!.y + panelBounds!.height).toBeLessThanOrEqual(681)
})
