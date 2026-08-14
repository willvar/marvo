import { expect, test } from '@playwright/test'
import { toMarkdownAssetPath, toNoteAssetUrl } from '../src/sdk/utils/noteAssets'
import {
  approveDevice,
  authenticateUserAdministrator,
  expectDialogTextRetainedDuringClose,
  openSidebar,
  workspaceAPI,
  workspacePath,
  workspaceURL,
} from './helpers'

test('中文媒体路径不会被重复编码', () => {
  const expected = '/api/notes/%E6%96%B0%E4%B8%AD%E5%9B%BD/assets/%E6%97%B6%E9%97%B4%E7%BA%BF.svg'
  expect(toNoteAssetUrl('assets/时间线.svg', '新中国')).toBe(expected)
  expect(toNoteAssetUrl('assets/%E6%97%B6%E9%97%B4%E7%BA%BF.svg', '新中国')).toBe(expected)
  expect(toMarkdownAssetPath('assets/%E6%97%B6%E9%97%B4%E7%BA%BF.svg')).toBe('assets/时间线.svg')
})

test('编辑状态下点击超链接不会打开页面', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright editor link behavior')
  const title = 'E2E editor link behavior'
  const created = await page.request.post(workspaceAPI('/api/notes'), {
    data: { title, content: '![](assets/时间线.svg)\n\n[编辑态链接](https://example.com)', tags: [] },
  })
  expect(created.ok()).toBeTruthy()

  await page.goto(workspacePath(`/note/${encodeURIComponent(title)}`))
  await expect(page.locator('.note-preview')).toBeVisible()
  await page.locator('.editor-toolbar-left > .toolbar-btn').nth(1).click()
  const expectedAssetURL = workspaceAPI(
    `/api/notes/${encodeURIComponent(title)}/assets/${encodeURIComponent('时间线.svg')}`,
  )
  await expect(page.locator('.tiptap img')).toHaveAttribute('src', expectedAssetURL)
  const editorLink = page.locator('.tiptap a', { hasText: '编辑态链接' })
  await expect(editorLink).toBeVisible()
  const pageCount = page.context().pages().length
  await editorLink.click()
  await page.waitForTimeout(200)
  await expect(page).toHaveURL(new RegExp(`/note/${encodeURIComponent(title)}$`))
  expect(page.context().pages()).toHaveLength(pageCount)

  const editor = page.locator('.tiptap')
  await editor.press('Control+End')
  await page.keyboard.type('\n保存路径检查')
  await expect(page.locator('.dsh-header-save-status')).toHaveText(/草稿已保护|保存中…/)
  await expect(page.locator('.dsh-header-save-status')).toHaveText('已保存', { timeout: 10_000 })
  const saved = await (await page.request.get(workspaceAPI(`/api/notes/${encodeURIComponent(title)}`))).json()
  expect(saved.content).toContain('assets/时间线.svg')
  expect(saved.content).not.toContain('assets/%E6%97%B6%E9%97%B4%E7%BA%BF.svg')

  await page.locator('.toolbar-btn').filter({ hasText: '查看' }).click()
  await expect(page.locator('.note-preview a', { hasText: '编辑态链接' })).toHaveAttribute(
    'href',
    'https://example.com',
  )
})

