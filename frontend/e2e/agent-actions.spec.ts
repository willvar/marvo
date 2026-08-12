import { expect, test } from '@playwright/test'
import { approveDevice, workspaceAPI, workspaceAPIRegex, workspacePath } from './helpers'

test('Agent 会话隐藏内部压缩消息并正确呈现恢复与停止结果', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright normalized Agent messages')
  const created = await page.request.post(workspaceAPI('/api/agent/session'))
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const now = Date.now()
  const part = (messageID: string, id: string, value: Record<string, unknown>) => ({
    id,
    messageID,
    sessionID: session.id,
    ...value,
  })

  await page.route(new RegExp(workspaceAPIRegex(`/api/agent/session/${session.id}/message(?:\\?.*)?$`)), (route) =>
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
  await page.goto(workspacePath('/agent'))

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
  const created = await page.request.post(workspaceAPI('/api/agent/session'))
  expect(created.ok()).toBeTruthy()
  const session = (await created.json()) as { id: string }
  const readMessageID = 'assistant-tools-read'
  const searchMessageID = 'assistant-tools-search'
  const answerMessageID = 'assistant-tools-answer'
  const startedAt = Date.now()

  await page.route(new RegExp(workspaceAPIRegex(`/api/agent/session/${session.id}/message(?:\\?.*)?$`)), (route) =>
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
  await page.goto(workspacePath('/agent'))

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
