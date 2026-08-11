import { expect, test } from '@playwright/test'
import { approveDevice, createLongAgentSession } from './helpers'

test('进入和切换长 Agent 会话后定位到消息底部', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent conversation scroll')
  const firstID = await createLongAgentSession(page, 'FIRST SCROLL SESSION')
  const secondID = await createLongAgentSession(page, 'SECOND SCROLL SESSION')
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), firstID)
  await page.goto('/agent')

  const messageScroller = page.locator('.x-bubble-list-scroll')
  await expect(page.getByText('FIRST SCROLL SESSION 18', { exact: false })).toBeAttached()
  const timedBubble = page
    .locator('.x-bubble')
    .filter({ has: page.locator('.x-bubble-header time') })
    .first()
  const timeBounds = await timedBubble.locator('.x-bubble-header time').boundingBox()
  const contentBounds = await timedBubble.locator('.x-bubble-content').boundingBox()
  expect(timeBounds).not.toBeNull()
  expect(contentBounds).not.toBeNull()
  expect(timeBounds!.y + timeBounds!.height).toBeLessThanOrEqual(contentBounds!.y)
  const messageScrollbar = await messageScroller.evaluate((element) => {
    const style = getComputedStyle(element)
    return { width: style.scrollbarWidth, color: style.scrollbarColor }
  })
  const conversationScrollbar = await page.locator('.x-conversations-list').evaluate((element) => {
    const style = getComputedStyle(element)
    return { width: style.scrollbarWidth, color: style.scrollbarColor }
  })
  expect(conversationScrollbar).toEqual(messageScrollbar)
  await expect
    .poll(() => messageScroller.evaluate((element) => element.scrollHeight - element.scrollTop - element.clientHeight))
    .toBeLessThanOrEqual(2)

  await messageScroller.evaluate((element) => {
    element.scrollTop = 0
    element.dispatchEvent(new Event('scroll'))
  })
  const jumpToBottom = page.getByRole('button', { name: '回到底部' })
  await expect(jumpToBottom).toBeVisible()
  await expect(jumpToBottom).not.toContainText('回到底部')
  await jumpToBottom.click()
  await expect
    .poll(() => messageScroller.evaluate((element) => element.scrollHeight - element.scrollTop - element.clientHeight))
    .toBeLessThanOrEqual(2)
  await expect(jumpToBottom).toHaveCount(0)

  await messageScroller.evaluate((element) => {
    element.scrollTop = 0
    element.dispatchEvent(new Event('scroll'))
  })
  await page.locator(`.x-conversations-item[data-key="${secondID}"]`).click()
  await expect(page.getByText('SECOND SCROLL SESSION 18', { exact: false })).toBeAttached()
  await expect
    .poll(() => messageScroller.evaluate((element) => element.scrollHeight - element.scrollTop - element.clientHeight))
    .toBeLessThanOrEqual(2)
})