test('长标签保持局部滚动且保存状态位于标题右侧', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright long note tags')
  const title = 'E2E long note tags'
  const tags = Array.from({ length: 16 }, (_, index) => `很长的标签-${index + 1}-${'内容'.repeat(8)}`)
  const created = await page.request.post(workspaceAPI('/api/notes'), {
    data: { title, content: '标签布局检查', tags },
  })
  expect(created.ok()).toBeTruthy()

  await page.goto(workspacePath(`/note/${encodeURIComponent(title)}`))
  const saving = page.locator('.dsh-header-save-status')
  const headerTitle = page.locator('.dsh-header-title')
  const header = page.locator('.dsh-header')
  const tagViewport = page.locator('.editor-tags')
  const deleteButton = page.getByTitle('移到回收站')
  await expect(saving).toHaveText('已保存')
  await expect(saving).toBeVisible()
  await expect(deleteButton).toBeVisible()
  expect(await tagViewport.evaluate((element) => element.scrollWidth > element.clientWidth)).toBeTruthy()

  const savingBounds = await saving.boundingBox()
  const titleBounds = await headerTitle.boundingBox()
  const headerBounds = await header.boundingBox()
  const tagsBounds = await tagViewport.boundingBox()
  const deleteBounds = await deleteButton.boundingBox()
  expect(savingBounds).not.toBeNull()
  expect(titleBounds).not.toBeNull()
  expect(headerBounds).not.toBeNull()
  expect(tagsBounds).not.toBeNull()
  expect(deleteBounds).not.toBeNull()
  expect(titleBounds!.x + titleBounds!.width).toBeLessThanOrEqual(savingBounds!.x)
  expect(savingBounds!.x + savingBounds!.width).toBeLessThanOrEqual(headerBounds!.x + headerBounds!.width)
  expect(tagsBounds!.x + tagsBounds!.width).toBeLessThanOrEqual(deleteBounds!.x)
  await expect(page.locator('.editor-toolbar .dsh-header-save-status')).toHaveCount(0)
})

test('标签失焦时写入原笔记且不会跟随笔记切换', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'webkit-portrait')
  await approveDevice(page, 'Playwright pending note tag')
  const suffix = testInfo.project.name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()
  const firstTitle = `E2E pending tag first ${suffix}`
  const secondTitle = `E2E pending tag second ${suffix}`
  for (const title of [firstTitle, secondTitle]) {
    const created = await page.request.post(workspaceAPI('/api/notes'), {
      data: { title, content: title, tags: [] },
    })
    expect(created.ok()).toBeTruthy()
  }

  await page.goto(workspacePath(`/note/${encodeURIComponent(firstTitle)}`))
  const tagInput = page.getByPlaceholder('添加标签')
  const tag = '失焦提交的标签'
  await page.route('**/api/notes/*/meta', async (route) => {
    if (
      route
        .request()
        .url()
        .includes(`${encodeURIComponent(firstTitle)}/meta`)
    ) {
      await new Promise((resolve) => setTimeout(resolve, 300))
    }
    await route.continue()
  })
  await tagInput.fill(tag)
  await openSidebar(page)
  await page.locator('.dsh-nav-item').filter({ hasText: secondTitle }).click()

  await expect(page).toHaveURL(new RegExp(`/note/${encodeURIComponent(secondTitle)}$`))
  await expect(tagInput).toHaveValue('')
  await expect(page.locator('.note-preview')).toContainText(secondTitle)
  await expect
    .poll(async () => {
      const response = await page.request.get(workspaceAPI(`/api/notes/${encodeURIComponent(firstTitle)}`))
      const note = await response.json()
      return note.note.tags
    })
    .toEqual([tag])
  const secondNote = await (
    await page.request.get(workspaceAPI(`/api/notes/${encodeURIComponent(secondTitle)}`))
  ).json()
  expect(secondNote.note.tags).toEqual([])
})

