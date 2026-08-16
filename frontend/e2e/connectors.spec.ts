import { expect, test } from '@playwright/test'
import { approveDevice, authenticateUserAdministrator, workspaceAPI, workspacePath } from './helpers'

test('用户后台可配置 Activity Webhook 且敏感地址不回显', async ({ page }, testInfo) => {
  test.skip(!['chromium-landscape', 'chromium-portrait'].includes(testInfo.project.name))
  const connectorName = 'E2E Activity Webhook 用于验证较长名称排版'
  await page.setViewportSize(
    testInfo.project.name === 'chromium-portrait' ? { width: 360, height: 740 } : { width: 1366, height: 768 },
  )
  await approveDevice(page, '连接器深链接设备')
  await authenticateUserAdministrator(page)

  const existing = await page.request.get(workspaceAPI('/api/admin/connectors'))
  if (existing.ok()) {
    const items = ((await existing.json()).connectors || []) as Array<{ id: string; name: string }>
    for (const connector of items.filter((item) => item.name === connectorName)) {
      await page.request.delete(workspaceAPI(`/api/admin/connectors/${connector.id}`))
    }
  }

  await page.goto(workspacePath('/admin/connectors'))
  await expect(page).toHaveTitle(/活动连接器/)
  await expect(page.getByRole('heading', { name: '活动连接器', exact: true })).toBeVisible()
  const createButton = page.getByRole('button', { name: /新建连接器|添加第一个连接器/ }).first()
  await expect(createButton).toBeVisible()
  const buttonColors = await createButton.evaluate((button) => {
    const style = getComputedStyle(button)
    return { background: style.backgroundColor, foreground: style.color }
  })
  expect(buttonColors.background).not.toBe('rgba(0, 0, 0, 0)')
  expect(buttonColors.background).not.toBe(buttonColors.foreground)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(
    true,
  )
  if (testInfo.project.name === 'chromium-portrait') {
    const overviewTops = await page
      .locator('.activity-connectors-overview > div')
      .evaluateAll((items) => items.map((item) => Math.round(item.getBoundingClientRect().top)))
    expect(Math.max(...overviewTops) - Math.min(...overviewTops)).toBeLessThanOrEqual(1)
  }
  await createButton.click()

  const dialog = page.getByRole('dialog', { name: '新建连接器' })
  await expect(dialog).toBeVisible()
  const provider = dialog.getByRole('combobox', { name: '选择连接器服务' })
  await expect(provider).toHaveCSS('padding-left', '12px')
  await expect(provider).toHaveCSS('padding-right', '12px')
  await provider.click()
  const providerList = page.locator('.activity-connector-provider-content')
  await expect(providerList).toBeVisible()
  const [providerBounds, providerListBounds] = await Promise.all([provider.boundingBox(), providerList.boundingBox()])
  expect(providerBounds).not.toBeNull()
  expect(providerListBounds).not.toBeNull()
  expect(providerListBounds!.y).toBeGreaterThanOrEqual(providerBounds!.y + providerBounds!.height - 1)
  expect(providerListBounds!.y + providerListBounds!.height).toBeLessThanOrEqual(page.viewportSize()!.height - 8)
  await provider.fill('SMTP')
  await page.locator('.activity-connector-provider-item[data-value="smtp"]').click()
  const uncheckedIndicator = dialog.locator('.activity-connector-checkbox [data-part="indicator"][hidden]').first()
  await expect(uncheckedIndicator).toHaveCSS('display', 'none')
  await provider.fill('Webhook')
  await page.locator('.activity-connector-provider-item').filter({ hasText: 'Webhook' }).click()

  const editorActions = dialog.locator('.activity-connector-editor-actions')
  await expect(editorActions).toBeVisible()
  const editorActionsBounds = await editorActions.boundingBox()
  expect(editorActionsBounds).not.toBeNull()
  expect(editorActionsBounds!.y + editorActionsBounds!.height).toBeLessThanOrEqual(page.viewportSize()!.height)
  await dialog.getByLabel('连接器名称').fill(connectorName)
  const credentialURL = 'https://example.test/hooks/e2e-secret-token'
  await dialog.getByLabel(/请求地址/).fill(credentialURL)
  const saveResponsePromise = page.waitForResponse(
    (response) => response.url().endsWith('/admin/connectors') && response.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: '保存连接器' }).click()
  const saveResponse = await saveResponsePromise
  expect(saveResponse.status()).toBe(201)
  expect(await saveResponse.text()).not.toContain(credentialURL)

  const card = page.locator('.activity-connector-card').filter({ hasText: connectorName })
  await expect(card).toBeVisible()
  await expect(card.locator('.activity-connector-status')).toHaveCSS('white-space', 'nowrap')
  const actionTops = await card
    .locator('footer button')
    .evaluateAll((buttons) => buttons.map((button) => Math.round(button.getBoundingClientRect().top)))
  expect(Math.max(...actionTops) - Math.min(...actionTops)).toBeLessThanOrEqual(1)
  await card.getByRole('button', { name: '编辑' }).click()
  const editDialog = page.getByRole('dialog', { name: '编辑连接器' })
  const address = editDialog.getByLabel(/请求地址/)
  await expect(address).toHaveAttribute('type', 'password')
  await expect(address).toHaveValue('')
  await expect(address).toHaveAttribute('placeholder', '已保存；留空保持不变')
  await editDialog.getByRole('button', { name: '取消' }).click()

  await card.getByRole('button', { name: '删除' }).click()
  const deleteDialog = page.getByRole('dialog', { name: '删除连接器' })
  await expect(deleteDialog).toContainText(connectorName)
  await deleteDialog.getByRole('button', { name: '确认删除' }).click()
  await expect(card).toHaveCount(0)
  await expect(deleteDialog).toBeHidden()

  await page.context().clearCookies()
  const missingActivity = '00000000000000000000000000000000'
  const activityPath = workspacePath(`/activity?activity=${missingActivity}`)
  await page.goto(activityPath)
  await expect(page).toHaveURL(activityPath)
  await expect(page.getByText('链接指向的活动已不存在或暂时无法访问')).toBeVisible()
})
