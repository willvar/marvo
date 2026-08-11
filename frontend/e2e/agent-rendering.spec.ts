import { expect, test } from '@playwright/test'
import { approveDevice } from './helpers'

test('已回答的智能体提问按纵向问答呈现', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright answered question layout')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const messageID = 'assistant-answered-question'
  const now = Date.now()

  await page.route(new RegExp(`/api/agent/session/${session.id}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: {
            id: messageID,
            role: 'assistant',
            sessionID: session.id,
            time: { created: now, completed: now + 100 },
          },
          parts: [
            {
              id: 'answered-question-intro',
              type: 'text',
              text: '我需要确认一个选项。',
              messageID,
              sessionID: session.id,
            },
            {
              id: 'answered-question-part',
              callID: 'answered-question-call',
              type: 'tool',
              tool: 'question',
              messageID,
              sessionID: session.id,
              state: {
                status: 'completed',
                input: { questions: [{ question: '下一组想挑战什么难度？' }] },
                metadata: { answers: [['先不玩了']] },
              },
            },
          ],
        },
      ],
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  const summary = page.locator('.x-question-summary')
  const intro = page.getByText('我需要确认一个选项。', { exact: true })
  const item = summary.locator('.x-question-summary-item')
  const question = item.locator(':scope > span')
  const answer = item.locator(':scope > b')
  await expect(summary).toContainText('已回答')
  await expect(question).toHaveText('下一组想挑战什么难度？')
  await expect(answer).toHaveText('先不玩了')
  await expect(summary).toHaveCSS('margin-bottom', '0px')

  const introBox = await intro.boundingBox()
  const summaryBox = await summary.boundingBox()
  const questionBox = await question.boundingBox()
  const answerBox = await answer.boundingBox()
  expect(introBox).not.toBeNull()
  expect(summaryBox).not.toBeNull()
  expect(questionBox).not.toBeNull()
  expect(answerBox).not.toBeNull()
  expect(summaryBox!.y - (introBox!.y + introBox!.height)).toBeGreaterThanOrEqual(12)
  expect(answerBox!.y).toBeGreaterThanOrEqual(questionBox!.y + questionBox!.height)
  expect(Math.abs(answerBox!.x - questionBox!.x)).toBeLessThan(2)
  expect(answerBox!.height).toBeLessThan(30)
})

test('多轮提问按真实顺序渲染且等待态不重复旧思考标题', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright question reasoning order')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const messageID = 'assistant-question-reasoning-order'
  const part = (id: string, value: Record<string, unknown>) => ({
    id,
    messageID,
    sessionID: session.id,
    ...value,
  })

  await page.route(/\/api\/agent\/session\/status(?:\?.*)?$/, (route) =>
    route.fulfill({ json: { [session.id]: { type: 'busy' } } }),
  )
  await page.route(new RegExp(`/api/agent/session/${session.id}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: { id: messageID, role: 'assistant', sessionID: session.id, time: { created: Date.now() } },
          parts: [
            part('reasoning-riddle-first', { type: 'reasoning', text: '## Choosing the first riddle' }),
            part('question-riddle-first', {
              callID: 'question-riddle-first-call',
              type: 'tool',
              tool: 'question',
              state: {
                status: 'completed',
                input: { questions: [{ question: '第一题？' }] },
                metadata: { answers: [['第一答']] },
              },
            }),
            part('riddle-feedback', { type: 'text', text: '第一题答对了。' }),
            part('reasoning-riddle-second', {
              type: 'reasoning',
              text: '## Selecting less common riddles with options',
            }),
            part('question-riddle-second', {
              callID: 'question-riddle-second-call',
              type: 'tool',
              tool: 'question',
              state: {
                status: 'completed',
                input: { questions: [{ question: '第二题？' }] },
                metadata: { answers: [['第二答']] },
              },
            }),
          ],
        },
      ],
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  const blocks = page
    .locator('.agent-message-assistant .x-bubble-content')
    .locator(':scope > :is(.x-think, .x-question-summary, .agent-message-text)')
  await expect(blocks).toHaveCount(6)
  const texts = (await blocks.allTextContents()).map((text) => text.replace(/\s+/g, ' ').trim())
  expect(texts[0]).toContain('Choosing the first riddle')
  expect(texts[1]).toContain('第一题？第一答')
  expect(texts[2]).toBe('第一题答对了。')
  expect(texts[3]).toContain('Selecting less common riddles with options')
  expect(texts[4]).toContain('第二题？第二答')
  expect(texts[5]).toContain('正在思考')
  expect(texts[5]).not.toContain('Selecting less common riddles with options')
  expect(texts.filter((text) => text.includes('Selecting less common riddles with options'))).toHaveLength(1)
})

