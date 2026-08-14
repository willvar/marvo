import { expect, test } from '@playwright/test'
import {
  approveDevice,
  authenticateUserAdministrator,
  openAgentSessions,
  workspaceAPI,
  workspaceAPIRegex,
  workspacePath,
} from './helpers'

test('低高度窄屏中的智能体保留完整消息区、输入框和会话抽屉', async ({ page }) => {
  await approveDevice(page, 'PW compact Agent layout')
  const created = await page.request.post(workspaceAPI('/api/agent/session'))
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.setViewportSize({ width: 360, height: 420 })
  await page.goto(workspacePath('/agent'))

  const composer = page.locator('.agent-chat-input .x-sender')
  const messages = page.locator('.agent-chat-main .x-bubble-list')
  await expect(page.locator('.agent-chat-sessions')).toHaveCount(0)
  await expect(page.locator('.agent-chat-mobile-toolbar')).toBeVisible()
  await expect(composer).toBeVisible()
  await expect(messages).toBeVisible()

  const [composerBounds, messageBounds] = await Promise.all([composer.boundingBox(), messages.boundingBox()])
  expect(composerBounds).not.toBeNull()
  expect(messageBounds).not.toBeNull()
  expect(composerBounds!.y + composerBounds!.height).toBeLessThanOrEqual(420)
  expect(messageBounds!.height).toBeGreaterThan(80)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360)

  expect(await openAgentSessions(page)).toBe(true)
  const drawer = page.getByRole('dialog', { name: '对话列表' })
  const creation = drawer.getByRole('button', { name: '新对话', exact: true })
  const more = drawer.getByRole('button', { name: '更多操作' }).first()
  const [drawerBounds, creationBounds, moreBounds] = await Promise.all([
    drawer.boundingBox(),
    creation.boundingBox(),
    more.boundingBox(),
  ])
  expect(drawerBounds).not.toBeNull()
  expect(drawerBounds!.x).toBeGreaterThanOrEqual(0)
  expect(drawerBounds!.y).toBeGreaterThanOrEqual(0)
  expect(drawerBounds!.x + drawerBounds!.width).toBeLessThanOrEqual(360)
  expect(drawerBounds!.y + drawerBounds!.height).toBeLessThanOrEqual(420)
  expect(creationBounds!.height).toBeGreaterThanOrEqual(44)
  expect(moreBounds!.width).toBeGreaterThanOrEqual(40)
  expect(moreBounds!.height).toBeGreaterThanOrEqual(40)

  await creation.click()
  await expect(drawer).toBeHidden()
  await expect(composer).toBeVisible()
})