test('笔记列表品牌栏与内容标题栏保持对齐', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright shell header alignment')

  const noteTitle = 'E2E shell alignment'
  const note = await page.request.post(workspaceAPI('/api/notes'), {
    data: { title: noteTitle, content: 'alignment', tags: [] },
  })
  expect(note.ok()).toBeTruthy()
  const session = await page.request.post(workspaceAPI('/api/agent/session'))
  expect(session.ok()).toBeTruthy()

  const brand = page.locator('.dsh-brand')
  const header = page.locator('.dsh-header')
  const siderToggle = page.locator('.dsh-sider-toggle')
  const headerAction = page.locator('.dsh-header-agent')
  const [brandBounds, headerBounds, siderToggleBounds, headerActionBounds] = await Promise.all([
    brand.boundingBox(),
    header.boundingBox(),
    siderToggle.boundingBox(),
    headerAction.boundingBox(),
  ])

  expect(brandBounds).not.toBeNull()
  expect(headerBounds).not.toBeNull()
  expect(siderToggleBounds).not.toBeNull()
  expect(headerActionBounds).not.toBeNull()
  expect(brandBounds!.height).toBe(headerBounds!.height)
  expect(
    Math.abs(
      siderToggleBounds!.y + siderToggleBounds!.height / 2 - (headerActionBounds!.y + headerActionBounds!.height / 2),
    ),
  ).toBeLessThanOrEqual(0.5)
  expect(
    Math.abs(
      brandBounds!.x +
        brandBounds!.width -
        (siderToggleBounds!.x + siderToggleBounds!.width) -
        (headerBounds!.x + headerBounds!.width - (headerActionBounds!.x + headerActionBounds!.width)),
    ),
  ).toBeLessThanOrEqual(0.5)

  await page.goto(workspacePath(`/note/${encodeURIComponent(noteTitle)}`))
  const [searchRowBounds, editorToolbarBounds] = await Promise.all([
    page.locator('.dsh-search').boundingBox(),
    page.locator('.editor-toolbar').boundingBox(),
  ])
  expect(searchRowBounds!.y).toBe(editorToolbarBounds!.y)
  expect(searchRowBounds!.height).toBe(editorToolbarBounds!.height)

  const logo = page.locator('.dsh-logo')
  await expect(logo).toHaveAttribute('href', workspacePath())
  await logo.click()
  await expect(page).toHaveURL(workspaceURL())

  await page.goto(workspacePath('/agent'))
  const search = page.locator('.dsh-search-inp')
  const sessionsPane = page.locator('.agent-chat-sessions')
  const creation = page.locator('.x-conversations-creation')
  const noteItem = page.locator('.dsh-nav-item').first()
  const conversationItem = page.locator('.x-conversations-item').first()
  await expect(creation).toBeVisible()
  await expect(noteItem).toBeVisible()
  await expect(conversationItem).toBeVisible()
  await Promise.all([
    page.locator('.dsh-nav').evaluate((element) => (element.scrollTop = 0)),
    page.locator('.x-conversations-list').evaluate((element) => (element.scrollTop = 0)),
  ])
  const [searchBounds, sessionsPaneBounds, creationBounds, noteItemBounds, conversationItemBounds] = await Promise.all([
    search.boundingBox(),
    sessionsPane.boundingBox(),
    creation.boundingBox(),
    noteItem.boundingBox(),
    conversationItem.boundingBox(),
  ])
  expect(searchBounds!.y).toBe(creationBounds!.y)
  expect(searchBounds!.height).toBe(creationBounds!.height)
  expect(searchBounds!.x - brandBounds!.x).toBe(creationBounds!.x - sessionsPaneBounds!.x)
  expect(noteItemBounds!.y).toBe(conversationItemBounds!.y)
  expect(noteItemBounds!.x - brandBounds!.x).toBe(conversationItemBounds!.x - sessionsPaneBounds!.x)

  const messageContent = page.locator('.x-bubble-list-content')
  const composer = page.locator('.agent-chat-composer-wrap')
  await expect(composer).toBeVisible()
  const [messageContentBounds, composerBounds, messagePadding] = await Promise.all([
    messageContent.boundingBox(),
    composer.boundingBox(),
    messageContent.evaluate((element) => {
      const style = getComputedStyle(element)
      return { left: Number.parseFloat(style.paddingLeft), right: Number.parseFloat(style.paddingRight) }
    }),
  ])
  expect(messageContentBounds!.x + messagePadding.left).toBe(composerBounds!.x)
  expect(messageContentBounds!.x + messageContentBounds!.width - messagePadding.right).toBe(
    composerBounds!.x + composerBounds!.width,
  )
  const [footerButtonBounds, senderBounds] = await Promise.all([
    page.getByRole('link', { name: '管理后台' }).boundingBox(),
    page.locator('.agent-chat-input .x-sender').boundingBox(),
  ])
  expect(footerButtonBounds!.y + footerButtonBounds!.height).toBe(senderBounds!.y + senderBounds!.height)

  await page.evaluate(() => localStorage.setItem('marvo.ui.agentAssistantDisplayMode', 'sidebar'))
  await page.reload()
  await page.goto(workspacePath())
  const sideHeader = page.locator('.agent-side-header')
  const sideAction = page.locator('.agent-side-action')
  await expect(sideHeader).toBeVisible()
  const [alignedHeaderBounds, sideHeaderBounds, alignedHeaderActionBounds, sideActionBounds] = await Promise.all([
    header.boundingBox(),
    sideHeader.boundingBox(),
    headerAction.boundingBox(),
    sideAction.boundingBox(),
  ])
  expect(sideHeaderBounds!.y).toBe(alignedHeaderBounds!.y)
  expect(sideHeaderBounds!.height).toBe(alignedHeaderBounds!.height)
  expect(sideActionBounds!.y + sideActionBounds!.height / 2).toBe(
    alignedHeaderActionBounds!.y + alignedHeaderActionBounds!.height / 2,
  )
  const sideSenderBounds = await page.locator('.agent-side-panel .x-sender').boundingBox()
  const alignedFooterButtonBounds = await page.getByRole('link', { name: '管理后台' }).boundingBox()
  expect(sideSenderBounds!.y + sideSenderBounds!.height).toBe(
    alignedFooterButtonBounds!.y + alignedFooterButtonBounds!.height,
  )

  await page.setViewportSize({ width: 390, height: 844 })
  const edgeToggle = page.locator('.dsh-edge-toggle')
  await expect(edgeToggle).toBeVisible()
  const [compactBrandBounds, compactHeaderBounds, edgeToggleBounds, compactHeaderActionBounds] = await Promise.all([
    brand.boundingBox(),
    header.boundingBox(),
    edgeToggle.boundingBox(),
    headerAction.boundingBox(),
  ])
  expect(compactBrandBounds!.height).toBe(48)
  expect(compactHeaderBounds!.height).toBe(48)
  expect(
    Math.abs(
      edgeToggleBounds!.y +
        edgeToggleBounds!.height / 2 -
        (compactHeaderActionBounds!.y + compactHeaderActionBounds!.height / 2),
    ),
  ).toBeLessThanOrEqual(0.5)
})

