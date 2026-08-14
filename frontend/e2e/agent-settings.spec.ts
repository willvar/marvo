import { expect, test } from '@playwright/test'
import {
  approveDevice,
  authenticateUserAdministrator,
  expectDialogTextRetainedDuringClose,
  workspacePath,
} from './helpers'

test('智能体设置在单页展示所有分区并始终显示统一保存', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await authenticateUserAdministrator(page)
  await page.goto(workspacePath('/admin/agent'))

  const saveButton = page.getByRole('button', { name: '保存设置' })
  await expect(page.locator('.agent-settings-page-heading').getByRole('button', { name: '保存设置' })).toBeVisible()
  await expect(page.getByRole('tablist')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '当前设备的智能体布局' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '提供商', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '模型', exact: true })).toBeVisible()
  await expect(page.locator('.agent-variant-group')).toContainText('关闭')
  await expect(page.locator('.agent-variant-group')).not.toContainText('none')
  await expect(page.getByRole('heading', { name: '联网搜索' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '全局提示词' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '个性化规则' })).toBeVisible()

  const actionButtons = page.locator('.agent-settings-page-actions .agent-settings-action')
  const [discardBounds, saveBounds] = await Promise.all([
    actionButtons.nth(0).boundingBox(),
    actionButtons.nth(1).boundingBox(),
  ])
  expect(discardBounds).not.toBeNull()
  expect(saveBounds).not.toBeNull()
  expect(Math.abs(discardBounds!.width - saveBounds!.width)).toBeLessThanOrEqual(1)
  expect(Math.abs(discardBounds!.height - saveBounds!.height)).toBeLessThanOrEqual(1)

  await expect(saveButton).toBeVisible()
})

test('Exa API Key 只写入后端且已保存密钥不回显浏览器', async ({ page }) => {
  await authenticateUserAdministrator(page)
  await page.goto(workspacePath('/admin/agent'))

  const input = page.getByLabel('Exa API Key')
  const status = page.locator('.agent-exa-status')
  const saveButton = page.getByRole('button', { name: '保存设置' })
  await expect(input).toHaveAttribute('type', 'password')
  await expect(input).toHaveValue('')

  const key = 'e2e-exa-api-key'
  await input.fill(key)
  await expect(status).toContainText(/等待保存|等待替换/)
  const saveResponsePromise = page.waitForResponse(
    (response) => response.url().endsWith('/agent/settings') && response.request().method() === 'PUT',
  )
  await saveButton.click()
  const saveResponse = await saveResponsePromise
  const saveResponseBody = await saveResponse.text()
  expect(JSON.parse(saveResponseBody).exa_configured).toBe(true)
  expect(saveResponseBody).not.toContain(key)
  await expect(status).toHaveText('已配置')
  await expect(input).toHaveValue('')
  await expect(input).toHaveAttribute('placeholder', '已配置；输入新密钥可替换')
  await expect(page.locator('body')).not.toContainText(key)

  await page.reload()
  await expect(input).toHaveValue('')
  await expect(status).toHaveText('已配置')
  await page.getByRole('button', { name: '移除密钥' }).click()
  await expect(status).toHaveText('等待移除')
  await expect(page.getByRole('button', { name: '保留密钥' })).toBeVisible()
  await saveButton.click()
  await expect(status).toHaveText('匿名额度')
  await expect(page.getByRole('button', { name: '移除密钥' })).toHaveCount(0)
})

