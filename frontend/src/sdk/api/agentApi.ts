import {
  createOpencodeClient,
  type AgentPartInput,
  type FilePartInput,
  type GlobalEvent,
  type OutputFormat,
  type SubtaskPartInput,
  type TextPartInput,
} from '@opencode-ai/sdk/v2/client'
import { notifyUnauthorized } from './unauthorized'
import { api } from './useApi'
import { scopedAPIPath } from '../workspace'

export type SSEEvent = Extract<GlobalEvent['payload'], { properties: unknown }>
export type AgentFilePartInput = FilePartInput

export interface AgentPromptContext {
  note?: {
    title: string
  }
  viewport?: {
    width: number
    height: number
    devicePixelRatio: number
  }
}

type ClientResult<T> = { data?: T; error?: unknown }

function apiBase(): string {
  const base = import.meta.env?.VITE_API_BASE || ''
  const path = scopedAPIPath('/api/agent')
  return base ? `${base}${path}` : path
}

type OpenCodeClient = ReturnType<typeof createOpencodeClient>

let cachedClientBase = ''
let cachedClient: OpenCodeClient | null = null

function opencodeClient() {
  const baseUrl = apiBase()
  if (!cachedClient || cachedClientBase !== baseUrl) {
    cachedClientBase = baseUrl
    cachedClient = createOpencodeClient({ baseUrl, fetch: sdkFetch })
  }
  return cachedClient
}

function sdkFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return fetch(input, { ...init, credentials: 'include' }).then((response) => {
    if (response.status === 401) notifyUnauthorized()
    return response
  })
}

export type SendMessageRequest = {
  messageID?: string
  model?: { providerID: string; modelID: string }
  agent?: string
  noReply?: boolean
  tools?: Record<string, boolean>
  format?: OutputFormat
  variant?: string
  marvoContext?: AgentPromptContext
  parts?: Array<TextPartInput | FilePartInput | AgentPartInput | SubtaskPartInput>
}

async function responseData<T>(response: Promise<ClientResult<T> | T>): Promise<{ data: T }> {
  const result = await response
  if (isClientResult(result) && result.error) {
    const msg = typeof result.error === 'string' ? result.error : errorMessage(result.error)
    throw new Error(msg)
  }
  return { data: isClientResult(result) && result.data !== undefined ? result.data : (result as T) }
}

function isClientResult<T>(value: ClientResult<T> | T): value is ClientResult<T> {
  return typeof value === 'object' && value !== null && ('data' in value || 'error' in value)
}

function errorMessage(error: unknown): string {
  if (typeof error === 'string') return error
  if (typeof error !== 'object' || error === null) return '服务不可用'
  for (const key of ['message', 'error', 'data', 'cause']) {
    if (!(key in error)) continue
    const value = errorMessage((error as Record<string, unknown>)[key])
    if (value !== '服务不可用') return value
  }
  return '服务不可用'
}

export const agentApi = {
  listSessions: () => responseData(opencodeClient().session.list()),
  createSession: (data?: Parameters<OpenCodeClient['session']['create']>[0]) =>
    responseData(opencodeClient().session.create(data)),
  deleteSession: (id: string) => responseData(opencodeClient().session.delete({ sessionID: id })),
  updateSession: (id: string, data: Omit<Parameters<OpenCodeClient['session']['update']>[0], 'sessionID'>) =>
    responseData(opencodeClient().session.update({ sessionID: id, ...data })),
  getSession: (id: string) => responseData(opencodeClient().session.get({ sessionID: id })),
  getSessionStatuses: () => responseData(opencodeClient().session.status()),
  getMessages: (sessionId: string) => responseData(opencodeClient().session.messages({ sessionID: sessionId })),
  sendMessage: (sessionId: string, data: SendMessageRequest) =>
    api.post(`/api/agent/session/${encodeURIComponent(sessionId)}/prompt_async`, data),
  abortSession: (sessionId: string) => responseData(opencodeClient().session.abort({ sessionID: sessionId })),

  listPermissions: () => responseData(opencodeClient().permission.list()),
  respondPermission: (permissionID: string, reply: 'once' | 'always' | 'reject') =>
    responseData(opencodeClient().permission.reply({ requestID: permissionID, reply })),

  listQuestions: () => responseData(opencodeClient().question.list()),
  respondQuestion: (requestID: string, answers: string[][]) =>
    responseData(opencodeClient().question.reply({ requestID, answers })),
  rejectQuestion: (requestID: string) => responseData(opencodeClient().question.reject({ requestID })),
}

function delay(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve) => {
    const timeout = window.setTimeout(resolve, ms)
    signal.addEventListener(
      'abort',
      () => {
        window.clearTimeout(timeout)
        resolve()
      },
      { once: true },
    )
  })
}

export function createSSEConnection(onEvent: (event: SSEEvent) => void, onError?: () => void): { close: () => void } {
  const controller = new AbortController()
  let retryDelay = 1000

  ;(async () => {
    while (!controller.signal.aborted) {
      try {
        const events = await opencodeClient().global.event({ signal: controller.signal })
        retryDelay = 1000
        for await (const envelope of events.stream) {
          const payload = envelope?.payload
          if (!payload || payload.type === 'sync') continue
          onEvent(payload)
        }
      } catch {
        if (controller.signal.aborted) return
        onError?.()
        await delay(retryDelay, controller.signal)
        retryDelay = Math.min(retryDelay * 2, 30000)
      }
    }
  })()

  return { close: () => controller.abort() }
}