test('主题 fontSize 按同一比例缩放全站文字', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright global font scaling')
  let fontSize = 14
  await page.route(`**${workspaceAPI('/api/theme')}`, async (route) => {
    await route.fulfill({
      json: {
        fontSize,
        contentFontSize: 15,
        darkMode: false,
        contentLineHeight: 1.8,
        contentWidth: 'full',
        accentColor: '#4f46e5',
      },
    })
  })

  const title = 'E2E global font scaling'
  const created = await page.request.post(workspaceAPI('/api/notes'), {
    data: { title, content: '全站字号比例测试', tags: [] },
  })
  expect(created.ok()).toBeTruthy()
  await page.goto(workspacePath(`/note/${encodeURIComponent(title)}`))
  await expect(page.locator('.note-preview')).toBeVisible()

  const typographySizes = () =>
    page.evaluate(() => {
      const size = (selector: string) => {
        const element = document.querySelector(selector)
        if (!(element instanceof HTMLElement)) throw new Error(`Missing typography target: ${selector}`)
        return Number.parseFloat(getComputedStyle(element).fontSize)
      }
      return {
        root: size('html'),
        brand: size('.dsh-logo'),
        navigation: size('.dsh-nav-item'),
        preview: size('.note-preview'),
        footer: size('.dsh-footer-button'),
      }
    })

  const baseline = await typographySizes()
  fontSize = 21
  await page.reload()
  await expect(page.locator('.note-preview')).toBeVisible()
  const scaled = await typographySizes()

  for (const key of Object.keys(baseline) as Array<keyof typeof baseline>) {
    expect(scaled[key] / baseline[key]).toBeCloseTo(1.5, 2)
  }
})