test('Agent 重连和空闲收尾不会让已显示的回复消失', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent monotonic reconciliation')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const userID = 'monotonic-user'
  const assistantID = 'monotonic-assistant'
  const finalAssistantID = 'monotonic-assistant-final'
  const answer = '这条已经显示的完整回复不能被落后快照删除'
  const finalAnswer = '状态确认完成后的最终回复'
  const now = Date.now()

  await page.route(new RegExp(`/api/agent/session/${session.id}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: { id: userID, role: 'user', sessionID: session.id, time: { created: now } },
          parts: [{ id: 'monotonic-user-text', type: 'text', messageID: userID, text: '测试状态同步' }],
        },
      ],
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  await page.evaluate(
    async ({ sessionID, userMessageID, assistantMessageID, answerText, createdAt }) => {
      const storeModulePath = '/src/stores/agent.ts'
      const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
      const store = useAgentStore()
      store.disconnect()
      store.currentSessionId = sessionID
      store.setConversationCollection(sessionID, {
        messages: [
          { id: userMessageID, role: 'user', sessionID, time: { created: createdAt } },
          {
            id: assistantMessageID,
            role: 'assistant',
            parentID: userMessageID,
            sessionID,
            time: { created: createdAt + 10 },
          },
        ],
        parts: {
          [userMessageID]: [
            { id: 'monotonic-user-text', type: 'text', messageID: userMessageID, text: '测试状态同步' },
          ],
          [assistantMessageID]: [
            { id: 'monotonic-answer', type: 'text', messageID: assistantMessageID, text: answerText },
            {
              id: 'monotonic-tool',
              type: 'tool',
              tool: 'read',
              callID: 'monotonic-tool-call',
              messageID: assistantMessageID,
              state: { status: 'running', input: { filePath: '/workspace/index.md' } },
            },
          ],
        },
      })
      store.setSessionSending(sessionID, true)
    },
    {
      sessionID: session.id,
      userMessageID: userID,
      assistantMessageID: assistantID,
      answerText: answer,
      createdAt: now,
    },
  )

  const reply = page.locator('.agent-message-assistant', { hasText: answer })
  await expect(reply).toBeVisible()
  await page.evaluate(async () => {
    const storeModulePath = '/src/stores/agent.ts'
    const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
    await useAgentStore().refreshRuntime()
  })
  await expect(reply).toBeVisible()
  await page.waitForTimeout(200)
  await expect
    .poll(async () =>
      page.evaluate(async () => {
        const storeModulePath = '/src/stores/agent.ts'
        const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
        return useAgentStore().sending
      }),
    )
    .toBe(true)
  await expect(page.locator('.agent-message-assistant .x-thought-node-loading')).toBeVisible()

  await page.evaluate(
    async ({ sessionID, userMessageID, assistantMessageID, finalMessageID, finalText, completedAt }) => {
      const storeModulePath = '/src/stores/agent.ts'
      const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
      const store = useAgentStore()
      const conversation = store.conversations[sessionID]
      store.setConversationCollection(sessionID, {
        messages: [
          ...conversation.messages,
          {
            id: finalMessageID,
            role: 'assistant',
            parentID: userMessageID,
            sessionID,
            time: { created: completedAt, completed: completedAt + 10 },
          },
        ],
        parts: {
          ...conversation.parts,
          [assistantMessageID]: [
            ...(conversation.parts[assistantMessageID] || []).map((part: any) =>
              part.id === 'monotonic-tool' ? { ...part, state: { ...part.state, status: 'completed' } } : part,
            ),
            {
              id: 'monotonic-tool-finish',
              type: 'step-finish',
              messageID: assistantMessageID,
              reason: 'tool-calls',
            },
          ],
          [finalMessageID]: [
            { id: 'monotonic-final-text', type: 'text', messageID: finalMessageID, text: finalText },
            { id: 'monotonic-final-finish', type: 'step-finish', messageID: finalMessageID, reason: 'stop' },
          ],
        },
      })
    },
    {
      sessionID: session.id,
      userMessageID: userID,
      assistantMessageID: assistantID,
      finalMessageID: finalAssistantID,
      finalText: finalAnswer,
      completedAt: now + 100,
    },
  )

  await page.evaluate(async (sessionID) => {
    const storeModulePath = '/src/stores/agent.ts'
    const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
    useAgentStore().handleEvent({ type: 'session.idle', properties: { sessionID } } as never)
  }, session.id)
  await expect
    .poll(async () =>
      page.evaluate(async () => {
        const storeModulePath = '/src/stores/agent.ts'
        const { useAgentStore } = await import(/* @vite-ignore */ storeModulePath)
        return useAgentStore().sending
      }),
    )
    .toBe(false)
  await expect(reply).toBeVisible()
  await expect(page.getByText(finalAnswer, { exact: true })).toBeVisible()
})

test('Agent 思考过程默认收起并可手动展开', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright collapsed reasoning')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const messageID = 'assistant-collapsed-reasoning'
  const reasoningText = '默认收起的思考内容'
  const introText = '先确认需要保留的内容。'
  const answerText = '最终答案\n\n**保留 Markdown**'
  const copiedTurn = `${introText}\n\n问题：是否保留 Markdown？\n回答：保留\n\n${answerText}`

  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          ;(window as typeof window & { __copiedAgentReply?: string }).__copiedAgentReply = text
        },
      },
    })
  })

  await page.route(new RegExp(`/api/agent/session/${session.id}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: {
            id: messageID,
            role: 'assistant',
            sessionID: session.id,
            time: { created: Date.now(), completed: Date.now() },
          },
          parts: [
            { id: 'reasoning-part', type: 'reasoning', messageID, sessionID: session.id, text: reasoningText },
            { id: 'intro-part', type: 'text', messageID, sessionID: session.id, text: introText },
            {
              id: 'question-part',
              callID: 'question-call',
              type: 'tool',
              tool: 'question',
              messageID,
              sessionID: session.id,
              state: {
                status: 'completed',
                input: { questions: [{ question: '是否保留 Markdown？' }] },
                metadata: { answers: [['保留']] },
              },
            },
            { id: 'answer-part', type: 'text', messageID, sessionID: session.id, text: answerText },
          ],
        },
      ],
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  const think = page.locator('.agent-message-assistant .x-think')
  const trigger = think.locator('.x-think-status')
  await expect(think).toContainText('思考过程')
  await expect(trigger).toHaveAttribute('aria-expanded', 'false')
  await expect(think.locator('.x-think-content')).toHaveCount(0)
  await trigger.click()
  await expect(trigger).toHaveAttribute('aria-expanded', 'true')
  await expect(think.locator('.x-think-content')).toContainText(reasoningText)

  const copy = page.getByRole('button', { name: '复制', exact: true })
  await copy.click()
  await expect(page.getByRole('button', { name: '已复制', exact: true })).toBeVisible()
  expect(
    await page.evaluate(() => (window as typeof window & { __copiedAgentReply?: string }).__copiedAgentReply),
  ).toBe(copiedTurn)
})