test('子会话提问和权限请求会在根会话完成响应', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent child requests')
  const created = await page.request.post('/api/agent/session')
  const alternateCreated = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  expect(alternateCreated.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const alternate = (await alternateCreated.json()) as { id: string }
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')
  await expect(page.locator(`.x-conversations-item[data-key="${session.id}"]`)).toHaveClass(/active/)

  const childID = `${session.id}-child`
  await page.evaluate(
    async ({ rootID, childSessionID }) => {
      const storeModulePath = '/src/stores/agent.ts'
      const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
      const store = useAgentStore()
      store.allSessions = [
        ...store.allSessions.filter((item: { id: string }) => item.id !== childSessionID),
        {
          id: childSessionID,
          parentID: rootID,
          title: '后台任务',
          time: { created: Date.now(), updated: Date.now() },
        },
      ]
      store.questions = {
        ...store.questions,
        [childSessionID]: [
          {
            id: 'question-child-e2e',
            sessionID: childSessionID,
            questions: [
              {
                header: '执行方式',
                question: '希望怎样处理？',
                options: [
                  { label: '快速处理', description: '优先完成主要内容' },
                  { label: '完整处理', description: '覆盖更多细节' },
                ],
                multiple: false,
                custom: true,
              },
              {
                header: '保留内容',
                question: '需要保留哪些内容？',
                options: [{ label: '保留图片', description: '继续引用现有图片' }],
                multiple: true,
                custom: true,
              },
            ],
          },
        ],
      }
    },
    { rootID: session.id, childSessionID: childID },
  )

  const rootItem = page.locator(`.x-conversations-item[data-key="${session.id}"]`)
  await expect(rootItem.locator('.x-conversations-status-attention')).toBeVisible()
  const questionPanel = page.locator('.agent-chat-input .agent-question')
  await expect(questionPanel).toContainText('第 1 题，共 2 题')
  await expect(page.locator('.agent-chat-input .agent-composer')).toHaveCount(0)
  await questionPanel.locator('.agent-question-option', { hasText: '快速处理' }).click()
  await questionPanel.getByRole('button', { name: '下一题' }).click()
  await expect(questionPanel).toContainText('第 2 题，共 2 题')
  await questionPanel.locator('.agent-question-option', { hasText: '保留图片' }).click()
  await questionPanel.locator('.agent-question-custom-option').click()
  const customAnswer = questionPanel.getByPlaceholder('输入答案…')
  await customAnswer.click()
  await customAnswer.fill('保留引用链接')
  await page.locator(`.x-conversations-item[data-key="${alternate.id}"]`).click()
  await expect(questionPanel).toHaveCount(0)
  await rootItem.click()
  await expect(questionPanel).toContainText('第 2 题，共 2 题')
  await expect(questionPanel.getByPlaceholder('输入答案…')).toHaveValue('保留引用链接')
  const questionReply = page.waitForRequest(
    (request) => request.method() === 'POST' && request.url().includes('/question/question-child-e2e/reply'),
  )
  await questionPanel.getByRole('button', { name: '提交' }).click()
  expect((await questionReply).postDataJSON()).toEqual({ answers: [['快速处理'], ['保留图片', '保留引用链接']] })
  await expect(questionPanel).toHaveCount(0)
  await expect(page.locator('.agent-chat-input .agent-composer')).toBeVisible()

  await page.evaluate(async (childSessionID) => {
    const storeModulePath = '/src/stores/agent.ts'
    const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
    const store = useAgentStore()
    store.permissions = {
      ...store.permissions,
      [childSessionID]: [
        {
          id: 'permission-child-e2e',
          sessionID: childSessionID,
          permission: 'bash',
          patterns: ['make test'],
          metadata: {},
          always: [],
        },
      ],
    }
  }, childID)

  const permissionPanel = page.locator('.agent-chat-input .agent-permission')
  await expect(permissionPanel).toContainText('执行命令')
  await expect(permissionPanel).toContainText('make test')
  await expect(page.locator('.agent-chat-input .agent-composer')).toHaveCount(0)
  await expect(permissionPanel.getByRole('button', { name: '允许本次' })).toHaveClass(/x-button-primary/)
  await expect(permissionPanel.getByRole('button', { name: '始终允许' })).toHaveClass(/x-button-secondary/)
  const permissionReply = page.waitForRequest(
    (request) => request.method() === 'POST' && request.url().includes('/permission/permission-child-e2e/reply'),
  )
  await permissionPanel.getByRole('button', { name: '允许本次' }).click()
  expect((await permissionReply).postDataJSON()).toEqual({ reply: 'once' })
  await expect(permissionPanel).toHaveCount(0)
  await expect(page.locator('.agent-chat-input .agent-composer')).toBeVisible()
  await expect(rootItem.locator('.x-conversations-status-attention')).toHaveCount(0)
})