test('回收站破坏性操作统一使用组件确认弹框', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright trash confirmations')
  const titles = ['E2E trash restore field', 'E2E trash single delete', 'E2E trash empty']
  for (const title of titles) {
    const created = await page.request.post(workspaceAPI('/api/notes'), { data: { title, content: '', tags: [] } })
    expect(created.ok()).toBeTruthy()
    const note = (await created.json()) as { instance_token: string }
    const trashed = await page.request.delete(workspaceAPI(`/api/notes/${encodeURIComponent(title)}`), {
      data: { instance_token: note.instance_token },
    })
    expect(trashed.ok()).toBeTruthy()
  }

  let nativeDialogs = 0
  page.on('dialog', async (dialog) => {
    nativeDialogs++
    await dialog.dismiss()
  })
  await page.goto(workspacePath('/trash'))

  const restoreCard = page.locator('.trash-card').filter({ hasText: titles[0] })
  await restoreCard.getByRole('button', { name: '恢复', exact: true }).click()
  const restoreInput = restoreCard.getByRole('textbox', { name: '新标题' })
  await expect(restoreInput).toHaveAttribute('data-scope', 'field')
  await expect(restoreInput).toHaveAttribute('data-part', 'input')
  await expect(restoreInput).toBeFocused()
  await restoreCard.getByRole('button', { name: '取消' }).click()

  const singleCard = page.locator('.trash-card').filter({ hasText: titles[1] })
  await singleCard.getByRole('button', { name: '永久删除' }).click()
  await expect(page.getByRole('heading', { name: '永久删除笔记' })).toBeVisible()
  await expectDialogTextRetainedDuringClose(page, page.getByRole('dialog', { name: '永久删除笔记' }), titles[1], () =>
    page.getByRole('button', { name: '取消', exact: true }).click(),
  )
  await singleCard.getByRole('button', { name: '永久删除' }).click()
  await page.getByRole('button', { name: '确认永久删除' }).click()
  await expect(singleCard).toHaveCount(0)

  await page.getByRole('button', { name: '清空回收站' }).click()
  await expect(page.getByRole('heading', { name: '清空回收站' })).toBeVisible()
  await page.getByRole('button', { name: '确认清空' }).click()
  await expect(page.getByText('回收站是空的')).toBeVisible()
  expect(nativeDialogs).toBe(0)
})

