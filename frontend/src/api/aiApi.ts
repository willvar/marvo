import { createOpencodeClient } from '@opencode-ai/sdk/v2/client'

export interface SendMessageRequest {
  parts: Array<{ type: string; text: string }>
  agent?: string
  model?: {
    providerID: string
    modelID: string
  }
}

export interface SSEEvent {
  id: string
  type: string
  properties: Record<string, any>
}

function apiBase(): string {
  const base = import.meta.env.VITE_API_BASE || ''
  return base ? `${base}/api/ai` : '/api/ai'
}

function sdkFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return fetch(input, { ...init, credentials: 'include' }).then((response) => {
    if (response.status === 401) window.location.href = '/login'
    return response
  })
}

const client = createOpencodeClient({
  baseUrl: apiBase(),
  fetch: sdkFetch,
})

async function responseData<T = any>(response: Promise<any>): Promise<{ data: T }> {
  const result = await response
  return { data: result?.data ?? result }
}

export const aiApi = {
  listSessions: () => responseData(client.session.list()),
  createSession: (data?: Record<string, any>) => responseData(client.session.create(data as any)),
  deleteSession: (id: string) => responseData(client.session.delete({ sessionID: id })),
  getMessages: (sessionId: string) => responseData(client.session.messages({ sessionID: sessionId })),
  sendMessage: (sessionId: string, data: SendMessageRequest) =>
    responseData(client.session.promptAsync({ sessionID: sessionId, ...data } as any)),
  abortSession: (sessionId: string) => responseData(client.session.abort({ sessionID: sessionId })),

  listPermissions: () => responseData(client.permission.list()),
  respondPermission: (permissionID: string, reply: 'once' | 'always' | 'reject') =>
    responseData(client.permission.reply({ requestID: permissionID, reply })),

  listQuestions: () => responseData(client.question.list()),
  respondQuestion: (requestID: string, answers: string[][]) =>
    responseData(client.question.reply({ requestID, answers: answers as any })),
  rejectQuestion: (requestID: string) =>
    responseData(client.question.reject({ requestID })),
}

export function createSSEConnection(
  onEvent: (event: SSEEvent) => void,
  onError?: () => void,
): { close: () => void } {
  const controller = new AbortController()

  ;(async () => {
    try {
      const events = await client.global.event({ signal: controller.signal } as any)
      for await (const envelope of events.stream as AsyncIterable<any>) {
        const payload = envelope?.payload
        if (!payload || payload.type === 'sync') continue
        onEvent(payload)
      }
    } catch {
      if (!controller.signal.aborted) onError?.()
    }
  })()

  return { close: () => controller.abort() }
}