test('2560×1440 下设置页用满可用宽度并对齐双栏板块', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 2560, height: 1440 })
  await authenticateUserAdministrator(page)
  await page.goto(workspacePath('/admin/agent'))

  const settingsPage = page.locator('.agent-settings-page')
  const styleLayout = page.locator('.agent-style-layout')
  const runtimeLayout = page.locator('.agent-runtime-grid')
  const advancedLayout = page.locator('.agent-advanced-grid')
  await expect(settingsPage).toBeVisible()
  const [pageBounds, styleColumns, runtimeColumns, advancedColumns] = await Promise.all([
    settingsPage.boundingBox(),
    styleLayout.evaluate((element) => getComputedStyle(element).gridTemplateColumns),
    runtimeLayout.evaluate((element) => getComputedStyle(element).gridTemplateColumns),
    advancedLayout.evaluate((element) => getComputedStyle(element).gridTemplateColumns),
  ])
  expect(pageBounds).not.toBeNull()
  expect(pageBounds!.width).toBeGreaterThanOrEqual(2250)
  expect(styleColumns.split(' ')).toHaveLength(2)
  expect(runtimeColumns.split(' ')).toHaveLength(2)
  expect(advancedColumns.split(' ')).toHaveLength(2)

  const [providerBounds, modelBounds] = await Promise.all([
    page.locator('.provider-list-section').boundingBox(),
    page.locator('.agent-model-section').boundingBox(),
  ])
  expect(providerBounds).not.toBeNull()
  expect(modelBounds).not.toBeNull()
  expect(Math.abs(providerBounds!.height - modelBounds!.height)).toBeLessThanOrEqual(1)
  const advancedCards = page.locator('.agent-advanced-grid > .agent-settings-section')
  const [promptBounds, rulesBounds] = await Promise.all([
    advancedCards.nth(0).boundingBox(),
    advancedCards.nth(1).boundingBox(),
  ])
  expect(promptBounds).not.toBeNull()
  expect(rulesBounds).not.toBeNull()
  expect(Math.abs(promptBounds!.height - rulesBounds!.height)).toBeLessThanOrEqual(1)
  await expect(page.locator('.agent-settings-prompt')).toHaveCSS('max-height', 'none')
})

test('一次保存同时提交本机布局与后端智能体设置', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await authenticateUserAdministrator(page)
  await page.goto(workspacePath('/admin/agent'))

  const prompt = page.locator('.agent-settings-prompt')
  const saveButton = page.getByRole('button', { name: '保存设置' })
  await expect(prompt).toBeVisible()
  await expect(saveButton).toBeDisabled()
  const originalPrompt = await prompt.inputValue()
  const floating = page.getByRole('radio', { name: /^浮动按钮/ })
  const sidebar = page.getByRole('radio', { name: /^内容右侧栏/ })
  const floatingOption = page
    .locator('.agent-display-mode-item')
    .filter({ has: page.locator('input[value="floating"]') })
  const sidebarOption = page.locator('.agent-display-mode-item').filter({ has: page.locator('input[value="sidebar"]') })
  const originalMode = (await floating.isChecked()) ? 'floating' : 'sidebar'
  await (originalMode === 'floating' ? sidebarOption : floatingOption).click()
  await prompt.fill(`${originalPrompt}\nPlaywright 统一保存`.trim())

  await expect(saveButton).toBeEnabled()
  await saveButton.click()
  await expect(saveButton).toBeDisabled()
  await page.reload()
  await expect(prompt).toHaveValue(`${originalPrompt}\nPlaywright 统一保存`.trim())
  await expect(originalMode === 'floating' ? sidebar : floating).toBeChecked()

  await (originalMode === 'floating' ? floatingOption : sidebarOption).click()
  await prompt.fill(originalPrompt)
  await saveButton.click()
  await expect(saveButton).toBeDisabled()
})