test('撤回设备批准需要组件确认', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright device administration')
  const suffix = Date.now()
  const deviceName = `Playwright 撤回确认 ${suffix}`
  const localDeviceID = `marvo-playwright-revoke-${suffix}`
  const application = await page.request.post(workspaceAPI('/api/auth/apply'), {
    data: {
      local_device_id: localDeviceID,
      device_name: deviceName,
      device_info: { platform: 'Playwright' },
    },
  })
  expect(application.ok()).toBeTruthy()

  let nativeDialogs = 0
  page.on('dialog', async (dialog) => {
    nativeDialogs++
    await dialog.dismiss()
  })

  await page.goto(workspacePath())
  const adminEntry = page.getByRole('link', { name: '管理后台' })
  await expect(adminEntry).toBeVisible()
  await expect(adminEntry).toHaveAttribute('target', '_blank')
  await expect(adminEntry).toHaveAttribute('rel', 'noopener noreferrer')
  const loginPagePromise = page.context().waitForEvent('page')
  await adminEntry.click()
  const loginPage = await loginPagePromise
  await expect.poll(() => new URL(loginPage.url()).pathname).toBe(workspacePath('/login'))
  expect(new URL(loginPage.url()).searchParams.get('mode')).toBe('admin')
  await expect(page).toHaveURL(workspaceURL())
  await loginPage.close()

  await authenticateUserAdministrator(page)
  await page.goto(workspacePath())
  const adminPagePromise = page.context().waitForEvent('page')
  await page.getByRole('link', { name: '管理后台' }).click()
  const adminPage = await adminPagePromise
  await expect(adminPage).toHaveURL(workspaceURL('/admin'))
  await expect(page).toHaveURL(workspaceURL())
  await adminPage.close()
  await page.goto(workspacePath('/admin'))

  const pendingRow = page.locator('tbody tr').filter({ hasText: deviceName })
  await expect(pendingRow).toBeVisible()
  await pendingRow.getByRole('button', { name: '批准', exact: true }).click()
  await expect(pendingRow).toHaveCount(0)

  await page.getByRole('button', { name: /已批准设备/ }).click()
  const approvedRow = page.locator(`tbody tr[data-device-id="${localDeviceID}"]`)
  await expect(approvedRow).toBeVisible()
  await expect(approvedRow.locator('td').nth(1)).toHaveText(/^\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2}$/)

  const listedDevices = (await (await page.request.get(workspaceAPI('/api/admin/devices'))).json()).devices as Array<{
    local_device_id: string
    device_name: string
  }>
  const existingName = listedDevices.find((device) => device.local_device_id !== localDeviceID)?.device_name
  expect(existingName).toBeTruthy()
  const normalActionTops = await approvedRow
    .locator('.btn-group .admin-btn')
    .evaluateAll((buttons) => buttons.map((button) => button.getBoundingClientRect().top))
  expect(normalActionTops).toHaveLength(2)
  expect(Math.max(...normalActionTops) - Math.min(...normalActionTops)).toBeLessThan(2)
  await approvedRow.getByRole('button', { name: '编辑', exact: true }).click()
  const nameInput = approvedRow.getByLabel('设备名称')
  await expect(nameInput).toBeFocused()
  const editingActionTops = await approvedRow
    .locator('.btn-group .admin-btn')
    .evaluateAll((buttons) => buttons.map((button) => button.getBoundingClientRect().top))
  expect(editingActionTops).toHaveLength(2)
  expect(Math.max(...editingActionTops) - Math.min(...editingActionTops)).toBeLessThan(2)
  await nameInput.fill(existingName!)
  await approvedRow.getByRole('button', { name: '保存', exact: true }).click()
  await expect(approvedRow.getByRole('alert')).toHaveText('设备名称不能与其他已批准设备重复')

  const renamedDevice = `工作平板 ${suffix}`
  await nameInput.fill(`  ${renamedDevice}  `)
  await nameInput.press('Enter')
  await expect(approvedRow).toBeVisible()
  await expect(approvedRow).toContainText(renamedDevice)
  await expect(approvedRow.getByRole('button', { name: '编辑', exact: true })).toBeVisible()
  await page.reload()
  await page.getByRole('button', { name: /已批准设备/ }).click()
  await expect(approvedRow).toBeVisible()
  await expect(approvedRow).toContainText(renamedDevice)

  await approvedRow.locator('td').first().locator('a').click()
  const detailDialog = page.getByRole('dialog', { name: '设备信息' })
  await expect(detailDialog).toContainText('Playwright')
  await expectDialogTextRetainedDuringClose(page, detailDialog, 'Playwright', () =>
    detailDialog.locator('.dialog-close').click(),
  )

  await approvedRow.getByRole('button', { name: '撤回', exact: true }).click()
  await expect(page.getByRole('heading', { name: '撤回设备批准' })).toBeVisible()
  await expect(page.getByText(`确定撤回「${renamedDevice}」的访问权限吗？`, { exact: false })).toBeVisible()
  const revokeDialog = page.locator('.dialog-panel').filter({ hasText: '撤回设备批准' })
  await expectDialogTextRetainedDuringClose(page, revokeDialog, renamedDevice, () =>
    page.getByRole('button', { name: '取消', exact: true }).click(),
  )
  await expect(approvedRow).toBeVisible()

  await approvedRow.getByRole('button', { name: '撤回', exact: true }).click()
  await page.getByRole('button', { name: '确认撤回', exact: true }).click()
  await expect(approvedRow).toHaveCount(0)
  expect(nativeDialogs).toBe(0)
})

test('路由资源版本失效时自动恢复到当前前端版本', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright stale route recovery')
  await page.goto(workspacePath())

  let trashModuleRequests = 0
  await page.route('**/src/pages/desktop/Trash.vue*', async (route) => {
    trashModuleRequests++
    if (trashModuleRequests === 1) {
      await route.fulfill({ status: 404, contentType: 'text/plain', body: 'stale module' })
      return
    }
    await route.continue()
  })

  await page.getByRole('button', { name: '回收站', exact: true }).click()
  await expect(page).toHaveURL(/\/trash$/)
  await expect(page.getByRole('heading', { name: '回收站' })).toBeVisible()
  expect(trashModuleRequests).toBeGreaterThanOrEqual(2)
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('marvo.staleAssetReload'))).toBeNull()
})

test('桌面端刷新后保留笔记列表展开状态', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright note list preference')

  await page.getByTitle('收起列表').click()
  await expect(page.getByTitle('展开列表')).toBeVisible()
  await page.reload()
  await expect(page.getByTitle('展开列表')).toBeVisible()

  await page.getByTitle('展开列表').click()
  await expect(page.getByTitle('收起列表')).toBeVisible()
  await page.reload()
  await expect(page.getByTitle('收起列表')).toBeVisible()
})