test('Agent 按消息归属保留文本与动作的实际顺序', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent timeline projection')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const now = Date.now()
  const info = (id: string, role: 'user' | 'assistant', parentID: string | undefined, offset: number) => ({
    id,
    role,
    parentID,
    sessionID: session.id,
    time: { created: now + offset, completed: role === 'assistant' ? now + offset + 80 : undefined },
  })
  const part = (messageID: string, id: string, value: Record<string, unknown>) => ({
    id,
    messageID,
    sessionID: session.id,
    ...value,
  })

  await page.route(new RegExp(`/api/agent/session/${session.id}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: info('timeline-user-1', 'user', undefined, 0),
          parts: [part('timeline-user-1', 'timeline-user-1-text', { type: 'text', text: '请完成第一项' })],
        },
        {
          info: info('timeline-assistant-1', 'assistant', 'timeline-user-1', 100),
          parts: [
            part('timeline-assistant-1', 'timeline-reasoning', {
              type: 'reasoning',
              text: '## 分析需求\n先确认处理路径',
            }),
            part('timeline-assistant-1', 'timeline-progress', { type: 'text', text: '先说明处理方向' }),
          ],
        },
        {
          info: info('timeline-assistant-2', 'assistant', 'timeline-user-1', 200),
          parts: [
            part('timeline-assistant-2', 'timeline-tool', {
              type: 'tool',
              tool: 'read',
              callID: 'timeline-read',
              state: {
                status: 'completed',
                input: { filePath: '/workspace/index.md' },
                output: '# 内容',
              },
            }),
          ],
        },
        {
          info: info('timeline-assistant-3', 'assistant', 'timeline-user-1', 300),
          parts: [part('timeline-assistant-3', 'timeline-answer', { type: 'text', text: '第一项最终答复' })],
        },
        {
          info: info('timeline-user-2', 'user', undefined, 400),
          parts: [part('timeline-user-2', 'timeline-user-2-text', { type: 'text', text: '请完成第二项' })],
        },
        {
          info: info('timeline-assistant-4', 'assistant', 'timeline-user-2', 500),
          parts: [part('timeline-assistant-4', 'timeline-answer-2', { type: 'text', text: '第二项最终答复' })],
        },
      ],
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  await expect(page.locator('.agent-message-user')).toHaveCount(2)
  await expect(page.locator('.agent-message-assistant')).toHaveCount(2)
  const firstReply = page.locator('.agent-message-assistant').first()
  await expect(firstReply.locator('.x-think-status')).toContainText('思考过程 · 分析需求')
  await expect(firstReply.locator('.x-think-status')).toHaveAttribute('aria-expanded', 'false')
  const projectedOrder = await firstReply.evaluate((element) =>
    [...element.querySelectorAll('.agent-message-text, .x-thought-chain')].map((node) =>
      node.classList.contains('x-thought-chain') ? 'action' : 'text',
    ),
  )
  expect(projectedOrder).toEqual(['text', 'action', 'text'])
  const projectedText = firstReply.locator('.agent-message-text')
  await expect(projectedText.nth(0)).toContainText('先说明处理方向')
  await expect(projectedText.nth(0)).toHaveClass(/agent-message-text-intermediate/)
  const action = firstReply.locator('.x-thought-chain').first()
  await action.locator('.x-thought-node-trigger').click()
  await expect(action).toContainText('读取文件')
  await expect(projectedText.nth(1)).toContainText('第一项最终答复')
  await expect(projectedText.nth(1)).not.toHaveClass(/agent-message-text-intermediate/)
})

test('Agent 重试状态使用面向用户的提示', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent retry status')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }

  await page.route(new RegExp(`/api/agent/session/${session.id}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: { id: 'retry-user', role: 'user', sessionID: session.id, time: { created: Date.now() } },
          parts: [
            {
              id: 'retry-user-text',
              type: 'text',
              messageID: 'retry-user',
              sessionID: session.id,
              text: '需要重试的请求',
            },
          ],
        },
      ],
    }),
  )
  await page.route(/\/api\/agent\/session\/status(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: {
        [session.id]: {
          type: 'retry',
          attempt: 2,
          message: '429 rate limit exceeded',
          next: Date.now() + 60_000,
          action: {
            reason: 'rate_limit',
            provider: 'fake',
            title: '服务提示',
            message: '可以查看服务状态后继续',
            label: '查看服务状态',
            link: 'https://example.com/agent-status',
          },
        },
      },
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  const retry = page.locator('.agent-message-assistant .x-retry')
  await expect(retry).toBeVisible()
  await expect(retry).toContainText('正在重试')
  await expect(retry).toContainText('请求较多，请稍后重试')
  await expect(retry).toContainText('第 2 次')
  await expect(retry).toContainText('可以查看服务状态后继续')
  await expect(retry.getByRole('button', { name: '查看服务状态' })).toBeVisible()
  await expect(retry).not.toContainText('429')
  await expect(retry).not.toContainText('fake')
})

test('Agent 流式回复不显示光标且链接在新窗口打开', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright Agent link behavior')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const messageID = 'assistant-link-behavior'

  await page.route(new RegExp(`/api/agent/session/${session.id}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: {
            id: messageID,
            role: 'assistant',
            sessionID: session.id,
            time: { created: Date.now() },
          },
          parts: [
            {
              id: 'answer-link-part',
              type: 'text',
              messageID,
              sessionID: session.id,
              text: '[外部链接](https://example.com/agent-link)',
            },
          ],
        },
      ],
    }),
  )
  await page.context().route('https://example.com/**', (route) => route.fulfill({ body: 'opened' }))
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  const answer = page.locator('.agent-message-assistant .agent-message-text')
  const link = answer.getByRole('link', { name: '外部链接' })
  await expect(link).toHaveAttribute('target', '_blank')
  await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  expect(await answer.evaluate((element) => getComputedStyle(element, '::after').content)).toBe('none')

  const newPagePromise = page.context().waitForEvent('page')
  await link.click()
  const newPage = await newPagePromise
  await newPage.waitForLoadState()
  await expect(newPage).toHaveURL('https://example.com/agent-link')
  await expect(page).toHaveURL('http://127.0.0.1:15080/agent')
  await newPage.close()
})
