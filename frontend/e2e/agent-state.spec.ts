import { expect, test } from '@playwright/test'
import { buildAgentTimeline } from '../src/components/agentTimeline'
import { agentMessageRenderKey, markOptimisticMessage, mergeMessageCollections } from '../src/stores/agentMessageState'
import {
  agentRootSessionID,
  agentSessionTreeIDs,
  agentSessionTreeRequest,
  agentSessionTreeStatus,
} from '../src/stores/agentSessionTree'

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

test('子任务从普通动作链中独立呈现并关联子会话', () => {
  const rootID = 'subtask-root'
  const childID = 'subtask-child'
  const userID = 'subtask-user'
  const assistantID = 'subtask-assistant'
  const timeline = buildAgentTimeline(
    [
      { id: userID, role: 'user', sessionID: rootID, time: { created: 1 } },
      { id: assistantID, role: 'assistant', parentID: userID, sessionID: rootID, time: { created: 2 } },
    ],
    {
      [userID]: [{ id: 'subtask-user-text', type: 'text', messageID: userID, text: '检查资料' }],
      [assistantID]: [
        {
          id: 'subtask-tool',
          callID: 'subtask-call',
          type: 'tool',
          tool: 'task',
          messageID: assistantID,
          sessionID: rootID,
          state: {
            status: 'running',
            input: { description: '核对历史资料', subagent_type: 'explore', background: true },
            metadata: { sessionId: childID, background: true },
          },
        },
      ],
    },
    {
      running: true,
      sessions: [
        { id: rootID, title: '主对话', time: { created: 1, updated: 1 } },
        {
          id: childID,
          parentID: rootID,
          title: '核对历史资料 (@explore subagent)',
          time: { created: 2, updated: 2 },
        },
      ],
      sessionStatuses: { [childID]: { type: 'busy' } },
    },
  )

  const assistant = timeline.find((item) => item.role === 'assistant')
  expect(assistant?.role).toBe('assistant')
  if (!assistant || assistant.role !== 'assistant') return
  expect(assistant.segments).toContainEqual(
    expect.objectContaining({
      type: 'subtask',
      sessionID: childID,
      title: '探索智能体',
      description: '核对历史资料',
      status: 'running',
      background: true,
    }),
  )
  expect(assistant.segments.some((segment) => segment.type === 'action')).toBe(false)
  expect(assistant.segments.some((segment) => segment.type === 'thinking')).toBe(false)
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