test('刷新页面时会补齐待响应请求所属的子会话', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent child request restore')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const childID = `${session.id}-restored-child`

  await page.route(/\/api\/agent\/question(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: [
        {
          id: 'question-restored-child',
          sessionID: childID,
          questions: [
            {
              header: '恢复后的提问',
              question: '页面刷新后仍然可以看到吗？',
              options: [{ label: '可以', description: '继续处理' }],
              multiple: false,
              custom: false,
            },
          ],
        },
      ],
    }),
  )
  await page.route(new RegExp(`/api/agent/session/${childID}(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: {
        id: childID,
        parentID: session.id,
        title: '后台任务',
        time: { created: Date.now(), updated: Date.now() },
      },
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  await expect(page.locator('.agent-chat-input .agent-question')).toContainText('页面刷新后仍然可以看到吗？')
  await expect(
    page.locator(`.x-conversations-item[data-key="${session.id}"] .x-conversations-status-attention`),
  ).toBeVisible()
})

test('子任务卡片可进入只读子会话并返回主对话', async ({ page }, testInfo) => {
  await approveDevice(page, `Playwright Agent subtask navigation ${testInfo.project.name}`)
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const childID = `${session.id}-visible-child`
  const now = Date.now()

  await page.route(new RegExp(`/api/agent/session/${session.id}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: { id: 'subtask-user-root', role: 'user', sessionID: session.id, time: { created: now } },
          parts: [
            {
              id: 'subtask-user-root-text',
              type: 'text',
              messageID: 'subtask-user-root',
              sessionID: session.id,
              text: '核对资料',
            },
          ],
        },
        {
          info: {
            id: 'subtask-assistant-root',
            role: 'assistant',
            parentID: 'subtask-user-root',
            sessionID: session.id,
            time: { created: now + 1, completed: now + 2 },
          },
          parts: [
            {
              id: 'subtask-tool-root',
              callID: 'subtask-tool-call-root',
              type: 'tool',
              tool: 'task',
              messageID: 'subtask-assistant-root',
              sessionID: session.id,
              state: {
                status: 'completed',
                input: { description: '核对历史资料', subagent_type: 'explore' },
                output: '已完成',
                metadata: { sessionId: childID },
              },
            },
            {
              id: 'subtask-answer-root',
              type: 'text',
              messageID: 'subtask-assistant-root',
              sessionID: session.id,
              text: '资料已经核对完成。',
            },
          ],
        },
      ],
    }),
  )
  await page.route(new RegExp(`/api/agent/session/${childID}(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: {
        id: childID,
        parentID: session.id,
        title: '核对历史资料 (@explore subagent)',
        time: { created: now + 1, updated: now + 2 },
      },
    }),
  )
  await page.route(new RegExp(`/api/agent/session/${childID}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: { id: 'subtask-user-child', role: 'user', sessionID: childID, time: { created: now + 1 } },
          parts: [
            {
              id: 'subtask-user-child-input',
              type: 'subtask',
              messageID: 'subtask-user-child',
              sessionID: childID,
              agent: 'explore',
              description: '核对历史资料',
              prompt: '核对历史资料',
            },
          ],
        },
        {
          info: {
            id: 'subtask-assistant-child',
            role: 'assistant',
            parentID: 'subtask-user-child',
            sessionID: childID,
            time: { created: now + 2, completed: now + 3 },
          },
          parts: [
            {
              id: 'subtask-answer-child',
              type: 'text',
              messageID: 'subtask-assistant-child',
              sessionID: childID,
              text: '子任务查到两条相关资料。',
            },
          ],
        },
      ],
    }),
  )

  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  const card = page.getByRole('button', { name: '查看子任务：核对历史资料' })
  await expect(card).toBeVisible()
  await expect(card).toContainText('探索智能体')
  await card.click()

  await expect(page.locator('.agent-chat-subtask-header')).toContainText('核对历史资料')
  await expect(page.getByText('子任务查到两条相关资料。', { exact: true })).toBeVisible()
  await expect(page.locator('.agent-chat-subtask-readonly')).toContainText('子任务记录为只读')
  await expect(page.locator('.agent-chat-input .agent-composer')).toHaveCount(0)
  await expect(page.locator(`.x-conversations-item[data-key="${session.id}"]`)).toHaveClass(/active/)

  await page.getByRole('button', { name: '返回主对话' }).click()
  await expect(page.getByText('资料已经核对完成。', { exact: true })).toBeVisible()
  await expect(page.locator('.agent-chat-input .agent-composer')).toBeVisible()
})

