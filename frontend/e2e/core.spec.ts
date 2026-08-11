import { expect, request, test, type Page } from '@playwright/test'
import { buildAgentTimeline } from '../src/components/agentTimeline'
import { toMarkdownAssetPath, toNoteAssetUrl } from '../src/sdk/utils/noteAssets'
import { agentMessageRenderKey, markOptimisticMessage, mergeMessageCollections } from '../src/stores/agentMessageState'
import {
  agentRootSessionID,
  agentSessionTreeIDs,
  agentSessionTreeRequest,
  agentSessionTreeStatus,
} from '../src/stores/agentSessionTree'

const backendURL = 'http://127.0.0.1:15090'
const approvedTestDeviceID = 'marvo-playwright-approved-device'

test('中文媒体路径不会被重复编码', () => {
  const expected = '/api/notes/%E6%96%B0%E4%B8%AD%E5%9B%BD/assets/%E6%97%B6%E9%97%B4%E7%BA%BF.svg'
  expect(toNoteAssetUrl('assets/时间线.svg', '新中国')).toBe(expected)
  expect(toNoteAssetUrl('assets/%E6%97%B6%E9%97%B4%E7%BA%BF.svg', '新中国')).toBe(expected)
  expect(toMarkdownAssetPath('assets/%E6%97%B6%E9%97%B4%E7%BA%BF.svg')).toBe('assets/时间线.svg')
})

test('智能体子会话的状态与待响应请求会归并到根会话', () => {
  const sessions = [
    { id: 'root', title: '根会话', time: { created: 1, updated: 1 } },
    { id: 'child', parentID: 'root', title: '子会话', time: { created: 2, updated: 2 } },
    { id: 'leaf', parentID: 'child', title: '末级会话', time: { created: 3, updated: 3 } },
    { id: 'other', title: '其他会话', time: { created: 4, updated: 4 } },
  ]
  const request = { id: 'question-child', sessionID: 'leaf' }

  expect(agentSessionTreeIDs(sessions, 'root')).toEqual(['root', 'child', 'leaf'])
  expect(agentRootSessionID(sessions, 'leaf')).toBe('root')
  expect(agentSessionTreeRequest(sessions, { leaf: [request] }, 'root')).toBe(request)
  expect(
    agentSessionTreeStatus(
      sessions,
      {
        root: { type: 'idle' },
        child: { type: 'busy' },
        leaf: { type: 'retry', attempt: 2, message: '稍后继续', next: Date.now() + 2_000 },
      },
      'root',
    ),
  ).toMatchObject({ type: 'retry', attempt: 2 })
})

test('智能体提问结果、重试动作和失败终态按语义进入时间线', () => {
  const sessionID = 'timeline-states'
  const userID = 'timeline-user'
  const assistantID = 'timeline-assistant'
  const timeline = buildAgentTimeline(
    [
      { id: userID, role: 'user', sessionID, time: { created: 1 } },
      {
        id: assistantID,
        role: 'assistant',
        parentID: userID,
        sessionID,
        error: { name: 'StructuredOutputError', data: { message: 'invalid response' } },
        time: { created: 2, completed: 3 },
      },
    ],
    {
      [userID]: [{ id: 'timeline-user-text', type: 'text', messageID: userID, text: '执行任务' }],
      [assistantID]: [
        {
          id: 'timeline-tool',
          callID: 'timeline-tool-call',
          type: 'tool',
          tool: 'read',
          messageID: assistantID,
          state: { status: 'completed', input: { filePath: '/workspace/index.md' } },
        },
        {
          id: 'timeline-question',
          callID: 'timeline-question-call',
          type: 'tool',
          tool: 'question',
          messageID: assistantID,
          state: {
            status: 'completed',
            input: { questions: [{ question: '采用哪个方案？' }] },
            metadata: { answers: [['方案 A']] },
          },
        },
      ],
    },
    { running: false },
  )
  const assistant = timeline.find((item) => item.role === 'assistant')
  expect(assistant?.role).toBe('assistant')
  if (!assistant || assistant.role !== 'assistant') return
  expect(assistant.segments.find((segment) => segment.type === 'action')).toMatchObject({
    type: 'action',
    items: [{ title: '执行失败', status: 'error' }],
  })
  expect(assistant.segments.find((segment) => segment.type === 'question')).toMatchObject({
    type: 'question',
    status: 'answered',
    items: [{ question: '采用哪个方案？', answers: ['方案 A'] }],
  })
  expect(assistant.segments.find((segment) => segment.type === 'error')).toMatchObject({
    type: 'error',
    text: '智能体返回的结果不完整，请重试',
  })
  expect(assistant.copyText).toBe('问题：采用哪个方案？\n回答：方案 A')

  const retryTimeline = buildAgentTimeline(
    [{ id: userID, role: 'user', sessionID, time: { created: 1 } }],
    { [userID]: [{ id: 'retry-user-text', type: 'text', messageID: userID, text: '继续' }] },
    {
      running: true,
      status: {
        type: 'retry',
        attempt: 3,
        message: 'Too many requests',
        next: Date.now() + 2_000,
        action: {
          reason: 'rate_limit',
          provider: 'fake',
          title: '需要处理',
          message: '请检查服务状态',
          label: '打开服务页',
          link: 'https://example.com/status',
        },
      },
    },
  )
  const retryAssistant = retryTimeline.find((item) => item.role === 'assistant')
  expect(retryAssistant?.role).toBe('assistant')
  expect(
    retryAssistant && retryAssistant.role === 'assistant'
      ? retryAssistant.segments.find((segment) => segment.type === 'retry')
      : undefined,
  ).toMatchObject({ type: 'retry', attempt: 3, message: '请求较多，请稍后重试' })
})

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