test('智能体样式设置切换并记忆浮动按钮与内容右侧栏', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright Agent display mode')
  await authenticateUserAdministrator(page)
  await page.goto(workspacePath('/admin/agent'))

  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await page
    .locator('.agent-display-mode-item')
    .filter({ has: page.locator('input[value="sidebar"]') })
    .click()
  await expect(page.getByRole('radio', { name: /^内容右侧栏/ })).toBeChecked()
  await expect(page.getByRole('button', { name: '保存设置' })).toBeEnabled()
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await page.goto(workspacePath())
  await expect(page.locator('.agent-side-panel')).toBeVisible()
  await expect(page.locator('.agent-fab')).toHaveCount(0)
  await page.reload()
  await expect(page.locator('.agent-side-panel')).toBeVisible()
  await page.setViewportSize({ width: 800, height: 1000 })
  await expect(page.locator('.agent-side-panel')).toHaveCount(0)
  await expect(page.locator('.agent-fab')).toBeVisible()
  await page.setViewportSize({ width: 1366, height: 768 })
  await expect(page.locator('.agent-side-panel')).toBeVisible()
  const synchronizedWorkspace = await page.context().newPage()
  await synchronizedWorkspace.setViewportSize({ width: 1366, height: 768 })
  await synchronizedWorkspace.goto(workspacePath())
  await expect(synchronizedWorkspace.locator('.agent-side-panel')).toBeVisible()

  await page.goto(workspacePath('/agent'))
  await expect(page.locator('.agent-side-panel')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '设置', exact: true })).toHaveCount(0)
  await page.goto(workspacePath('/admin/agent'))
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await page
    .locator('.agent-display-mode-item')
    .filter({ has: page.locator('input[value="floating"]') })
    .click()
  await expect(page.getByRole('radio', { name: /^浮动按钮/ })).toBeChecked()
  await page.getByRole('button', { name: '放弃修改' }).click()
  await expect(page.getByRole('radio', { name: /^内容右侧栏/ })).toBeChecked()
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await page
    .locator('.agent-display-mode-item')
    .filter({ has: page.locator('input[value="floating"]') })
    .click()
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(synchronizedWorkspace.locator('.agent-fab')).toBeVisible()
  await expect(synchronizedWorkspace.locator('.agent-side-panel')).toHaveCount(0)
  await synchronizedWorkspace.close()
  await page.goto(workspacePath())
  await expect(page.locator('.agent-fab')).toBeVisible()
  await expect(page.locator('.agent-side-panel')).toHaveCount(0)
})

test('智能体设置可连接 API Key 与 OAuth 提供商并即时刷新模型', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await authenticateUserAdministrator(page)
  await page.goto(workspacePath('/admin/agent'))

  await expect(page.getByText('连接和断开操作立即生效，模型列表会自动刷新，无需另行保存。')).toBeVisible()
  await expect(page.locator('.agent-settings-page-actions')).toBeVisible()
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toBeDisabled()

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
  await page.getByLabel('API Key', { exact: true }).fill('e2e-api-key')
  await page.getByRole('button', { name: '连接提供商', exact: true }).click()
  await expect(page.locator('.provider-connected-item').filter({ hasText: 'E2E API Key Provider' })).toHaveCount(1)
  await expect(
    page
      .locator('.provider-connected-item')
      .filter({ hasText: 'E2E API Key Provider' })
      .getByText('已连接', { exact: true }),
  ).toHaveCount(0)
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
  const disconnectDescription = page.getByText('将从 OpenCode 删除 E2E API Key Provider 的连接凭据。之后可重新连接。', {
    exact: true,
  })
  await expect(disconnectDescription).toBeVisible()
  await expectDialogTextRetainedDuringClose(
    page,
    page.getByRole('dialog', { name: '断开提供商' }),
    'E2E API Key Provider',
    () => page.getByRole('button', { name: '取消', exact: true }).click(),
  )

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

  const modelInput = page.getByRole('combobox', { name: '选择智能体模型' })
  await modelInput.click()
  await modelInput.fill('E2E Code OAuth Provider Model')
  await expect(page.locator('.agent-model-item').filter({ hasText: 'E2E Code OAuth Provider Model' })).toBeVisible()

  const saveButton = page.getByRole('button', { name: '保存设置' })
  const bounds = await saveButton.boundingBox()
  expect(bounds).not.toBeNull()
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(768)
})