test('浮动智能体可从子任务卡片进入只读记录', async ({ page }, testInfo) => {
  await approveDevice(page, `Playwright floating Agent subtask navigation ${testInfo.project.name}`)
  const rootID = 'floating-subtask-root'
  const childID = 'floating-subtask-child'
  const now = Date.now()

  await page.route(new RegExp(`/api/agent/session/${childID}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: { id: 'floating-child-user', role: 'user', sessionID: childID, time: { created: now + 1 } },
          parts: [
            {
              id: 'floating-child-input',
              type: 'subtask',
              messageID: 'floating-child-user',
              sessionID: childID,
              agent: 'general',
              description: '整理浮动窗资料',
              prompt: '整理浮动窗资料',
            },
          ],
        },
        {
          info: {
            id: 'floating-child-assistant',
            role: 'assistant',
            parentID: 'floating-child-user',
            sessionID: childID,
            time: { created: now + 2, completed: now + 3 },
          },
          parts: [
            {
              id: 'floating-child-answer',
              type: 'text',
              messageID: 'floating-child-assistant',
              sessionID: childID,
              text: '浮动窗子任务记录。',
            },
          ],
        },
      ],
    }),
  )

  await page.goto('/')
  await page.locator('.agent-fab').click()
  const panel = page.locator('.agent-float-panel')
  await expect(panel).toBeVisible()
  await page.evaluate(
    async ({ parentSessionID, childSessionID, createdAt }) => {
      const storeModulePath = '/src/stores/agent.ts'
      const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
      const store = useAgentStore()
      store.floatingSessionId = parentSessionID
      store.allSessions = [
        ...store.allSessions.filter(
          (session: { id: string }) => session.id !== parentSessionID && session.id !== childSessionID,
        ),
        {
          id: parentSessionID,
          title: '浮动主对话',
          time: { created: createdAt, updated: createdAt },
        },
        {
          id: childSessionID,
          parentID: parentSessionID,
          title: '整理浮动窗资料 (@general subagent)',
          time: { created: createdAt + 1, updated: createdAt + 2 },
        },
      ]
      store.floatingMessages = [
        { id: 'floating-root-user', role: 'user', sessionID: parentSessionID, time: { created: createdAt } },
        {
          id: 'floating-root-assistant',
          role: 'assistant',
          parentID: 'floating-root-user',
          sessionID: parentSessionID,
          time: { created: createdAt + 1, completed: createdAt + 2 },
        },
      ]
      store.floatingParts = {
        'floating-root-user': [
          {
            id: 'floating-root-user-text',
            type: 'text',
            messageID: 'floating-root-user',
            sessionID: parentSessionID,
            text: '整理资料',
          },
        ],
        'floating-root-assistant': [
          {
            id: 'floating-root-task',
            callID: 'floating-root-task-call',
            type: 'tool',
            tool: 'task',
            messageID: 'floating-root-assistant',
            sessionID: parentSessionID,
            state: {
              status: 'completed',
              input: { description: '整理浮动窗资料', subagent_type: 'general' },
              metadata: { sessionId: childSessionID },
            },
          },
        ],
      }
    },
    { parentSessionID: rootID, childSessionID: childID, createdAt: now },
  )

  const card = panel.getByRole('button', { name: '查看子任务：整理浮动窗资料' })
  await expect(card).toBeVisible()
  await card.click()
  await expect(panel.getByText('浮动窗子任务记录。', { exact: true })).toBeVisible()
  await expect(panel.locator('.agent-assistant-readonly')).toContainText('子任务记录为只读')
  await expect(panel.locator('.agent-composer')).toHaveCount(0)

  await panel.getByRole('button', { name: '主对话' }).click()
  await expect(card).toBeVisible()
  await expect(panel.locator('.agent-composer')).toBeVisible()
})

test('智能体加载与子会话执行异常不会显示成空白状态', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent visible errors')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  let rejectSessionList = true
  await page.route(/\/api\/agent\/session(?:\?.*)?$/, async (route) => {
    if (route.request().method() !== 'GET' || !rejectSessionList) {
      await route.continue()
      return
    }
    await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ message: 'offline' }) })
  })
  await page.goto('/agent')
  const runtimeNotice = page.locator('.agent-chat-runtime-notice')
  await expect(runtimeNotice).toContainText('无法加载对话')
  await expect(page.locator('.x-conversations-empty')).toContainText('暂无对话')

  rejectSessionList = false
  await runtimeNotice.getByRole('button', { name: '重新连接' }).click()
  await expect(page.locator(`.x-conversations-item[data-key="${session.id}"]`)).toBeVisible()
  await page.locator(`.x-conversations-item[data-key="${session.id}"]`).click()

  const childID = `${session.id}-failed-child`
  await page.evaluate(
    async ({ rootID, childSessionID }) => {
      const storeModulePath = '/src/stores/agent.ts'
      const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
      const store = useAgentStore()
      store.allSessions = [
        ...store.allSessions,
        {
          id: childSessionID,
          parentID: rootID,
          title: '后台任务',
          time: { created: Date.now(), updated: Date.now() },
        },
      ]
      store.handleEvent({
        type: 'session.error',
        properties: {
          sessionID: childSessionID,
          error: { name: 'ContextOverflowError', data: { message: 'maximum context length exceeded' } },
        },
      } as never)
    },
    { rootID: session.id, childSessionID: childID },
  )

  await expect(runtimeNotice).toContainText('本次执行未完成')
  await expect(runtimeNotice).toContainText('当前对话内容过多，请新建对话后继续')
  await expect(
    page.locator(`.x-conversations-item[data-key="${session.id}"] .x-conversations-status-error`),
  ).toBeVisible()
})

test('Agent 输入草稿按会话分别恢复', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent conversation drafts')
  const firstResponse = await page.request.post('/api/agent/session')
  const secondResponse = await page.request.post('/api/agent/session')
  expect(firstResponse.ok()).toBeTruthy()
  expect(secondResponse.ok()).toBeTruthy()
  const first = (await firstResponse.json()) as { id: string }
  const second = (await secondResponse.json()) as { id: string }
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), first.id)
  await page.goto('/agent')

  const input = page.locator('.agent-chat-input .x-sender-input')
  await input.fill('第一段未发送草稿')
  await page.locator(`.x-conversations-item[data-key="${second.id}"]`).click()
  await expect(input).toHaveValue('')
  await input.fill('第二段未发送草稿')
  await page.locator(`.x-conversations-item[data-key="${first.id}"]`).click()
  await expect(input).toHaveValue('第一段未发送草稿')

  await page.reload()
  await expect(input).toHaveValue('第一段未发送草稿')
  await page.locator(`.x-conversations-item[data-key="${second.id}"]`).click()
  await expect(input).toHaveValue('第二段未发送草稿')
})

test('根路由 Agent 输入创建新会话并发送后进入 Agent 页面', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright home Agent launch')

  let createRequests = 0
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (request.method() === 'POST' && url.pathname === '/api/agent/session') createRequests++
  })

  const promptText = `HOME AGENT ${Date.now()}`
  const promptRequestPromise = page.waitForRequest(
    (request) =>
      request.method() === 'POST' &&
      request.url().includes('/api/agent/session/') &&
      request.url().endsWith('/prompt_async'),
  )
  const input = page.locator('.home-agent-composer .x-sender-input')
  await expect(input).toHaveAttribute('placeholder', '向智能体描述你想完成的内容...')
  await input.fill(promptText)
  await input.press('Enter')

  const promptRequest = await promptRequestPromise
  const promptPath = new URL(promptRequest.url()).pathname
  const sessionID = decodeURIComponent(promptPath.match(/\/api\/agent\/session\/([^/]+)\/prompt_async$/)?.[1] || '')
  expect(sessionID).not.toBe('')
  expect(promptRequest.postDataJSON().parts[0].text).toBe(promptText)
  await expect(page).toHaveURL('http://127.0.0.1:15080/agent')
  await expect(page.locator('.x-conversations-item.active')).toHaveAttribute('data-key', sessionID)
  await expect(page.getByText(promptText, { exact: true })).toBeVisible()
  expect(createRequests).toBe(1)
})

test('智能体附件会提示重复选择并显示发送前准备状态', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent attachment feedback')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  const imageInput = page.locator('.agent-chat-input .agent-composer-picker input[type="file"]').first()
  const duplicateFile = {
    name: 'duplicate.png',
    mimeType: 'image/png',
    buffer: Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
      'base64',
    ),
  }
  await imageInput.setInputFiles(duplicateFile)
  await imageInput.setInputFiles(duplicateFile)
  await expect(page.locator('.agent-composer-notice')).toHaveText('已忽略 1 个重复附件')
  await expect(page.locator('.agent-chat-input .x-attachment-card')).toHaveCount(1)

  await page.evaluate(() => {
    const readAsDataURL = FileReader.prototype.readAsDataURL
    FileReader.prototype.readAsDataURL = function delayedRead(blob: Blob) {
      window.setTimeout(() => readAsDataURL.call(this, blob), 500)
    }
  })
  const promptRequest = page.waitForRequest(
    (request) => request.method() === 'POST' && request.url().includes(`/session/${session.id}/prompt_async`),
  )
  await page.locator('.agent-chat-input .x-sender-input').fill('检查附件反馈')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  const preparing = page.locator('.agent-chat-input .x-attachment-preparing')
  await expect(preparing).toBeVisible()
  await expect(preparing).toContainText('正在准备…')
  const promptBody = (await promptRequest).postDataJSON()
  expect(promptBody.parts[1]).toMatchObject({ type: 'file', filename: 'duplicate.png', mime: 'image/png' })
  await expect(page.locator('.agent-chat-input .x-attachment-card')).toHaveCount(0)
})