test('Agent 落后快照不会回退实时回复或更换整轮渲染标识', () => {
  const sessionID = 'session-monotonic'
  const localID = 'local_user'
  const serverUserID = 'server-user'
  const assistantID = 'assistant-live'
  const extraAssistantID = 'assistant-live-extra'
  const created = Date.now()
  const optimisticUser = markOptimisticMessage({
    id: localID,
    role: 'user',
    sessionID,
    time: { created },
  })
  const live = {
    messages: [
      optimisticUser,
      { id: assistantID, role: 'assistant', sessionID, time: { created: created + 10 } },
      { id: extraAssistantID, role: 'assistant', sessionID, time: { created: created + 20 } },
    ],
    parts: {
      [localID]: [{ id: 'local_user_text', type: 'text', messageID: localID, text: '检查消息稳定性' }],
      [assistantID]: [
        { id: 'assistant_text', type: 'text', messageID: assistantID, text: '这是已经流式生成完成的回答' },
        {
          id: 'assistant_tool',
          type: 'tool',
          messageID: assistantID,
          tool: 'read',
          state: { status: 'completed', output: '读取完成' },
        },
      ],
      [extraAssistantID]: [
        { id: 'assistant_extra_text', type: 'text', messageID: extraAssistantID, text: '快照尚未包含的后续内容' },
      ],
    },
  }
  const staleSnapshot = {
    messages: [
      { id: serverUserID, role: 'user', sessionID, time: { created: created + 1 } },
      {
        id: assistantID,
        role: 'assistant',
        parentID: serverUserID,
        sessionID,
        time: { created: created + 10 },
      },
    ],
    parts: {
      [serverUserID]: [{ id: 'server_user_text', type: 'text', messageID: serverUserID, text: '检查消息稳定性' }],
      [assistantID]: [
        { id: 'assistant_text', type: 'text', messageID: assistantID, text: '这是已经流式生成' },
        {
          id: 'assistant_tool',
          type: 'tool',
          messageID: assistantID,
          tool: 'read',
          state: { status: 'running' },
        },
      ],
    },
  }

  const beforeKeys = buildAgentTimeline(live.messages, live.parts, { running: true }).map((item) => item.key)
  const merged = mergeMessageCollections(staleSnapshot, live)
  const afterKeys = buildAgentTimeline(merged.messages, merged.parts, { running: true }).map((item) => item.key)

  expect(merged.messages.map((message) => message.id)).toEqual([serverUserID, assistantID, extraAssistantID])
  expect(agentMessageRenderKey(merged.messages[0])).toBe(localID)
  expect(merged.parts[assistantID][0].text).toBe('这是已经流式生成完成的回答')
  expect(merged.parts[assistantID][1].state?.status).toBe('completed')
  expect(merged.parts[extraAssistantID][0].text).toBe('快照尚未包含的后续内容')
  expect(afterKeys).toEqual(beforeKeys)
})

test('Agent 空思考片段仍保留正在思考的回复块', () => {
  const userID = 'empty-reasoning-user'
  const assistantID = 'empty-reasoning-assistant'
  const timeline = buildAgentTimeline(
    [
      { id: userID, role: 'user', sessionID: 'empty-reasoning', time: { created: 1 } },
      { id: assistantID, role: 'assistant', parentID: userID, sessionID: 'empty-reasoning', time: { created: 2 } },
    ],
    {
      [userID]: [{ id: 'empty-reasoning-user-text', type: 'text', messageID: userID, text: '开始处理' }],
      [assistantID]: [{ id: 'empty-reasoning-part', type: 'reasoning', messageID: assistantID, text: '' }],
    },
    { running: true },
  )

  const assistant = timeline.find((item) => item.role === 'assistant')
  expect(assistant?.role).toBe('assistant')
  expect(assistant && assistant.role === 'assistant' ? assistant.segments.map((segment) => segment.type) : []).toEqual([
    'thinking',
  ])
})