test('窄屏智能体提问保持横向可读并提供足够触控热区', async ({ page }, testInfo) => {
  test.skip((page.viewportSize()?.width || 1024) > 768)
  await approveDevice(page, `PW compact question ${testInfo.project.name}`)
  const created = await page.request.post(workspaceAPI('/api/agent/session'))
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  await page.route(new RegExp(workspaceAPIRegex('/api/agent/question(?:\\?.*)?$')), (route) =>
    route.fulfill({
      json: [
        {
          id: 'question-compact-e2e',
          sessionID: session.id,
          questions: [
            {
              header: '优先级',
              question: '这项工作应该按什么优先级处理？',
              options: [
                { label: '优先处理', description: '先完成关键内容' },
                { label: '正常处理', description: '按照当前顺序继续' },
              ],
              multiple: false,
              custom: true,
            },
            {
              header: '范围',
              question: '需要覆盖哪些内容？',
              options: [{ label: '全部内容', description: '检查完整范围' }],
              multiple: true,
              custom: true,
            },
            {
              header: '确认',
              question: '现在开始吗？',
              options: [{ label: '开始', description: '立即执行' }],
              multiple: false,
              custom: false,
            },
          ],
        },
      ],
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto(workspacePath('/agent'))

  const panel = page.locator('.agent-chat-input .agent-question')
  const question = panel.locator('.agent-request-question-text')
  await expect(panel).toBeVisible()
  await expect(question).toHaveText('这项工作应该按什么优先级处理？')
  const [panelBounds, questionBounds] = await Promise.all([panel.boundingBox(), question.boundingBox()])
  const viewport = page.viewportSize()
  expect(panelBounds).not.toBeNull()
  expect(questionBounds).not.toBeNull()
  expect(viewport).not.toBeNull()
  expect(panelBounds!.x).toBeGreaterThanOrEqual(0)
  expect(panelBounds!.x + panelBounds!.width).toBeLessThanOrEqual(viewport!.width)
  expect(panelBounds!.y + panelBounds!.height).toBeLessThanOrEqual(viewport!.height)
  expect(questionBounds!.width).toBeGreaterThan(220)

  const progressButtons = panel.locator('.agent-question-progress button')
  await expect(progressButtons).toHaveCount(3)
  for (const bounds of await progressButtons.evaluateAll((buttons) =>
    buttons.map((button) => {
      const box = button.getBoundingClientRect()
      return { width: box.width, height: box.height }
    }),
  )) {
    expect(bounds.width).toBeGreaterThanOrEqual(40)
    expect(bounds.height).toBeGreaterThanOrEqual(40)
  }
  await expect(panel.locator('.agent-question-option').first()).toBeVisible()
  await panel.locator('.agent-question-option').first().click()
  await expect(panel.getByRole('button', { name: '下一题' })).toBeEnabled()
})

test('窄屏用户后台改用侧滑导航并将设备表格重排为可操作卡片', async ({ page }) => {
  test.skip((page.viewportSize()?.width || 1024) > 768)
  await approveDevice(page, 'PW compact admin layout')
  await authenticateUserAdministrator(page)
  await page.goto(workspacePath('/admin'))

  await expect(page.locator('.admin-sidebar')).toBeHidden()
  const navigationTrigger = page.getByRole('button', { name: '打开后台导航' })
  await expect(navigationTrigger).toBeVisible()
  await expect(navigationTrigger).toHaveAttribute('aria-expanded', 'false')
  const triggerBounds = await navigationTrigger.boundingBox()
  expect(triggerBounds).not.toBeNull()
  expect(triggerBounds!.width).toBeGreaterThanOrEqual(40)
  expect(triggerBounds!.height).toBeGreaterThanOrEqual(40)

  await navigationTrigger.click()
  const drawer = page.getByRole('dialog', { name: 'Marvo', exact: true })
  const navigation = drawer.getByRole('navigation', { name: '后台导航' })
  await expect(drawer).toBeVisible()
  await expect.poll(() => drawer.evaluate((element) => getComputedStyle(element).transform)).toBe('none')
  await expect(navigation.getByRole('link', { name: '设备审批' })).toHaveAttribute('aria-current', 'page')
  const navigationIcons = navigation.locator('.anticon')
  expect(await navigationIcons.count()).toBeGreaterThanOrEqual(4)
  for (const bounds of await navigationIcons.evaluateAll((icons) =>
    icons.map((icon) => {
      const box = icon.getBoundingClientRect()
      return { width: box.width, height: box.height }
    }),
  )) {
    expect(bounds.width).toBeGreaterThan(0)
    expect(bounds.height).toBeGreaterThan(0)
  }
  const drawerBounds = await drawer.boundingBox()
  expect(drawerBounds).not.toBeNull()
  expect(Math.abs(drawerBounds!.x)).toBeLessThanOrEqual(0.5)
  expect(drawerBounds!.width).toBeLessThan(page.viewportSize()!.width)
  await drawer.getByRole('button', { name: '关闭后台导航' }).click()
  await expect(drawer).toBeHidden()
  await expect(navigationTrigger).toHaveAttribute('aria-expanded', 'false')

  await page.getByRole('button', { name: /已批准设备/ }).click()
  const table = page.locator('.admin-table')
  const firstCard = table.locator('tbody tr').first()
  await expect(firstCard).toBeVisible()
  await expect(table).toHaveCSS('display', 'block')
  await expect(firstCard.locator('td[data-label="设备名称"]')).toBeVisible()
  await expect(firstCard.locator('td[data-label="批准时间"]')).toBeVisible()
  const actions = firstCard.locator('td[data-label="操作"] .admin-btn')
  await expect(actions).toHaveCount(2)

  const actionBounds = await actions.evaluateAll((buttons) =>
    buttons.map((button) => {
      const box = button.getBoundingClientRect()
      return { x: box.x, y: box.y, width: box.width, height: box.height }
    }),
  )
  for (const bounds of actionBounds) {
    expect(bounds.x).toBeGreaterThanOrEqual(0)
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(390)
    expect(bounds.height).toBeGreaterThanOrEqual(40)
  }
  expect(Math.abs(actionBounds[0].y - actionBounds[1].y)).toBeLessThanOrEqual(1)
  await expect(page.locator('.admin-workspace-entry')).toHaveCSS('min-height', '40px')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)

  await page.setViewportSize({ width: 800, height: 1100 })
  await expect(page.locator('.admin-sidebar')).toBeHidden()
  await expect(page.getByRole('button', { name: '打开后台导航' })).toBeVisible()

  await page.setViewportSize({ width: 1100, height: 800 })
  await expect(page.locator('.admin-sidebar')).toBeVisible()
  await expect(page.getByRole('button', { name: '打开后台导航' })).toBeHidden()
})

test('横屏用户后台收起侧栏后所有图标保持同一中轴', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await authenticateUserAdministrator(page)
  await page.goto(workspacePath('/admin'))

  const sidebar = page.locator('.admin-sidebar')
  await page.getByRole('button', { name: '收起后台导航' }).click()
  await expect(sidebar).toHaveClass(/collapsed/)
  await expect(sidebar).toHaveCSS('width', '64px')

  const icons = sidebar.locator(
    '.admin-sidebar-brand > .marvo-mark, .admin-sidebar-nav a > .anticon, .admin-sidebar-toggle > .anticon',
  )
  expect(await icons.count()).toBeGreaterThanOrEqual(6)
  await expect
    .poll(async () => {
      const centers = await icons.evaluateAll((elements) =>
        elements.map((element) => {
          const bounds = element.getBoundingClientRect()
          return bounds.left + bounds.width / 2
        }),
      )
      return Math.max(...centers) - Math.min(...centers)
    })
    .toBeLessThanOrEqual(0.5)
})

test('移动端未固定的浮动智能体可通过外部区域关闭', async ({ page }) => {
  test.skip((page.viewportSize()?.width || 1024) > 768)
  await approveDevice(page, 'PW mobile Agent outside close')
  await page.evaluate(() => localStorage.removeItem('marvo.agentFloating.pinned'))
  await page.reload()

  await page.locator('.agent-fab').click()
  const panel = page.locator('.agent-float-mobile')
  await expect(panel).toBeVisible()
  await expect.poll(() => panel.evaluate((element) => getComputedStyle(element).transform)).toBe('none')
  const panelBounds = await panel.boundingBox()
  const viewport = page.viewportSize()
  expect(panelBounds).not.toBeNull()
  expect(viewport).not.toBeNull()
  expect(panelBounds!.y).toBeGreaterThanOrEqual(0)
  expect(panelBounds!.y + panelBounds!.height).toBeLessThanOrEqual(viewport!.height)

  const panelActions = panel.locator('.agent-float-actions button')
  for (const bounds of await panelActions.evaluateAll((buttons) =>
    buttons.map((button) => {
      const box = button.getBoundingClientRect()
      return { width: box.width, height: box.height }
    }),
  )) {
    expect(bounds.width).toBeGreaterThanOrEqual(40)
    expect(bounds.height).toBeGreaterThanOrEqual(40)
  }

  await page.locator('.dialog-backdrop').click({ position: { x: 8, y: 8 } })
  await expect(panel).toBeHidden()
  await expect(page.locator('.agent-fab')).toHaveAttribute('aria-expanded', 'false')
})

test('窄屏主要工作区与用户后台页面均不产生整页横向溢出', async ({ page }, testInfo) => {
  test.skip((page.viewportSize()?.width || 1024) > 768)
  await approveDevice(page, `PW compact routes ${testInfo.project.name}`)

  const title = `窄屏布局 ${testInfo.project.name} ${Date.now()}`
  const created = await page.request.post(workspaceAPI('/api/notes'), {
    data: { title, content: '用于检查窄屏笔记页面。', tags: ['窄屏'] },
  })
  expect(created.ok()).toBeTruthy()

  const workspaceRoutes = ['', `/note/${encodeURIComponent(title)}`, '/agent', '/trash']
  for (const path of workspaceRoutes) {
    await page.goto(workspacePath(path))
    await expect(page.locator('.dsh')).toBeVisible()
    await expect
      .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth))
      .toBe(true)
  }

  await authenticateUserAdministrator(page)
  const adminRoutes = ['/admin', '/admin/settings', '/admin/agent', '/admin/security']
  for (const path of adminRoutes) {
    await page.goto(workspacePath(path))
    await expect(page.locator('.admin-shell')).toBeVisible()
    await expect
      .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth))
      .toBe(true)
  }
})
