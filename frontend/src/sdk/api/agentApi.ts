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

export type SSEEvent = Extract<GlobalEvent['payload'], { properties: unknown }>
export type AgentFilePartInput = FilePartInput

type ClientResult<T> = { data?: T; error?: unknown }

function apiBase(): string {
  const base = import.meta.env?.VITE_API_BASE || ''
  return base ? `${base}/api/agent` : '/api/agent'
}

function sdkFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return fetch(input, { ...init, credentials: 'include' }).then((response) => {
    if (response.status === 401) notifyUnauthorized()
    return response
  })
}

const client = createOpencodeClient({
  baseUrl: apiBase(),
  fetch: sdkFetch,
})

export type SendMessageRequest = {
  messageID?: string
  model?: { providerID: string; modelID: string }
  agent?: string
  noReply?: boolean
  tools?: Record<string, boolean>
  format?: OutputFormat
  system?: string
  variant?: string
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
  listSessions: () => responseData(client.session.list()),
  createSession: (data?: Parameters<typeof client.session.create>[0]) => responseData(client.session.create(data)),
  deleteSession: (id: string) => responseData(client.session.delete({ sessionID: id })),
  updateSession: (id: string, data: Omit<Parameters<typeof client.session.update>[0], 'sessionID'>) =>
    responseData(client.session.update({ sessionID: id, ...data })),
  getSession: (id: string) => responseData(client.session.get({ sessionID: id })),
  getSessionStatuses: () => responseData(client.session.status()),
  getMessages: (sessionId: string) => responseData(client.session.messages({ sessionID: sessionId })),
  sendMessage: (sessionId: string, data: SendMessageRequest) =>
    responseData(client.session.promptAsync({ sessionID: sessionId, ...data })),
  abortSession: (sessionId: string) => responseData(client.session.abort({ sessionID: sessionId })),

  listPermissions: () => responseData(client.permission.list()),
  respondPermission: (permissionID: string, reply: 'once' | 'always' | 'reject') =>
    responseData(client.permission.reply({ requestID: permissionID, reply })),

  listQuestions: () => responseData(client.question.list()),
  respondQuestion: (requestID: string, answers: string[][]) =>
    responseData(client.question.reply({ requestID, answers })),
  rejectQuestion: (requestID: string) => responseData(client.question.reject({ requestID })),
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
        const events = await client.global.event({ signal: controller.signal })
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
