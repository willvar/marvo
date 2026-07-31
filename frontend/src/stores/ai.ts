import { create } from 'zustand'
import { aiApi, createSSEConnection, SendMessageRequest, SSEEvent } from '../api/aiApi'

export interface AISession {
  id: string
  title: string
  time: { created: number; updated: number }
  [key: string]: any
}

export interface MessageInfo {
  id: string
  role: string
  sessionID: string
  modelID?: string
  providerID?: string
  error?: any
  time?: { created: number; completed?: number }
  [key: string]: any
}

export interface MessagePart {
  id: string
  type: string
  messageID: string
  text?: string
  callID?: string
  tool?: string
  state?: { status: string; input?: any; output?: string; title?: string; metadata?: any; error?: string; time?: any }
  [key: string]: any
}

export interface PermissionRequest {
  id: string
  sessionID: string
  permission: string
  patterns: string[]
  metadata?: Record<string, any>
  always: string[]
  tool?: { messageID: string; callID: string }
}

export interface QuestionRequest {
  id: string
  sessionID: string
  questions: Array<{
    header?: string
    question: string
    options: Array<{ label: string; description: string }>
    multiple?: boolean
    custom?: boolean
  }>
}

interface AIState {
  connected: boolean
  sessions: AISession[]
  currentSessionId: string | null
  messages: MessageInfo[]
  parts: Record<string, MessagePart[]>
  loading: boolean
  sending: boolean
  permissions: Record<string, PermissionRequest[]>
  questions: Record<string, QuestionRequest[]>

  eventSource: { close: () => void } | null
  connect: () => void
  disconnect: () => void
  loadSessions: () => Promise<void>
  createSession: () => Promise<string | undefined>
  selectSession: (id: string) => Promise<void>
  deleteSession: (id: string) => Promise<void>
  sendMessage: (text: string) => Promise<void>
  abortSession: () => Promise<void>
  respondPermission: (permissionID: string, reply: 'once' | 'always' | 'reject') => Promise<void>
  respondQuestion: (requestID: string, answers: string[][]) => Promise<void>
  rejectQuestion: (requestID: string) => Promise<void>
  handleEvent: (event: SSEEvent) => void
}

function extractInfo(raw: any): MessageInfo | null {
  if (!raw) return null
  if (raw.id && raw.role) return { ...raw }
  if (raw.info) return { ...raw.info, parts: undefined }
  return null
}

function extractParts(raw: any): MessagePart[] {
  if (Array.isArray(raw.parts)) return raw.parts
  if (Array.isArray(raw.info?.parts)) return raw.info.parts
  return []
}

