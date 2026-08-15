import { expect, test } from '@playwright/test'
import {
  approveDevice,
  authenticateUserAdministrator,
  closeCompactSidebar,
  openAgentSessions,
  openSidebar,
  workspaceAPI,
  workspaceAPIRegex,
  workspacePath,
  workspaceURL,
} from './helpers'

test('核心笔记流程在响应式布局中安全工作', async ({ page }, testInfo) => {
  const suffix = testInfo.project.name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()
  let title = `E2E ${suffix} core`
  let encodedTitle = encodeURIComponent(title)
  const brand = `知识空间 ${suffix}`
  await approveDevice(page, `Playwright ${suffix}`)
  await authenticateUserAdministrator(page)
  const brandUpdate = await page.request.put(workspaceAPI('/api/admin/brand'), { data: { name: brand } })
  expect(brandUpdate.ok()).toBeTruthy()
  await page.reload()
  await expect(page.locator('.dsh-logo')).toHaveText(brand)
  await expect(page.locator('.dsh-logo .marvo-mark')).toHaveCount(0)
  await expect(page).toHaveTitle(`工作区 · ${brand}`)

  await expect(page.locator('.dsh-header-title-input')).toHaveCount(0)
  await expect(page.locator('.home-welcome')).toBeVisible()
  await expect(page.getByRole('button', { name: '新建笔记', exact: true })).toHaveCount(0)
  await expect(page.locator('.home-welcome')).toContainText('左侧列表上方的“搜索或新建”输入框')
  await expect(page.locator('.home-agent-composer .x-sender-input')).toBeVisible()
  await expect(page.locator('.home-agent-composer .x-sender-input')).toHaveAttribute(
    'placeholder',
    '向智能体描述你想完成的内容...',
  )

  const appEntry = page.getByRole('button', { name: 'APP', exact: true })
  const agentEntry = page.getByRole('button', { name: '智能体', exact: true })
  await expect(appEntry).toBeVisible()
  await expect(agentEntry).toBeVisible()
  const [appEntryBounds, agentEntryBounds] = await Promise.all([appEntry.boundingBox(), agentEntry.boundingBox()])
  expect(appEntryBounds).not.toBeNull()
  expect(agentEntryBounds).not.toBeNull()
  expect(appEntryBounds!.x + appEntryBounds!.width).toBeLessThanOrEqual(agentEntryBounds!.x)

  await openSidebar(page)
  await expect(page.locator('.dsh-footer')).not.toContainText('智能体')
  const footerButtons = page.locator('.dsh-footer-button')
  await expect(footerButtons).toHaveCount(2)
  await expect(footerButtons.nth(0)).toHaveText(/回收站/)
  await expect(footerButtons.nth(1)).toHaveText(/管理后台/)
  await expect(page.locator('.dsh-footer')).not.toContainText('Android APP')
  await expect(page.locator('.dsh-footer')).not.toContainText('智能体设置')
  await closeCompactSidebar(page)
  await agentEntry.click()
  await expect(page).toHaveURL(workspaceURL('/agent'))
  await expect(page).toHaveTitle(`智能体 · ${brand}`)
  const staticHeaderTitle = page.locator('.dsh-header-title')
  await expect(staticHeaderTitle).toHaveText('智能体对话')
  await expect(staticHeaderTitle).not.toHaveClass(/is-clickable/)
  await expect(staticHeaderTitle).not.toHaveAttribute('title')
  expect(await staticHeaderTitle.evaluate((element) => element.tagName)).toBe('SPAN')
  expect(await staticHeaderTitle.evaluate((element) => getComputedStyle(element).cursor)).not.toBe('pointer')
  await expect(page.getByRole('button', { name: '设置', exact: true })).toHaveCount(0)
  await page.goto(workspacePath('/admin/agent'))
  await expect(page).toHaveTitle(`智能体设置 · Playwright 用户空间 · Marvo`)
  await expect(page.getByRole('heading', { name: '智能体设置' })).toBeVisible()
  await expect(page.getByRole('tablist')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '新增规则' })).toBeVisible()
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await expect(page.locator('.agent-model-selected')).toContainText('E2E Vision')
  await expect(page.locator('.agent-model-selected')).toContainText('支持图片')
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  const savedReasoning = (await page.locator('.agent-variant-item[data-state="checked"]').textContent())?.trim()

  const modelInput = page.getByRole('combobox', { name: '选择智能体模型' })
  await modelInput.click()
  await modelInput.fill('E2E Text')
  await page.locator('.agent-model-item').filter({ hasText: 'E2E Text' }).click()
  await expect(page.locator('.agent-model-selected')).toContainText('不支持图片')
  await modelInput.click()
  await modelInput.fill('E2E Vision')
  await page.locator('.agent-model-item').filter({ hasText: 'E2E Vision' }).click()
  await expect(page.locator('.agent-model-selected')).toContainText('支持图片')

  const changedReasoning = page
    .locator('.agent-variant-item')
    .filter({ hasText: savedReasoning === '高' ? /^低$/ : /^高$/ })
  await changedReasoning.click()
  await expect(changedReasoning).toHaveAttribute('data-state', 'checked')
  await expect(page.getByRole('button', { name: '保存设置' })).toBeEnabled()

  const personalizationRule = `称呼测试规则 ${suffix}`
  await page.getByRole('button', { name: '新增规则' }).click()
  const newPersonalizationRule = page.locator('.agent-personalization-rule').last()
  await expect(newPersonalizationRule).not.toHaveAttribute('data-invalid')
  await expect(newPersonalizationRule.locator('[data-part="error-text"]')).toBeHidden()
  await expect(page.getByRole('button', { name: '保存设置' })).toBeEnabled()
  await newPersonalizationRule.locator('.agent-personalization-input').fill(personalizationRule)
  const globalPrompt = `E2E global prompt ${suffix}`
  const expandedGlobalPrompt = `${globalPrompt}\n通过全屏编辑补充`
  await page.getByLabel('全局提示词', { exact: true }).fill(globalPrompt)
  await page.getByRole('button', { name: '展开编辑', exact: true }).click()
  await expect(page.getByRole('heading', { name: '全屏编辑全局提示词' })).toBeVisible()
  const fullscreenPrompt = page.getByRole('textbox', { name: '全屏编辑全局提示词' })
  await expect(fullscreenPrompt).toHaveValue(globalPrompt)
  await fullscreenPrompt.fill(expandedGlobalPrompt)
  await page.keyboard.press('Escape')
  await expect(page.getByRole('heading', { name: '全屏编辑全局提示词' })).toBeHidden()
  await expect(page.getByRole('heading', { name: '智能体设置' })).toBeVisible()
  await expect(page.getByLabel('全局提示词', { exact: true })).toHaveValue(expandedGlobalPrompt)
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByRole('button', { name: '保存设置' })).toBeDisabled()
  await page.reload()
  await expect(page.getByLabel('全局提示词', { exact: true })).toHaveValue(expandedGlobalPrompt)
  await expect(page.locator('.agent-personalization-input').last()).toHaveValue(personalizationRule)
  await page.goto(workspacePath('/agent'))
  await expect(page.locator('.dsh-header-agent')).toHaveCount(0)
  await openSidebar(page)
  const search = page.getByPlaceholder('搜索或新建...')
  await search.fill('非法/标题')
  await search.press('Enter')
  await expect(page.getByRole('alert')).toContainText('标题不能包含')
  await expect(page).toHaveURL(workspaceURL('/agent'))
  await search.fill(title)
  await search.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/note/${encodedTitle}$`))
  await expect(page).toHaveTitle(`${title} · ${brand}`)
  await expect(appEntry).toBeVisible()
  await expect(agentEntry).toBeVisible()
  const [noteAppEntryBounds, noteAgentEntryBounds] = await Promise.all([
    appEntry.boundingBox(),
    agentEntry.boundingBox(),
  ])
  expect(noteAppEntryBounds).not.toBeNull()
  expect(noteAgentEntryBounds).not.toBeNull()
  expect(noteAppEntryBounds!.x + noteAppEntryBounds!.width).toBeLessThanOrEqual(noteAgentEntryBounds!.x)
  expect(noteAgentEntryBounds!.x + noteAgentEntryBounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width)
  const editableHeaderTitle = page.locator('.dsh-header-title')
  await expect(editableHeaderTitle).toHaveText(title)
  await expect(editableHeaderTitle).toHaveClass(/is-clickable/)
  await expect(editableHeaderTitle).toHaveAttribute('title', '重命名笔记')
  expect(await editableHeaderTitle.evaluate((element) => element.tagName)).toBe('BUTTON')
  await expect(editableHeaderTitle).toHaveCSS('cursor', 'pointer')

  const modeButtons = page.locator('.editor-toolbar-left > .toolbar-btn')
  await expect(modeButtons.nth(0)).toHaveText('查看')
  await expect(modeButtons.nth(0)).toHaveClass(/active/)
  await expect(modeButtons.nth(1)).toHaveText('编辑')
  await expect(page.locator('.note-preview')).toBeVisible()
  await expect(page.locator('.tiptap')).toHaveCount(0)
  await modeButtons.nth(1).click()
  const editor = page.locator('.tiptap')
  await expect(editor).toBeVisible()

  const previousTitle = title
  title = `${title} renamed`
  encodedTitle = encodeURIComponent(title)
  await page.locator('.dsh-header-title').click()
  await page.locator('.dsh-header-title-input').fill(title)
  await page.locator('.dsh-header-title-input').press('Enter')
  await expect(page).toHaveURL(new RegExp(`/note/${encodedTitle}$`))
  await expect(page).toHaveTitle(`${title} · ${brand}`)
  await expect(page.locator('.dsh-header-title')).toHaveText(title)
  expect((await page.request.get(workspaceAPI(`/api/notes/${encodeURIComponent(previousTitle)}`))).status()).toBe(404)
  expect((await page.request.get(workspaceAPI(`/api/notes/${encodedTitle}`))).ok()).toBeTruthy()

  await editor.fill('DRAFT SURVIVES REFRESH')
  await page.waitForTimeout(350)
  const beforeDraftSave = await (await page.request.get(workspaceAPI(`/api/notes/${encodeURIComponent(title)}`))).json()
  expect(beforeDraftSave.content).not.toContain('DRAFT SURVIVES REFRESH')
  await page.reload()
  await expect(page.locator('.note-preview')).toContainText('DRAFT SURVIVES REFRESH')
  await page.getByRole('button', { name: '编辑' }).click()
  await expect(editor).toContainText('DRAFT SURVIVES REFRESH')
  await editor.press('Control+s')
  await expect(page.locator('.dsh-header-save-status')).toHaveText('已保存', { timeout: 10_000 })

  await editor.fill('ORPHAN DRAFT RECOVERY')
  await page.waitForTimeout(350)
  const beforeReplacement = await (
    await page.request.get(workspaceAPI(`/api/notes/${encodeURIComponent(title)}`))
  ).json()
  const trashed = await page.request.delete(workspaceAPI(`/api/notes/${encodeURIComponent(title)}`), {
    data: { instance_token: beforeReplacement.instance_token },
  })
  expect(trashed.ok()).toBeTruthy()
  const trashPayload = await trashed.json()
  const restored = await page.request.post(workspaceAPI(`/api/trash/${trashPayload.trash.id}/restore`), {
    data: { new_title: title },
  })
  expect(restored.ok()).toBeTruthy()
  await expect(page.getByRole('heading', { name: '发现旧实例草稿' })).toBeVisible()
  await page.getByRole('button', { name: '明确预览并恢复' }).click()
  await expect(page.getByRole('heading', { name: '保存前确认合并' })).toBeVisible()
  await page.getByRole('button', { name: '接受并重试保存' }).click()
  await expect(page.locator('.dsh-header-save-status')).toHaveText('已保存', { timeout: 10_000 })

  const formula = 'Euler: $e^{i\\pi}+1=0$'
  await editor.fill(formula)
  await expect(page.locator('.dsh-header-save-status')).toHaveText('已保存', { timeout: 10_000 })

  await page.getByRole('button', { name: '查看' }).click()
  await expect(page.locator('.note-preview')).toContainText(formula)
  await expect(page.locator('.note-preview .katex')).toHaveCount(0)
  await page.getByRole('button', { name: '编辑' }).click()

  const snapshotResponse = await page.request.get(workspaceAPI(`/api/notes/${encodeURIComponent(title)}`))
  const snapshot = await snapshotResponse.json()
  await editor.fill('LOCAL VERSION')
  const remoteWrite = await page.request.put(workspaceAPI(`/api/notes/${encodeURIComponent(title)}/content`), {
    data: {
      content: 'REMOTE VERSION',
      base_revision: snapshot.content_revision,
      instance_token: snapshot.instance_token,
    },
  })
  expect(remoteWrite.ok()).toBeTruthy()

  await expect(page.getByRole('heading', { name: '保存前确认合并' })).toBeVisible()
  await expect(page.locator('fieldset.ecore-bar')).toHaveAttribute('disabled', '')
  await expect(page.locator('fieldset.ecore-bar button').first()).toBeDisabled()
  await expect(page.getByPlaceholder('添加标签')).toBeDisabled()
  await expect(page.getByTitle('移到回收站')).toBeDisabled()
  await expect(page.locator('.dsh-header-title')).toHaveAttribute('aria-disabled', 'true')
  await page.getByRole('button', { name: '全部保留' }).click()
  await page.getByRole('button', { name: '接受并重试保存' }).click()
  await expect(page.getByRole('heading', { name: '保存前确认合并' })).toBeHidden()
  await expect(page.locator('fieldset.ecore-bar')).not.toHaveAttribute('disabled', '')
  await expect(page.locator('.dsh-header-title')).toHaveAttribute('aria-disabled', 'false')
  await expect(page.locator('.dsh-header-save-status')).toHaveText('已保存', { timeout: 10_000 })

  const assetUploadRoute = new RegExp(workspaceAPIRegex('/api/notes/.*/assets/[^/]+/content$'))
  await page.route(assetUploadRoute, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 500))
    await route.continue()
  })
  const chooserPromise = page.waitForEvent('filechooser')
  await page.getByTitle('插入附件').click()
  const chooser = await chooserPromise
  await chooser.setFiles({
    name: 'pixel.png',
    mimeType: 'image/png',
    buffer: Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
      'base64',
    ),
  })
  await expect(page.locator('.marvo-asset-placeholder')).toBeVisible()
  await expect(page.locator('.tiptap img')).toBeVisible({ timeout: 15_000 })
  await page.unroute(assetUploadRoute)
  await expect(page.locator('.dsh-header-save-status')).toHaveText('已保存', { timeout: 10_000 })

  await page.getByTitle('移到回收站').click()
  await page.getByRole('button', { name: '移到回收站', exact: true }).last().click()
  await expect(page).toHaveURL(workspaceURL())

  await page.goto(workspacePath('/trash'))
  await expect(page).toHaveTitle(`回收站 · ${brand}`)
  await page.getByRole('button', { name: '恢复', exact: true }).click()
  await expect(page.getByLabel('新标题')).toHaveValue(title)
  await page.getByRole('button', { name: '确认恢复' }).click()
  await expect(page).toHaveURL(new RegExp(`/note/${encodedTitle}$`))
  await expect(page.getByRole('heading', { name: '发现旧实例草稿' })).toBeVisible()
  await page.getByRole('button', { name: '放弃旧草稿' }).click()
  await expect(page.getByRole('heading', { name: '发现旧实例草稿' })).toBeHidden()
  await page.getByRole('button', { name: '编辑' }).click()

  await page.locator('.agent-fab').click()
  const floatingPanel = page.locator('.agent-float-panel')
  await expect(floatingPanel).toBeVisible()
  await expect(floatingPanel.getByRole('button', { name: '添加图片' })).toBeVisible()
  await expect(floatingPanel.getByRole('button', { name: '添加图片' }).locator('svg')).toBeVisible()
  const floatingAttachmentChooser = page.waitForEvent('filechooser')
  await floatingPanel.getByRole('button', { name: '添加附件' }).click()
  await (
    await floatingAttachmentChooser
  ).setFiles({
    name: 'float-notes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('Floating Agent attachment text'),
  })
  await expect(floatingPanel.locator('.agent-composer .x-attachments')).toContainText('float-notes.txt')

  const floatingPromptRequest = page.waitForRequest(
    (request) => request.method() === 'POST' && request.url().includes('/prompt_async'),
  )
  const floatingText = `Float attachment ${suffix}`
  await floatingPanel.getByPlaceholder('输入消息...').fill(floatingText)
  await floatingPanel.getByRole('button', { name: '发送', exact: true }).click()
  const floatingPromptBody = (await floatingPromptRequest).postDataJSON()
  expect(floatingPromptBody.parts[0].text).toBe(floatingText)
  expect(floatingPromptBody.model).toBeUndefined()
  expect(floatingPromptBody.system).toBeUndefined()
  expect(floatingPromptBody.marvoContext.note.title).toBe(title)
  expect(floatingPromptBody.marvoContext.viewport).toMatchObject({
    width: expect.any(Number),
    height: expect.any(Number),
  })
  expect(floatingPromptBody.parts[1]).toMatchObject({
    type: 'file',
    mime: 'text/plain',
    filename: 'float-notes.txt',
  })
  expect(floatingPromptBody.parts[1].url).toMatch(/^data:text\/plain;base64,/)
  await expect(floatingPanel.getByText(floatingText, { exact: true })).toBeVisible()
  await expect(floatingPanel.locator('.x-bubble-list .x-attachment-card')).toContainText('float-notes.txt')

  await floatingPanel.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(floatingPanel).toBeHidden()
  await expect(page.locator('.tiptap')).toHaveAttribute('contenteditable', 'true')
  await expect(page.locator('fieldset.ecore-bar')).not.toHaveAttribute('disabled', '')
  await expect(page.getByPlaceholder('添加标签')).toBeEnabled()
  await expect(page.getByTitle('移到回收站')).toBeEnabled()
  await expect(page.locator('.dsh-header-title')).toHaveAttribute('aria-disabled', 'false')
  const editDuringAgent = `BROWSER EDIT DURING Agent ${suffix}`
  await editor.press('Control+End')
  await page.keyboard.type(`\n${editDuringAgent}`)
  await expect(page.locator('.dsh-header-save-status')).toHaveText(/草稿已保护|保存中…/)
  await expect(page.locator('.dsh-header-save-status')).toHaveText('已保存', { timeout: 10_000 })
  const duringAgentRemote = await (
    await page.request.get(workspaceAPI(`/api/notes/${encodeURIComponent(title)}`))
  ).json()
  expect(duringAgentRemote.content).toContain(editDuringAgent)

  await page.locator('.agent-fab').click()
  await expect(floatingPanel).toBeVisible()
  await floatingPanel.getByRole('button', { name: '停止', exact: true }).click()
  await expect(floatingPanel.getByRole('button', { name: '发送', exact: true })).toBeVisible()

  await page.goto(workspacePath('/agent'))
  const compactAgentSessions = await openAgentSessions(page)
  const sessionItems = page.locator('.x-conversations-item')
  await expect.poll(() => sessionItems.count()).toBeGreaterThan(0)
  const sessionCount = await sessionItems.count()
  await page.getByRole('button', { name: '新对话', exact: true }).click()
  if (compactAgentSessions) await expect(page.getByRole('dialog', { name: '对话列表' })).toBeHidden()
  await openAgentSessions(page)
  await expect(sessionItems).toHaveCount(sessionCount + 1)
  const activeSession = sessionItems.first()
  await expect(activeSession).toHaveClass(/active/)
  const sessionTitle = `Agent 会话 ${suffix}`
  await activeSession.getByTitle('更多操作').click()
  await page.getByRole('menuitem', { name: '重命名' }).click()
  const sessionTitleInput = activeSession.getByLabel('会话名称')
  await expect(sessionTitleInput).toBeVisible()
  await expect(page.getByRole('heading', { name: '重命名会话' })).toHaveCount(0)
  await sessionTitleInput.fill('不会保存的名称')
  await sessionTitleInput.press('Escape')
  await expect(sessionTitleInput).toBeHidden()

  await activeSession.getByTitle('更多操作').click()
  await page.getByRole('menuitem', { name: '重命名' }).click()
  await sessionTitleInput.fill(sessionTitle)
  await sessionTitleInput.press('Enter')
  await expect(activeSession.locator('.agent-chat-session-title')).toHaveText(sessionTitle)
  await expect(page.getByRole('status').filter({ hasText: '已重命名' }).first()).toBeVisible()

  const blurredSessionTitle = `${sessionTitle} blur`
  await activeSession.getByTitle('更多操作').click()
  await page.getByRole('menuitem', { name: '重命名' }).click()
  await sessionTitleInput.fill(blurredSessionTitle)
  await sessionTitleInput.blur()
  await expect(activeSession.locator('.agent-chat-session-title')).toHaveText(blurredSessionTitle)

  const otherSession = sessionItems.nth(1)
  const otherSessionID = await otherSession.getAttribute('data-key')
  expect(otherSessionID).toBeTruthy()
  let markHistoryStarted!: () => void
  let markHistoryFinished!: () => void
  let releaseHistory!: () => void
  const historyStarted = new Promise<void>((resolve) => {
    markHistoryStarted = resolve
  })
  const historyFinished = new Promise<void>((resolve) => {
    markHistoryFinished = resolve
  })
  const historyReleased = new Promise<void>((resolve) => {
    releaseHistory = resolve
  })
  let historyDelayed = false
  const messageRoute = `**${workspaceAPI('/api/agent/session/*/message')}`
  await page.route(messageRoute, async (route) => {
    const shouldDelay =
      !historyDelayed &&
      route.request().method() === 'GET' &&
      route.request().url().includes(`/session/${otherSessionID}/message`)
    if (shouldDelay) {
      historyDelayed = true
      markHistoryStarted()
      await historyReleased
    }
    await route.continue()
    if (shouldDelay) markHistoryFinished()
  })

  await otherSession.click()
  await historyStarted
  expect(await sessionItems.count()).toBe(sessionCount + 1)
  expect(await page.locator('.x-conversations-loading').count()).toBe(0)
  await expect(otherSession).toHaveClass(/active/)
  await activeSession.click()
  if (compactAgentSessions) {
    await expect(page.getByRole('dialog', { name: '对话列表' })).toBeHidden()
    await openAgentSessions(page)
  }
  await expect(activeSession).toHaveClass(/active/)
  releaseHistory()
  await historyFinished
  await page.unroute(messageRoute)
  await expect(page.getByText(floatingText, { exact: true })).toHaveCount(0)

  const agentInput = page.locator('.agent-chat-input .x-sender-input')
  await expect(agentInput).toBeVisible()
  const imageChooser = page.waitForEvent('filechooser')
  await page.getByRole('button', { name: '添加图片' }).click()
  await (
    await imageChooser
  ).setFiles({
    name: 'agent-pixel.png',
    mimeType: 'image/png',
    buffer: Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
      'base64',
    ),
  })
  const attachmentChooser = page.waitForEvent('filechooser')
  await page.getByRole('button', { name: '添加附件' }).click()
  await (
    await attachmentChooser
  ).setFiles({
    name: 'agent-notes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('Agent attachment text'),
  })
  const composerAttachments = page.locator('.agent-composer .x-attachments')
  await expect(composerAttachments).toContainText('agent-pixel.png')
  await expect(composerAttachments).toContainText('agent-notes.txt')

  const slashText = `/compact ${suffix}`
  const slashPromptRequest = page.waitForRequest(
    (request) => request.method() === 'POST' && request.url().includes('/prompt_async'),
  )
  await agentInput.fill(slashText)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  const promptBody = (await slashPromptRequest).postDataJSON()
  expect(promptBody.parts[0].text).toBe(slashText)
  expect(promptBody.model).toBeUndefined()
  expect(promptBody.parts.slice(1).map((part: { filename: string }) => part.filename)).toEqual([
    'agent-pixel.png',
    'agent-notes.txt',
  ])
  expect(promptBody.parts[1]).toMatchObject({ type: 'file', mime: 'image/png' })
  expect(promptBody.parts[1].url).toMatch(/^data:image\/png;base64,/)
  expect(promptBody.parts[2]).toMatchObject({ type: 'file', mime: 'text/plain' })
  expect(promptBody.parts[2].url).toMatch(/^data:text\/plain;base64,/)
  await expect(page.getByText(slashText, { exact: true })).toBeVisible()
  await expect(page.locator('.x-bubble-list .x-attachment-card')).toContainText(['agent-pixel.png', 'agent-notes.txt'])
  await expect(composerAttachments).toHaveCount(0)
  await expect(agentInput).toHaveAttribute('placeholder', '智能体正在处理，可点击停止')
  const stopAction = page.locator('.agent-composer .x-sender-action-stop')
  await expect(stopAction).toBeVisible()
  expect(await stopAction.evaluate((element) => getComputedStyle(element, '::before').animationName)).not.toBe('none')
  await expect(page.locator('.agent-message-assistant .x-think')).toBeVisible()
  const thinking = page.locator('.agent-message-assistant .x-think')
  await expect(thinking).toContainText('正在思考')
  const shimmer = thinking.locator('.x-text-shimmer')
  await expect(shimmer).toHaveText('正在思考')
  const shimmerCharacters = shimmer.locator('.x-text-shimmer-char')
  await expect(shimmerCharacters).toHaveCount(4)
  const shimmerStyle = await shimmerCharacters.first().evaluate((element) => {
    const style = getComputedStyle(element)
    return { animationName: style.animationName, animationPlayState: style.animationPlayState }
  })
  expect(shimmerStyle.animationName).not.toBe('none')
  expect(shimmerStyle.animationPlayState).toBe('running')
  for (const colorScheme of ['light', 'dark']) {
    await page.evaluate((scheme) => (document.documentElement.dataset.colorScheme = scheme), colorScheme)
    const before = await shimmerCharacters.evaluateAll((elements) =>
      elements.map((element) => {
        const style = getComputedStyle(element)
        return [style.color, style.opacity, style.transform, style.textShadow].join('|')
      }),
    )
    await page.waitForTimeout(180)
    const after = await shimmerCharacters.evaluateAll((elements) =>
      elements.map((element) => {
        const style = getComputedStyle(element)
        return [style.color, style.opacity, style.transform, style.textShadow].join('|')
      }),
    )
    expect(after).not.toEqual(before)
  }
  await expect(page.locator('.agent-message-thinking')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '停止', exact: true })).toBeVisible()

  const promptedSessionID = await page.evaluate(() => localStorage.getItem('marvo.agent.currentSessionId'))
  expect(promptedSessionID).toBeTruthy()
  const upstreamMessages = await (
    await page.request.get(workspaceAPI(`/api/agent/session/${promptedSessionID}/message`))
  ).json()
  const injectedPrompt = [...upstreamMessages]
    .reverse()
    .find((message: any) => message.parts?.some((part: any) => part.type === 'text' && part.text === slashText))
  expect(injectedPrompt?.info?.model).toEqual({ providerID: 'fake', modelID: 'vision' })
  expect(injectedPrompt?.info?.system || '').not.toContain(globalPrompt)

  await page.reload()
  await expect(page.getByText(slashText, { exact: true })).toBeVisible()
  await expect(page.locator('.x-bubble-list .x-attachment-card')).toContainText(['agent-pixel.png', 'agent-notes.txt'])
  await expect(page.getByRole('button', { name: '停止', exact: true })).toBeVisible()
  await page.route(
    new RegExp(workspaceAPIRegex(`/api/agent/session/${promptedSessionID}/abort(?:\\?.*)?$`)),
    async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 600))
      await route.continue()
    },
  )
  await page.getByRole('button', { name: '停止', exact: true }).click()
  await expect(agentInput).toHaveAttribute('placeholder', '正在停止…')
  await expect(page.getByRole('button', { name: '正在停止', exact: true })).toBeVisible()
  await expect(page.locator('.agent-message-assistant .x-think-loading')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeVisible()

  const fitsViewport = await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
  expect(fitsViewport).toBeTruthy()
})