test('提供商选择器在竖屏中横向不越界且即时操作无需底部保存', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'chromium-landscape')
  await authenticateUserAdministrator(page)
  await page.route('**/api/user/*/agent/settings', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue()
      return
    }
    const response = await route.fetch()
    const settings = (await response.json()) as Record<string, unknown>
    await route.fulfill({
      response,
      json: {
        ...settings,
        model: { provider_id: 'fake', model_id: 'vision' },
        model_available: true,
        variant: 'max',
      },
    })
  })
  await page.goto(workspacePath('/admin/agent'))

  const search = page.getByRole('combobox', { name: '选择提供商' })
  const settingsPage = page.locator('.agent-settings-page')
  await expect(search).toBeVisible()
  await expect(settingsPage).toBeVisible()
  await expect(page.locator('.agent-settings-page-actions')).toBeVisible()
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toBeDisabled()

  const variantScroller = page.locator('.agent-variant-scroll')
  const variantGroup = page.locator('.agent-variant-group')
  await expect(variantScroller).toBeVisible()
  await expect(variantGroup).toHaveCSS('flex-wrap', 'nowrap')
  const variantLayout = await variantScroller.evaluate((scroller) => {
    const bounds = scroller.getBoundingClientRect()
    const items = [...scroller.querySelectorAll<HTMLElement>('.agent-variant-item')]
    const selected = scroller.querySelector<HTMLElement>('.agent-variant-item[data-state="checked"]')
    const selectedBounds = selected?.getBoundingClientRect()
    return {
      clientWidth: scroller.clientWidth,
      scrollWidth: scroller.scrollWidth,
      itemTops: [...new Set(items.map((item) => Math.round(item.getBoundingClientRect().top)))],
      selectedVisible:
        !!selectedBounds && selectedBounds.left >= bounds.left - 1 && selectedBounds.right <= bounds.right + 1,
    }
  })
  expect(variantLayout.scrollWidth).toBeGreaterThan(variantLayout.clientWidth)
  expect(variantLayout.itemTops).toHaveLength(1)
  expect(variantLayout.selectedVisible).toBe(true)

  const exaInput = page.getByLabel('Exa API Key')
  await exaInput.scrollIntoViewIfNeeded()
  const [exaInputBounds, exaControlBounds] = await Promise.all([
    exaInput.boundingBox(),
    page.locator('.agent-exa-control').boundingBox(),
  ])
  expect(exaInputBounds).not.toBeNull()
  expect(exaControlBounds).not.toBeNull()
  expect(exaInputBounds!.height).toBeGreaterThanOrEqual(40)
  expect(Math.abs(exaInputBounds!.width - exaControlBounds!.width)).toBeLessThanOrEqual(1)

  await search.click()
  await search.fill('E2E API Key Provider')
  const option = page.locator('.provider-picker-item').filter({ hasText: 'E2E API Key Provider' })
  await expect(option).toBeVisible()

  const [searchBounds, optionBounds, pageBounds] = await Promise.all([
    search.boundingBox(),
    option.boundingBox(),
    settingsPage.boundingBox(),
  ])
  for (const bounds of [searchBounds, optionBounds, pageBounds]) {
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390)
  }
  for (const bounds of [searchBounds, optionBounds]) {
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
  await expect(page.locator('.dsh-header-agent')).toHaveCSS('min-height', '40px')
  const siderToggleBounds = await page.locator('.dsh-sider-toggle').boundingBox()
  expect(siderToggleBounds).not.toBeNull()
  expect(siderToggleBounds!.width).toBeGreaterThanOrEqual(40)
  expect(siderToggleBounds!.height).toBeGreaterThanOrEqual(40)
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

test('未固定的浮动智能体在点击笔记区域后关闭', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 2560, height: 1440 })
  await approveDevice(page, 'Playwright floating Agent outside interaction')
  await page.evaluate(() => localStorage.removeItem('marvo.agentFloating.pinned'))
  await page.reload()

  const floatButton = page.locator('.agent-fab')
  const panel = page.locator('.agent-float-desktop')
  await floatButton.click()
  await expect(panel).toBeVisible()

  await page.locator('.dsh-content').click({ position: { x: 24, y: 24 } })
  await expect(panel).toBeHidden()
  await expect(floatButton).toHaveAttribute('aria-expanded', 'false')
})