test('Agent 按真实顺序保留多段思考且不把旧标题用于新等待态', () => {
  const sessionID = 'reasoning-question-order'
  const userID = 'reasoning-question-user'
  const assistantID = 'reasoning-question-assistant'
  const part = (id: string, value: Record<string, unknown>) => ({ id, messageID: assistantID, ...value })
  const timeline = buildAgentTimeline(
    [
      { id: userID, role: 'user', sessionID, time: { created: 1 } },
      { id: assistantID, role: 'assistant', parentID: userID, sessionID, time: { created: 2 } },
    ],
    {
      [userID]: [{ id: 'reasoning-question-user-text', type: 'text', messageID: userID, text: '出两道题' }],
      [assistantID]: [
        part('reasoning-first', { type: 'reasoning', text: '## Choosing the first riddle' }),
        part('question-first', {
          callID: 'question-first-call',
          type: 'tool',
          tool: 'question',
          state: {
            status: 'completed',
            input: { questions: [{ question: '第一题？' }] },
            metadata: { answers: [['第一答']] },
          },
        }),
        part('answer-first', { type: 'text', text: '第一题答对了。' }),
        part('reasoning-second', { type: 'reasoning', text: '## Selecting less common riddles with options' }),
        part('question-second', {
          callID: 'question-second-call',
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
    { running: true, status: { type: 'busy' } },
  )

  const assistant = timeline.find((item) => item.role === 'assistant')
  expect(assistant?.role).toBe('assistant')
  if (!assistant || assistant.role !== 'assistant') return
  expect(assistant.segments.map((segment) => segment.type)).toEqual([
    'reasoning',
    'question',
    'text',
    'reasoning',
    'question',
    'thinking',
  ])
  expect(assistant.segments[0]).toMatchObject({ type: 'reasoning', heading: 'Choosing the first riddle' })
  expect(assistant.segments[3]).toMatchObject({
    type: 'reasoning',
    heading: 'Selecting less common riddles with options',
  })
  expect(assistant.segments[5]).toMatchObject({ type: 'thinking', heading: '' })
})

test('Agent 中断标记作为独立时间线分隔符保留精确位置', () => {
  const sessionID = 'session-interrupted-divider'
  const userID = 'interrupted-user'
  const beforeID = 'assistant-before-interruption'
  const interruptedID = 'assistant-interrupted'
  const afterID = 'assistant-after-interruption'
  const timeline = buildAgentTimeline(
    [
      { id: userID, role: 'user', sessionID, time: { created: 1 } },
      { id: beforeID, role: 'assistant', parentID: userID, sessionID, time: { created: 2, completed: 3 } },
      {
        id: interruptedID,
        role: 'assistant',
        parentID: userID,
        sessionID,
        error: { name: 'MessageAbortedError', data: { message: 'Aborted' } },
        time: { created: 4, completed: 5 },
      },
      { id: afterID, role: 'assistant', parentID: userID, sessionID, time: { created: 6, completed: 7 } },
    ],
    {
      [userID]: [{ id: 'interrupted-user-text', type: 'text', messageID: userID, text: '请继续' }],
      [beforeID]: [{ id: 'before-text', type: 'text', messageID: beforeID, text: '中断前的输出' }],
      [interruptedID]: [],
      [afterID]: [{ id: 'after-text', type: 'text', messageID: afterID, text: '中断后的输出' }],
    },
    { running: false },
  )

  expect(timeline.map((item) => item.role)).toEqual(['user', 'assistant', 'divider', 'assistant'])
  expect(timeline[2]).toMatchObject({ role: 'divider', label: '已中断' })
  expect(timeline[1]?.role === 'assistant' ? timeline[1].segments[0] : undefined).toMatchObject({
    type: 'text',
    text: '中断前的输出',
  })
  expect(timeline[3]?.role === 'assistant' ? timeline[3].segments[0] : undefined).toMatchObject({
    type: 'text',
    text: '中断后的输出',
  })
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

test('编辑状态下点击超链接不会打开页面', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright editor link behavior')
  const title = 'E2E editor link behavior'
  const created = await page.request.post('/api/notes', {
    data: { title, content: '![](assets/时间线.svg)\n\n[编辑态链接](https://example.com)', tags: [] },
  })
  expect(created.ok()).toBeTruthy()

  await page.goto(`/note/${encodeURIComponent(title)}`)
  await expect(page.locator('.note-preview')).toBeVisible()
  await page.locator('.editor-toolbar-left > .toolbar-btn').nth(1).click()
  const expectedAssetURL = `/api/notes/${encodeURIComponent(title)}/assets/${encodeURIComponent('时间线.svg')}`
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
  const saved = await (await page.request.get(`/api/notes/${encodeURIComponent(title)}`)).json()
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
  const created = await page.request.post('/api/notes', { data: { title, content: '标签布局检查', tags } })
  expect(created.ok()).toBeTruthy()

  await page.goto(`/note/${encodeURIComponent(title)}`)
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

test('笔记列表品牌栏与内容标题栏保持对齐', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright shell header alignment')

  const noteTitle = 'E2E shell alignment'
  const note = await page.request.post('/api/notes', {
    data: { title: noteTitle, content: 'alignment', tags: [] },
  })
  expect(note.ok()).toBeTruthy()
  const session = await page.request.post('/api/agent/session')
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

  await page.goto(`/note/${encodeURIComponent(noteTitle)}`)
  const [searchRowBounds, editorToolbarBounds] = await Promise.all([
    page.locator('.dsh-search').boundingBox(),
    page.locator('.editor-toolbar').boundingBox(),
  ])
  expect(searchRowBounds!.y).toBe(editorToolbarBounds!.y)
  expect(searchRowBounds!.height).toBe(editorToolbarBounds!.height)

  const logo = page.locator('.dsh-logo')
  await expect(logo).toHaveAttribute('href', '/')
  await logo.click()
  await expect(page).toHaveURL('http://127.0.0.1:15080/')

  await page.goto('/agent')
  const search = page.locator('.dsh-search-inp')
  const sessionsPane = page.locator('.agent-chat-sessions')
  const creation = page.locator('.x-conversations-creation')
  const noteItem = page.locator('.dsh-nav-item').first()
  const conversationItem = page.locator('.x-conversations-item').first()
  await expect(creation).toBeVisible()
  await expect(noteItem).toBeVisible()
  await expect(conversationItem).toBeVisible()
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
    page.locator('.dsh-footer-button').boundingBox(),
    page.locator('.agent-chat-input .x-sender').boundingBox(),
  ])
  expect(footerButtonBounds!.y + footerButtonBounds!.height).toBe(senderBounds!.y + senderBounds!.height)

  await page.evaluate(() => localStorage.setItem('marvo.ui.agentAssistantDisplayMode', 'sidebar'))
  await page.reload()
  await page.goto('/')
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
  const alignedFooterButtonBounds = await page.locator('.dsh-footer-button').boundingBox()
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
  await page.route('**/api/theme', async (route) => {
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
  const created = await page.request.post('/api/notes', { data: { title, content: '全站字号比例测试', tags: [] } })
  expect(created.ok()).toBeTruthy()
  await page.goto(`/note/${encodeURIComponent(title)}`)
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
    const created = await page.request.post('/api/notes', { data: { title, content: '', tags: [] } })
    expect(created.ok()).toBeTruthy()
    const note = (await created.json()) as { instance_token: string }
    const trashed = await page.request.delete(`/api/notes/${encodeURIComponent(title)}`, {
      data: { instance_token: note.instance_token },
    })
    expect(trashed.ok()).toBeTruthy()
  }

  let nativeDialogs = 0
  page.on('dialog', async (dialog) => {
    nativeDialogs++
    await dialog.dismiss()
  })
  await page.goto('/trash')

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
  const suffix = Date.now()
  const deviceName = `Playwright 撤回确认 ${suffix}`
  const localDeviceID = `marvo-playwright-revoke-${suffix}`
  const application = await page.request.post('/api/auth/apply', {
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
  await page.goto('/admin/login')
  await page.getByPlaceholder('请输入密码').fill('e2e-admin-password')
  await page.getByRole('button', { name: '进入' }).click()
  await expect(page).toHaveURL(/\/admin$/)

  const pendingRow = page.locator('tbody tr').filter({ hasText: deviceName })
  await expect(pendingRow).toBeVisible()
  await pendingRow.getByRole('button', { name: '批准', exact: true }).click()
  await expect(pendingRow).toHaveCount(0)

  await page.getByRole('button', { name: /已批准设备/ }).click()
  const approvedRow = page.locator('tbody tr').filter({ hasText: deviceName })
  await expect(approvedRow).toBeVisible()
  await expect(approvedRow.locator('td').nth(1)).toHaveText(/^\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2}$/)

  await approvedRow.getByRole('button', { name: '撤回', exact: true }).click()
  await expect(page.getByRole('heading', { name: '撤回设备批准' })).toBeVisible()
  await expect(page.getByText(`确定撤回「${deviceName}」的访问权限吗？`, { exact: false })).toBeVisible()
  const revokeDialog = page.locator('.dialog-panel').filter({ hasText: '撤回设备批准' })
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await expect(revokeDialog).not.toContainText('未命名设备')
  await expect(page.getByRole('heading', { name: '撤回设备批准' })).toBeHidden()
  await expect(approvedRow).toBeVisible()

  await approvedRow.getByRole('button', { name: '撤回', exact: true }).click()
  await page.getByRole('button', { name: '确认撤回', exact: true }).click()
  await expect(approvedRow).toHaveCount(0)
  expect(nativeDialogs).toBe(0)
})

test('路由资源版本失效时自动恢复到当前前端版本', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright stale route recovery')
  await page.goto('/')

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

async function approveDevice(page: Page, deviceName: string) {
  await page.addInitScript((localDeviceID) => {
    localStorage.setItem('marvo_local_device_id', localDeviceID)
  }, approvedTestDeviceID)

  const tokenResponsePromise = page.waitForResponse(
    (response) => response.url().includes('/api/auth/token') && response.request().method() === 'GET',
  )
  await page.goto('/login')
  const tokenResponse = await tokenResponsePromise
  const token = (await tokenResponse.json()) as { status?: string }
  if (token.status === 'approved') {
    await expect(page).toHaveURL('http://127.0.0.1:15080/')
    await stopActiveAgentRuns(page)
    return
  }

  await page.getByPlaceholder('设备名称').fill(deviceName)
  await page.getByRole('button', { name: '申请访问' }).click()
  await expect(page.getByRole('heading', { name: '等待审批' })).toBeVisible()

  const admin = await request.newContext({ baseURL: backendURL })
  try {
    const verify = await admin.post('/api/auth/verify', { data: { password: 'e2e-admin-password' } })
    expect(verify.ok()).toBeTruthy()
    const { challenge_token: challengeToken } = await verify.json()
    const login = await admin.post('/api/auth', { data: { challenge_token: challengeToken } })
    expect(login.ok()).toBeTruthy()
    const pending = await admin.get('/api/admin/requests')
    const requests = (await pending.json()).requests as Array<{ id: string; device_name: string }>
    const target = requests.find((item) => item.device_name === deviceName)
    expect(target).toBeTruthy()
    const approval = await admin.post(`/api/admin/requests/${target!.id}/approve`)
    expect(approval.ok()).toBeTruthy()
  } finally {
    await admin.dispose()
  }

  await expect(page).toHaveURL('http://127.0.0.1:15080/', { timeout: 12_000 })
  await stopActiveAgentRuns(page)
}

async function stopActiveAgentRuns(page: Page) {
  const statusResponse = await page.request.get('/api/agent/session/status')
  expect(statusResponse.ok()).toBeTruthy()
  const statuses = (await statusResponse.json()) as Record<string, { type?: string }>
  for (const [sessionID, status] of Object.entries(statuses)) {
    if (status.type !== 'busy' && status.type !== 'retry') continue
    const abortResponse = await page.request.post(`/api/agent/session/${encodeURIComponent(sessionID)}/abort`)
    expect(abortResponse.ok()).toBeTruthy()
  }
}

async function openSidebar(page: Page) {
  const toggle = page.getByTitle('展开列表')
  if (await toggle.isVisible()) await toggle.click()
}

async function closeCompactSidebar(page: Page) {
  const overlay = page.locator('.dsh-overlay')
  if (await overlay.isVisible()) await page.getByTitle('收起列表').click()
}

async function createLongAgentSession(page: Page, label: string) {
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  for (let index = 0; index < 18; index++) {
    const prompt = await page.request.post(`/api/agent/session/${session.id}/prompt_async`, {
      data: {
        parts: [
          {
            type: 'text',
            text: `${label} ${index + 1} ${'滚动历史内容 '.repeat(12)}`,
          },
        ],
      },
    })
    expect(prompt.ok()).toBeTruthy()
  }
  const aborted = await page.request.post(`/api/agent/session/${session.id}/abort`)
  expect(aborted.ok()).toBeTruthy()
  return session.id
}

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

test('Agent 个性化设置切换并记忆浮动按钮与内容右侧栏', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright Agent display mode')
  await page.goto('/agent')
  await page.getByRole('button', { name: '设置', exact: true }).click()

  await expect(page.getByRole('tab', { name: '个性化' })).toHaveAttribute('aria-selected', 'true')
  await page
    .locator('.agent-display-mode-item')
    .filter({ has: page.locator('input[value="sidebar"]') })
    .click()
  await expect(page.getByRole('radio', { name: /^内容右侧栏/ })).toBeChecked()
  await page.getByRole('button', { name: '保存设置' }).click()
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
  await page
    .locator('.agent-display-mode-item')
    .filter({ has: page.locator('input[value="floating"]') })
    .click()
  await expect(page.getByRole('radio', { name: /^浮动按钮/ })).toBeChecked()
  await page.getByRole('button', { name: '保存设置' }).click()
  await page.goto('/')
  await expect(page.locator('.agent-fab')).toBeVisible()
  await expect(page.locator('.agent-side-panel')).toHaveCount(0)
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

test('Agent 会话隐藏内部压缩消息并正确呈现恢复与停止结果', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright normalized Agent messages')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const now = Date.now()
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
          info: { id: 'user-visible-1', role: 'user', sessionID: session.id, time: { created: now } },
          parts: [part('user-visible-1', 'user-visible-1-text', { type: 'text', text: '第一问' })],
        },
        {
          info: {
            id: 'assistant-reasoning',
            role: 'assistant',
            sessionID: session.id,
            agent: 'build',
            time: { created: now + 100, completed: now + 500 },
          },
          parts: [
            part('assistant-reasoning', 'assistant-reasoning-part', {
              type: 'reasoning',
              text: '只在思考组件中出现',
            }),
            part('assistant-reasoning', 'assistant-reasoning-answer', { type: 'text', text: '第一条回答' }),
          ],
        },
        {
          info: { id: 'user-compaction', role: 'user', sessionID: session.id, time: { created: now + 600 } },
          parts: [part('user-compaction', 'compaction-part', { type: 'compaction', auto: true })],
        },
        {
          info: {
            id: 'assistant-compaction',
            role: 'assistant',
            sessionID: session.id,
            mode: 'compaction',
            agent: 'compaction',
            summary: true,
            time: { created: now + 700, completed: now + 800 },
          },
          parts: [
            part('assistant-compaction', 'compaction-summary', {
              type: 'text',
              text: 'INTERNAL COMPACTION SUMMARY',
            }),
          ],
        },
        {
          info: { id: 'user-synthetic', role: 'user', sessionID: session.id, time: { created: now + 900 } },
          parts: [
            part('user-synthetic', 'synthetic-text', {
              type: 'text',
              synthetic: true,
              text: 'Continue if you have next steps',
            }),
          ],
        },
        {
          info: {
            id: 'assistant-empty',
            role: 'assistant',
            sessionID: session.id,
            agent: 'build',
            time: { created: now + 1_000, completed: now + 1_050 },
          },
          parts: [],
        },
        {
          info: { id: 'user-visible-2', role: 'user', sessionID: session.id, time: { created: now + 1_100 } },
          parts: [part('user-visible-2', 'user-visible-2-text', { type: 'text', text: '第二问' })],
        },
        {
          info: {
            id: 'assistant-tool-error',
            role: 'assistant',
            sessionID: session.id,
            agent: 'build',
            time: { created: now + 1_200, completed: now + 1_500 },
          },
          parts: [
            part('assistant-tool-error', 'tool-step-start', { type: 'step-start' }),
            part('assistant-tool-error', 'tool-error', {
              type: 'tool',
              tool: 'read',
              callID: 'read-failed',
              state: { status: 'error', input: { filePath: '/workspace/missing.md' }, error: 'not found' },
            }),
            part('assistant-tool-error', 'tool-step-finish', { type: 'step-finish', reason: 'tool-calls' }),
          ],
        },
        {
          info: {
            id: 'assistant-recovered',
            role: 'assistant',
            sessionID: session.id,
            agent: 'build',
            time: { created: now + 1_600, completed: now + 1_900 },
          },
          parts: [part('assistant-recovered', 'assistant-recovered-text', { type: 'text', text: '已经恢复并回答' })],
        },
        {
          info: { id: 'user-visible-3', role: 'user', sessionID: session.id, time: { created: now + 2_000 } },
          parts: [part('user-visible-3', 'user-visible-3-text', { type: 'text', text: '第三问' })],
        },
        {
          info: {
            id: 'assistant-aborted',
            role: 'assistant',
            sessionID: session.id,
            agent: 'build',
            error: { name: 'MessageAbortedError', data: { message: 'Aborted' } },
            time: { created: now + 2_100, completed: now + 2_200 },
          },
          parts: [],
        },
      ],
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  await expect(page.locator('.agent-message-user')).toHaveCount(3)
  await expect(page.locator('.agent-message-assistant')).toHaveCount(2)
  await expect(page.getByText('INTERNAL COMPACTION SUMMARY', { exact: true })).toHaveCount(0)
  await expect(page.getByText('Continue if you have next steps', { exact: true })).toHaveCount(0)
  await expect(page.locator('.agent-message-assistant .x-think')).toHaveCount(1)
  await expect(page.locator('.agent-message-error')).toHaveCount(0)
  await expect(page.getByRole('separator', { name: '已中断' })).toBeVisible()
  await expect(page.locator('.agent-message-stopped')).toHaveCount(0)

  const executionRoot = page.locator('.agent-message-assistant .x-thought-chain > .x-thought-node').first()
  await expect(executionRoot.locator(':scope > .x-thought-node-icon')).toHaveClass(/x-thought-node-warning/)
  await expect(executionRoot).toContainText('1 项未成功')
  await expect(page.getByText('已经恢复并回答', { exact: true })).toBeVisible()
})

test('Agent 动作链按用户语义合并且仅总状态可展开', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright structured thought chain')
  const created = await page.request.post('/api/agent/session')
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const readMessageID = 'assistant-tools-read'
  const searchMessageID = 'assistant-tools-search'
  const answerMessageID = 'assistant-tools-answer'
  const startedAt = Date.now()

  await page.route(new RegExp(`/api/agent/session/${session.id}/message(?:\\?.*)?$`), (route) =>
    route.fulfill({
      json: [
        {
          info: {
            id: readMessageID,
            role: 'assistant',
            sessionID: session.id,
            providerID: 'openai',
            modelID: 'gpt-5.6-sol-fast',
            agent: 'build',
            variant: 'high',
            time: { created: startedAt, completed: startedAt + 2_000 },
          },
          parts: [
            {
              id: 'read-step-start',
              type: 'step-start',
              messageID: readMessageID,
              sessionID: session.id,
            },
            {
              id: 'tool-read',
              type: 'tool',
              callID: 'call-read',
              messageID: readMessageID,
              sessionID: session.id,
              tool: 'read',
              state: {
                status: 'completed',
                title: '/workspace/index.md',
                input: { filePath: '/workspace/index.md', limit: 200 },
                output: '# 测试笔记',
                metadata: { display: { type: 'file', path: '/workspace/index.md' }, truncated: false },
                time: { start: startedAt + 100, end: startedAt + 350 },
              },
            },
            {
              id: 'read-step-finish',
              type: 'step-finish',
              messageID: readMessageID,
              sessionID: session.id,
              reason: 'tool-calls',
              cost: 0.001,
              tokens: { total: 1_240, input: 1_000, output: 40, reasoning: 200, cache: { read: 0, write: 0 } },
            },
          ],
        },
        {
          info: {
            id: searchMessageID,
            role: 'assistant',
            sessionID: session.id,
            providerID: 'openai',
            modelID: 'gpt-5.6-sol-fast',
            agent: 'build',
            variant: 'high',
            time: { created: startedAt + 2_100, completed: startedAt + 4_800 },
          },
          parts: [
            {
              id: 'search-step-start',
              type: 'step-start',
              messageID: searchMessageID,
              sessionID: session.id,
            },
            {
              id: 'tool-search-success',
              type: 'tool',
              callID: 'call-search-success',
              messageID: searchMessageID,
              sessionID: session.id,
              tool: 'websearch',
              state: {
                status: 'completed',
                title: 'Exa Web Search: 测试查询',
                input: { query: '测试查询' },
                output: 'Title: 测试结果\nURL: https://example.com',
                metadata: { provider: 'exa', truncated: false },
                time: { start: startedAt + 2_200, end: startedAt + 2_700 },
              },
            },
            {
              id: 'tool-search-error',
              type: 'tool',
              callID: 'call-search-error',
              messageID: searchMessageID,
              sessionID: session.id,
              tool: 'websearch',
              state: {
                status: 'error',
                input: { query: '失败查询' },
                error: '网络不可用',
                metadata: { provider: 'exa' },
                time: { start: startedAt + 2_250, end: startedAt + 2_600 },
              },
            },
            {
              id: 'tool-custom',
              type: 'tool',
              callID: 'call-custom',
              messageID: searchMessageID,
              sessionID: session.id,
              tool: 'mcp_calendar_lookup',
              state: {
                status: 'completed',
                title: '查询日程',
                input: { description: '明天下午' },
                output: '没有冲突',
                metadata: {},
                time: { start: startedAt + 2_650, end: startedAt + 2_750 },
              },
            },
            {
              id: 'patch-part',
              type: 'patch',
              messageID: searchMessageID,
              sessionID: session.id,
              hash: 'patch-hash',
              files: ['/workspace/index.md', '/workspace/meta.json'],
            },
            {
              id: 'search-step-finish',
              type: 'step-finish',
              messageID: searchMessageID,
              sessionID: session.id,
              reason: 'tool-calls',
              cost: 0.002,
              tokens: { total: 2_600, input: 2_000, output: 100, reasoning: 500, cache: { read: 1_000, write: 0 } },
            },
          ],
        },
        {
          info: {
            id: answerMessageID,
            role: 'assistant',
            sessionID: session.id,
            providerID: 'openai',
            modelID: 'gpt-5.6-sol-fast',
            agent: 'build',
            variant: 'high',
            time: { created: startedAt + 4_900, completed: startedAt + 6_000 },
          },
          parts: [
            {
              id: 'answer-step-start',
              type: 'step-start',
              messageID: answerMessageID,
              sessionID: session.id,
            },
            {
              id: 'answer-part',
              type: 'text',
              messageID: answerMessageID,
              sessionID: session.id,
              text: '动作执行结束',
              time: { start: startedAt + 5_000, end: startedAt + 5_900 },
            },
            {
              id: 'answer-step-finish',
              type: 'step-finish',
              messageID: answerMessageID,
              sessionID: session.id,
              reason: 'stop',
              cost: 0.001,
              tokens: { total: 3_200, input: 3_000, output: 120, reasoning: 80, cache: { read: 2_000, write: 0 } },
            },
          ],
        },
      ],
    }),
  )
  await page.evaluate((id) => localStorage.setItem('marvo.agent.currentSessionId', id), session.id)
  await page.goto('/agent')

  const chain = page.locator('.agent-message-assistant > .x-bubble-body .x-thought-chain').first()
  const rootTrigger = chain.locator(':scope > .x-thought-node > .x-thought-node-main > .x-thought-node-trigger')
  await expect(rootTrigger).toContainText('已完成')
  await expect(rootTrigger).toContainText('4 项处理 · 1 项未成功 · 6.0 秒')
  await expect(rootTrigger).toHaveAttribute('aria-expanded', 'false')
  await expect(chain.locator('.x-thought-chain-nested')).toHaveCount(0)

  await rootTrigger.click()
  await expect(rootTrigger).toHaveAttribute('aria-expanded', 'true')
  const activities = chain.locator(
    ':scope > .x-thought-node > .x-thought-node-main > .x-thought-node-content > .x-thought-chain-nested > .x-thought-node',
  )
  await expect(activities).toHaveCount(4)
  await expect(activities.nth(0)).toContainText('读取文件')
  await expect(activities.nth(0)).toContainText('index.md')
  await expect(activities.nth(1)).toContainText('检索 2 项资料')
  await expect(activities.nth(1)).toContainText('测试查询、失败查询 · 1 项未成功：网络不可用')
  await expect(activities.nth(1).locator(':scope > .x-thought-node-icon')).toHaveClass(/x-thought-node-warning/)
  await expect(activities.nth(2)).toContainText('查询日程')
  await expect(activities.nth(2)).toContainText('明天下午')
  await expect(activities.nth(3)).toContainText('更新 2 个文件')
  await expect(activities.nth(3)).toContainText('index.md、meta.json')
  await expect(activities.locator(':scope > .x-thought-node-main > .x-thought-node-trigger')).toHaveCount(0)
  await expect(chain).not.toContainText('第 1 轮')
  await expect(chain).not.toContainText('生成答复')
  await expect(chain).not.toContainText('250 ms')
  await expect(chain).not.toContainText('openai/gpt-5.6-sol-fast')
  await expect(chain).not.toContainText('继续调用工具')
  await expect(chain).not.toContainText('总计 1,240')

  await rootTrigger.click()
  await expect(rootTrigger).toHaveAttribute('aria-expanded', 'false')
  await expect(activities).toHaveCount(0)
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

test('核心笔记流程在响应式布局中安全工作', async ({ page }, testInfo) => {
  const suffix = testInfo.project.name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()
  let title = `E2E ${suffix} core`
  let encodedTitle = encodeURIComponent(title)
  await approveDevice(page, `Playwright ${suffix}`)

  await expect(page.locator('.dsh-header-title-input')).toHaveCount(0)
  await expect(page.locator('.home-welcome')).toBeVisible()
  await expect(page.getByRole('button', { name: '新建笔记', exact: true })).toHaveCount(0)
  await expect(page.locator('.home-welcome')).toContainText('左侧列表上方的“搜索或新建”输入框')
  await expect(page.locator('.home-agent-composer .x-sender-input')).toBeVisible()
  await expect(page.locator('.home-agent-composer .x-sender-input')).toHaveAttribute(
    'placeholder',
    '向智能体描述你想完成的内容...',
  )

  await openSidebar(page)
  await expect(page.locator('.dsh-footer')).not.toContainText('智能体')
  const footerButtons = page.locator('.dsh-footer-button')
  await expect(footerButtons).toHaveCount(1)
  await expect(footerButtons.nth(0)).toHaveText(/回收站/)
  await expect(page.locator('.dsh-footer')).not.toContainText('智能体设置')
  await closeCompactSidebar(page)
  await page.locator('.dsh-header-agent').click()
  await expect(page).toHaveURL('http://127.0.0.1:15080/agent')
  const staticHeaderTitle = page.locator('.dsh-header-title')
  await expect(staticHeaderTitle).toHaveText('智能体对话')
  await expect(staticHeaderTitle).not.toHaveClass(/is-clickable/)
  await expect(staticHeaderTitle).not.toHaveAttribute('title')
  expect(await staticHeaderTitle.evaluate((element) => element.tagName)).toBe('SPAN')
  expect(await staticHeaderTitle.evaluate((element) => getComputedStyle(element).cursor)).not.toBe('pointer')
  await page.getByRole('button', { name: '设置', exact: true }).click()
  await expect(page.getByRole('heading', { name: '智能体设置' })).toBeVisible()
  await page.getByRole('tab', { name: '高阶设置' }).click()
  await expect(page.locator('.agent-model-selected')).toContainText('E2E Vision')
  await expect(page.locator('.agent-model-selected')).toContainText('支持图片')

  const modelInput = page.getByRole('combobox', { name: '选择智能体模型' })
  await modelInput.click()
  await modelInput.fill('E2E Text')
  await page.locator('.agent-model-item').filter({ hasText: 'E2E Text' }).click()
  await expect(page.locator('.agent-model-selected')).toContainText('不支持图片')
  await modelInput.click()
  await modelInput.fill('E2E Vision')
  await page.locator('.agent-model-item').filter({ hasText: 'E2E Vision' }).click()
  await expect(page.locator('.agent-model-selected')).toContainText('支持图片')

  const highReasoning = page.locator('.agent-variant-item').filter({ hasText: '高' })
  await highReasoning.click()
  await expect(highReasoning).toHaveAttribute('data-state', 'checked')

  const globalPrompt = `E2E global prompt ${suffix}`
  await page.getByLabel('全局提示词').fill(globalPrompt)
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByRole('heading', { name: '智能体设置' })).toBeHidden()
  await expect(page.locator('.dsh-header-agent')).toBeVisible()
  await openSidebar(page)
  const search = page.getByPlaceholder('搜索或新建...')
  await search.fill('非法/标题')
  await search.press('Enter')
  await expect(page.getByRole('alert')).toContainText('标题不能包含')
  await expect(page).toHaveURL('http://127.0.0.1:15080/agent')
  await search.fill(title)
  await search.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/note/${encodedTitle}$`))
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
  await expect(page.locator('.dsh-header-title')).toHaveText(title)
  expect((await page.request.get(`/api/notes/${encodeURIComponent(previousTitle)}`)).status()).toBe(404)
  expect((await page.request.get(`/api/notes/${encodedTitle}`)).ok()).toBeTruthy()

  await editor.fill('DRAFT SURVIVES REFRESH')
  await page.waitForTimeout(350)
  const beforeDraftSave = await (await page.request.get(`/api/notes/${encodeURIComponent(title)}`)).json()
  expect(beforeDraftSave.content).not.toContain('DRAFT SURVIVES REFRESH')
  await page.reload()
  await expect(page.locator('.note-preview')).toContainText('DRAFT SURVIVES REFRESH')
  await page.getByRole('button', { name: '编辑' }).click()
  await expect(editor).toContainText('DRAFT SURVIVES REFRESH')
  await editor.press('Control+s')
  await expect(page.locator('.dsh-header-save-status')).toHaveText('已保存', { timeout: 10_000 })

  await editor.fill('ORPHAN DRAFT RECOVERY')
  await page.waitForTimeout(350)
  const beforeReplacement = await (await page.request.get(`/api/notes/${encodeURIComponent(title)}`)).json()
  const trashed = await page.request.delete(`/api/notes/${encodeURIComponent(title)}`, {
    data: { instance_token: beforeReplacement.instance_token },
  })
  expect(trashed.ok()).toBeTruthy()
  const trashPayload = await trashed.json()
  const restored = await page.request.post(`/api/trash/${trashPayload.trash.id}/restore`, {
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

  const snapshotResponse = await page.request.get(`/api/notes/${encodeURIComponent(title)}`)
  const snapshot = await snapshotResponse.json()
  await editor.fill('LOCAL VERSION')
  const remoteWrite = await page.request.put(`/api/notes/${encodeURIComponent(title)}/content`, {
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

  await page.route(/\/api\/notes\/.*\/assets\/[^/]+\/content$/, async (route) => {
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
  await page.unroute(/\/api\/notes\/.*\/assets\/[^/]+\/content$/)
  await expect(page.locator('.dsh-header-save-status')).toHaveText('已保存', { timeout: 10_000 })

  await page.getByTitle('移到回收站').click()
  await page.getByRole('button', { name: '移到回收站', exact: true }).last().click()
  await expect(page).toHaveURL('http://127.0.0.1:15080/')

  await page.goto('/trash')
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
  const duringAgentRemote = await (await page.request.get(`/api/notes/${encodeURIComponent(title)}`)).json()
  expect(duringAgentRemote.content).toContain(editDuringAgent)

  await page.locator('.agent-fab').click()
  await expect(floatingPanel).toBeVisible()
  await floatingPanel.getByRole('button', { name: '停止', exact: true }).click()
  await expect(floatingPanel.getByRole('button', { name: '发送', exact: true })).toBeVisible()

  await page.goto('/agent')
  const sessionItems = page.locator('.x-conversations-item')
  await expect.poll(() => sessionItems.count()).toBeGreaterThan(0)
  const sessionCount = await sessionItems.count()
  await page.getByRole('button', { name: '新对话', exact: true }).click()
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
  await page.route('**/api/agent/session/*/message', async (route) => {
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
  await expect(activeSession).toHaveClass(/active/)
  releaseHistory()
  await historyFinished
  await page.unroute('**/api/agent/session/*/message')
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

  const promptedSessionID = await page.locator('.x-conversations-item.active').getAttribute('data-key')
  expect(promptedSessionID).toBeTruthy()
  const upstreamMessages = await (await page.request.get(`/api/agent/session/${promptedSessionID}/message`)).json()
  const injectedPrompt = [...upstreamMessages]
    .reverse()
    .find((message: any) => message.parts?.some((part: any) => part.type === 'text' && part.text === slashText))
  expect(injectedPrompt?.info?.model).toEqual({ providerID: 'fake', modelID: 'vision' })
  expect(injectedPrompt?.info?.system || '').not.toContain(globalPrompt)

  await page.reload()
  await expect(page.getByText(slashText, { exact: true })).toBeVisible()
  await expect(page.locator('.x-bubble-list .x-attachment-card')).toContainText(['agent-pixel.png', 'agent-notes.txt'])
  await expect(page.getByRole('button', { name: '停止', exact: true })).toBeVisible()
  await page.route(new RegExp(`/api/agent/session/${promptedSessionID}/abort(?:\\?.*)?$`), async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 600))
    await route.continue()
  })
  await page.getByRole('button', { name: '停止', exact: true }).click()
  await expect(agentInput).toHaveAttribute('placeholder', '正在停止…')
  await expect(page.getByRole('button', { name: '正在停止', exact: true })).toBeVisible()
  await expect(page.locator('.agent-message-assistant .x-think-loading')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeVisible()

  const fitsViewport = await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
  expect(fitsViewport).toBeTruthy()
})