export const useAIStore = create<AIState>((set, get) => ({
  connected: false,
  sessions: [],
  currentSessionId: null,
  messages: [],
  parts: {},
  loading: false,
  sending: false,
  permissions: {},
  questions: {},
  eventSource: null,

  connect: () => {
    if (get().eventSource) return
    const es = createSSEConnection(
      (event) => get().handleEvent(event),
      () => set({ connected: false }),
    )
    set({ eventSource: es })
  },

  disconnect: () => {
    const es = get().eventSource
    if (es) {
      es.close()
      set({ eventSource: null, connected: false })
    }
  },

  loadSessions: async () => {
    set({ loading: true })
    try {
      const { data } = await aiApi.listSessions()
      set({ sessions: Array.isArray(data) ? data : [] })
    } finally {
      set({ loading: false })
    }
  },

  createSession: async () => {
    const { data } = await aiApi.createSession()
    set((state) => ({
      sessions: [data, ...state.sessions],
      currentSessionId: data.id,
      messages: [],
      parts: {},
      permissions: {},
      questions: {},
    }))
    return data.id
  },

  selectSession: async (id: string) => {
    set({ currentSessionId: id, messages: [], parts: {}, loading: true, permissions: {}, questions: {} })
    try {
      const [msgRes, permRes, qRes] = await Promise.allSettled([
        aiApi.getMessages(id),
        aiApi.listPermissions(),
        aiApi.listQuestions(),
      ])
      if (msgRes.status === 'fulfilled') {
        const data = msgRes.value.data
        const items = Array.isArray(data) ? data : []
        const messages: MessageInfo[] = []
        const parts: Record<string, MessagePart[]> = {}
        for (const item of items) {
          const info = extractInfo(item)
          if (info) messages.push(info)
          const itemParts = extractParts(item)
          if (itemParts.length > 0 && info) {
            parts[info.id] = itemParts
          }
        }
        set({ messages, parts })
      }
      if (permRes.status === 'fulfilled') {
        const perms = Array.isArray(permRes.value.data) ? permRes.value.data : []
        const grouped: Record<string, PermissionRequest[]> = {}
        for (const p of perms) grouped[p.sessionID] = [...(grouped[p.sessionID] || []), p]
        set({ permissions: grouped })
      }
      if (qRes.status === 'fulfilled') {
        const qs = Array.isArray(qRes.value.data) ? qRes.value.data : []
        const grouped: Record<string, QuestionRequest[]> = {}
        for (const q of qs) grouped[q.sessionID] = [...(grouped[q.sessionID] || []), q]
        set({ questions: grouped })
      }
    } finally {
      set({ loading: false })
    }
  },

  deleteSession: async (id: string) => {
    await aiApi.deleteSession(id)
    set((state) => ({
      sessions: state.sessions.filter((s) => s.id !== id),
      currentSessionId: state.currentSessionId === id ? null : state.currentSessionId,
      messages: state.currentSessionId === id ? [] : state.messages,
      parts: state.currentSessionId === id ? {} : state.parts,
      permissions: state.currentSessionId === id ? {} : state.permissions,
      questions: state.currentSessionId === id ? {} : state.questions,
    }))
  },

  sendMessage: async (text: string) => {
    const { currentSessionId, messages } = get()
    if (!currentSessionId) return

    set({ sending: true })

    const localId = `local_${Date.now()}`
    const userMsg: MessageInfo = { id: localId, role: 'user', sessionID: currentSessionId }
    const localPart: MessagePart = { id: `local_p_${Date.now()}`, type: 'text', messageID: localId, text }

    const lastMsg = messages[messages.length - 1]
    const lastIsLocalUser = lastMsg?.role === 'user' && lastMsg.id.startsWith('local_')

    if (!lastIsLocalUser) {
      set((state) => ({
        messages: [...state.messages, userMsg],
        parts: { ...state.parts, [localId]: [localPart] },
      }))
    }

    try {
      const request: SendMessageRequest = {
        parts: [{ type: 'text', text }],
        agent: 'build',
        model: { providerID: 'openaj', modelID: 'gpt-5.5' },
      }
      await aiApi.sendMessage(currentSessionId, request)
    } catch {
      set({ sending: false })
    }
  },

  abortSession: async () => {
    const { currentSessionId } = get()
    if (!currentSessionId) return
    await aiApi.abortSession(currentSessionId)
    set({ sending: false })
  },

  respondPermission: async (permissionID: string, reply: 'once' | 'always' | 'reject') => {
    const { currentSessionId } = get()
    try {
      await aiApi.respondPermission(permissionID, reply)
    } catch {
      throw new Error('permission response failed')
    }
    set((s) => ({
      permissions: {
        ...s.permissions,
        [currentSessionId!]: (s.permissions[currentSessionId!] || []).filter((p) => p.id !== permissionID),
      },
    }))
  },

  respondQuestion: async (requestID: string, answers: string[][]) => {
    const { currentSessionId } = get()
    try {
      await aiApi.respondQuestion(requestID, answers)
    } catch {
      throw new Error('question response failed')
    }
    set((s) => ({
      questions: {
        ...s.questions,
        [currentSessionId!]: (s.questions[currentSessionId!] || []).filter((q) => q.id !== requestID),
      },
    }))
  },

  rejectQuestion: async (requestID: string) => {
    const { currentSessionId } = get()
    try {
      await aiApi.rejectQuestion(requestID)
    } catch {
      throw new Error('question reject failed')
    }
    set((s) => ({
      questions: {
        ...s.questions,
        [currentSessionId!]: (s.questions[currentSessionId!] || []).filter((q) => q.id !== requestID),
      },
    }))
  },

  handleEvent: (event: SSEEvent) => {
    const { type, properties: props } = event
    const state = get()

    switch (type) {
      case 'server.connected':
        set({ connected: true })
        break

      case 'session.created':
        if (props.info && !state.sessions.find((s) => s.id === props.info.id)) {
          set((s) => ({ sessions: [props.info, ...s.sessions] }))
        }
        break

      case 'session.updated':
        set((s) => ({
          sessions: s.sessions.map((sess) =>
            sess.id === props.sessionID ? { ...sess, ...props.info } : sess
          ),
        }))
        break

      case 'session.deleted':
        set((s) => ({
          sessions: s.sessions.filter((sess) => sess.id !== props.sessionID),
          currentSessionId: s.currentSessionId === props.sessionID ? null : s.currentSessionId,
        }))
        break

      case 'session.status':
        if (props.sessionID === state.currentSessionId) {
          if (props.status?.type === 'busy') set({ sending: true })
          if (props.status?.type === 'idle') set({ sending: false })
        }
        break

      case 'session.idle': {
        if (props.sessionID === state.currentSessionId) set({ sending: false })
        break
      }

      case 'session.diff':
        break

      case 'message.updated': {
        if (props.sessionID !== state.currentSessionId) break
        const info = extractInfo(props.info)
        if (!info) break
        set((s) => {
          const idx = s.messages.findIndex((m) => m.id === info.id)
          if (idx >= 0) {
            const msgs = [...s.messages]
            msgs[idx] = { ...msgs[idx], ...info }
            return { messages: msgs }
          }
          if (info.role === 'user') {
            const localIdx = s.messages.findIndex((m) => m.role === 'user' && m.id.startsWith('local_'))
            if (localIdx >= 0) {
              const msgs = [...s.messages]
              const oldId = msgs[localIdx].id
              msgs[localIdx] = info
              const newParts = { ...s.parts }
              if (newParts[oldId]) {
                newParts[info.id] = newParts[oldId]
                delete newParts[oldId]
              }
              return { messages: msgs, parts: newParts }
            }
          }
          return { messages: [...s.messages, info] }
        })
        break
      }

      case 'message.part.updated': {
        if (props.sessionID !== state.currentSessionId) break
        const part = props.part as MessagePart
        set((s) => {
          const msgID = part.messageID
          const hasMsg = s.messages.some((m) => m.id === msgID)
          const currentParts = s.parts[msgID] || []
          const partIdx = currentParts.findIndex((p) => p.id === part.id)
          let newParts = [...currentParts]
          if (partIdx >= 0) {
            newParts[partIdx] = { ...newParts[partIdx], ...part }
          } else {
            if (part.type === 'text') {
              newParts = newParts.filter((p) => !p.id.startsWith('local_'))
            }
            newParts.push(part)
          }
          const updates: any = { parts: { ...s.parts, [msgID]: newParts } }
          if (!hasMsg) {
            updates.messages = [...s.messages, {
              id: msgID,
              role: 'assistant',
              sessionID: props.sessionID,
              time: { created: Date.now() / 1000 },
            }]
          }
          return updates
        })
        break
      }

      case 'message.part.delta': {
        if (props.sessionID !== state.currentSessionId) break
        const { messageID, partID, field, delta } = props
        const targetField = field || 'text'
        set((s) => {
          const currentParts = s.parts[messageID] || []
          const newParts = [...currentParts]
          const partIdx = newParts.findIndex((p) => p.id === partID)
          if (partIdx < 0) return {}
          const updated = { ...newParts[partIdx] }
          updated[targetField] = (updated[targetField] || '') + delta
          newParts[partIdx] = updated
          return { parts: { ...s.parts, [messageID]: newParts } }
        })
        break
      }

      case 'message.part.removed': {
        if (props.sessionID !== state.currentSessionId) break
        set((s) => {
          const msgID = props.messageID as string
          const currentParts = s.parts[msgID] || []
          return { parts: { ...s.parts, [msgID]: currentParts.filter((p) => p.id !== props.partID) } }
        })
        break
      }

      case 'message.removed': {
        if (props.sessionID !== state.currentSessionId) break
        const msgID = props.messageID as string
        set((s) => {
          const msgs = s.messages.filter((m) => m.id !== msgID)
          const newParts = { ...s.parts }
          delete newParts[msgID]
          return { messages: msgs, parts: newParts }
        })
        break
      }

      case 'permission.asked': {
        const perm = props as PermissionRequest
        set((s) => ({
          permissions: {
            ...s.permissions,
            [perm.sessionID]: [...(s.permissions[perm.sessionID] || []), perm],
          },
        }))
        break
      }

      case 'permission.replied': {
        const { sessionID, requestID } = props
        set((s) => ({
          permissions: {
            ...s.permissions,
            [sessionID]: (s.permissions[sessionID] || []).filter((p) => p.id !== requestID),
          },
        }))
        break
      }

      case 'question.asked': {
        const q = props as QuestionRequest
        set((s) => ({
          questions: {
            ...s.questions,
            [q.sessionID]: [...(s.questions[q.sessionID] || []), q],
          },
        }))
        break
      }

      case 'question.replied':
      case 'question.rejected': {
        const { sessionID, requestID } = props
        set((s) => ({
          questions: {
            ...s.questions,
            [sessionID]: (s.questions[sessionID] || []).filter((q) => q.id !== requestID),
          },
        }))
        break
      }
    }
  },
}))
