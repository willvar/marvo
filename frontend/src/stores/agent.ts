import { defineStore } from 'pinia'
import { v4 as uuidv4 } from 'uuid'
import type { SessionStatus } from '@opencode-ai/sdk/v2/client'
import {
  agentApi,
  clearAgentDraft,
  conciseAgentErrorDetail,
  createSSEConnection,
  formatAgentError,
  isAbortedAgentError,
  type AgentConnectionState,
  type AgentFilePartInput,
  type SSEEvent,
  type AgentSession,
  type AgentSessionError,
  type MessageInfo,
  type MessagePart,
  type PermissionRequest,
  type QuestionRequest,
} from '../sdk'
import {
  adoptOptimisticUserMessage,
  hasTerminalAgentResponse,
  markOptimisticMessage,
  mergeAgentMessagePart,
  mergeMessageCollections,
  type MessageCollection,
} from './agentMessageState'
import {
  agentRootSessionID,
  agentSessionTreeIDs,
  agentSessionTreeRequest,
  agentSessionTreeStatus,
  agentSessionTreeValue,
} from './agentSessionTree'

const CURRENT_SESSION_KEY = 'marvo.agent.currentSessionId'
const FLOATING_SESSION_KEY = 'marvo.agent.floatingSessionId'
const FLOATING_NOTE_KEY = 'marvo.agent.floatingNoteTitle'
let sessionsLoadPromise: Promise<void> | null = null
const sessionSettlementVersions = new Map<string, number>()

function invalidateSessionSettlement(sessionID: string) {
  const version = (sessionSettlementVersions.get(sessionID) || 0) + 1
  sessionSettlementVersions.set(sessionID, version)
  return version
}

function isBusy(status?: SessionStatus) {
  return status?.type === 'busy' || status?.type === 'retry'
}

function extractInfo(raw: any): MessageInfo | null {
  if (!raw) return null
  if (raw.id && raw.role) return { ...raw }
  if (raw.info) return { ...raw.info, parts: undefined }
  if (raw.id && raw.type) {
    const role = raw.type === 'user' ? 'user' : raw.type === 'assistant' ? 'assistant' : raw.type
    return { id: raw.id, role, sessionID: raw.sessionID, time: raw.time, ...raw }
  }
  return null
}

function extractParts(raw: any): MessagePart[] {
  if (Array.isArray(raw.parts)) return raw.parts
  if (Array.isArray(raw.info?.parts)) return raw.info.parts
  if (Array.isArray(raw.content)) {
    return raw.content.map((c: any) => ({
      id: c.id || `${raw.id}_${c.type}`,
      type: c.type,
      messageID: raw.id,
      sessionID: raw.sessionID,
      text: c.text,
      tool: c.name,
      callID: c.callID,
      state: c.state,
      ...c,
    }))
  }
  if (raw.type === 'user' && raw.text) {
    return [{ id: `${raw.id}_text`, type: 'text', messageID: raw.id, text: raw.text }]
  }
  return []
}

function normalizeMessages(raw: any): { messages: MessageInfo[]; parts: Record<string, MessagePart[]> } {
  const items = Array.isArray(raw) ? raw : []
  const messages: MessageInfo[] = []
  const parts: Record<string, MessagePart[]> = {}
  for (const item of items) {
    const info = extractInfo(item)
    if (info) messages.push(info)
    const itemParts = extractParts(item)
    if (itemParts.length > 0 && info) parts[info.id] = itemParts
  }
  return { messages, parts }
}

interface ConversationState {
  messages: MessageInfo[]
  parts: Record<string, MessagePart[]>
  loading: boolean
  loaded: boolean
  sending: boolean
  stopping: boolean
  loadVersion: number
  contentVersion: number
  error: string
  stale: boolean
}

function createConversationState(): ConversationState {
  return {
    messages: [],
    parts: {},
    loading: false,
    loaded: false,
    sending: false,
    stopping: false,
    loadVersion: 0,
    contentVersion: 0,
    error: '',
    stale: false,
  }
}

function updateMessage(collection: MessageCollection, info: MessageInfo): MessageCollection {
  const index = collection.messages.findIndex((message) => message.id === info.id)
  if (index >= 0) {
    const messages = [...collection.messages]
    messages[index] = { ...messages[index], ...info }
    return { messages, parts: collection.parts }
  }

  const adopted = adoptOptimisticUserMessage(collection, info)
  if (adopted) return adopted

  return { messages: [...collection.messages, info], parts: collection.parts }
}

function updateMessagePart(collection: MessageCollection, part: MessagePart, sessionID: string): MessageCollection {
  const messageID = part.messageID
  const current = collection.parts[messageID] || []
  const index = current.findIndex((item) => item.id === part.id)
  let messageParts: MessagePart[]
  if (index >= 0) {
    messageParts = [...current]
    messageParts[index] = mergeAgentMessagePart(part, messageParts[index])
  } else {
    messageParts = part.type === 'text' ? current.filter((item) => !item.id.startsWith('local_')) : [...current]
    messageParts.push(part)
  }

  const messages = collection.messages.some((message) => message.id === messageID)
    ? collection.messages
    : [...collection.messages, { id: messageID, role: 'assistant', sessionID, time: { created: Date.now() } }]
  return { messages, parts: { ...collection.parts, [messageID]: messageParts } }
}

function appendMessagePartDelta(
  collection: MessageCollection,
  messageID: string,
  partID: string,
  field: string,
  delta: string,
): MessageCollection {
  const current = collection.parts[messageID] || []
  const index = current.findIndex((part) => part.id === partID)
  if (index < 0) return collection
  const messageParts = [...current]
  const part = { ...messageParts[index] }
  part[field] = (part[field] || '') + delta
  messageParts[index] = part
  return { messages: collection.messages, parts: { ...collection.parts, [messageID]: messageParts } }
}

function removeMessagePart(collection: MessageCollection, messageID: string, partID: string): MessageCollection {
  const current = collection.parts[messageID] || []
  return {
    messages: collection.messages,
    parts: { ...collection.parts, [messageID]: current.filter((part) => part.id !== partID) },
  }
}

function removeMessage(collection: MessageCollection, messageID: string): MessageCollection {
  const parts = { ...collection.parts }
  delete parts[messageID]
  return {
    messages: collection.messages.filter((message) => message.id !== messageID),
    parts,
  }
}

function eventSessionID(properties: any): string {
  const value = properties?.sessionID ?? properties?.info?.sessionID ?? properties?.part?.sessionID
  return typeof value === 'string' ? value : ''
}

function normalizePermission(raw: any): PermissionRequest {
  return {
    ...raw,
    permission: raw?.permission || raw?.action || 'operation',
    patterns: Array.isArray(raw?.patterns) ? raw.patterns : Array.isArray(raw?.resources) ? raw.resources : [],
    metadata: raw?.metadata || {},
    always: Array.isArray(raw?.always) ? raw.always : [],
  } as PermissionRequest
}

function groupPermissions(raw: any) {
  const grouped: Record<string, PermissionRequest[]> = {}
  for (const value of Array.isArray(raw) ? raw : []) {
    const permission = normalizePermission(value)
    if (permission.id && permission.sessionID)
      grouped[permission.sessionID] = [...(grouped[permission.sessionID] || []), permission]
  }
  return grouped
}

function groupQuestions(raw: any) {
  const grouped: Record<string, QuestionRequest[]> = {}
  for (const question of Array.isArray(raw) ? raw : []) {
    if (question?.id && question?.sessionID)
      grouped[question.sessionID] = [...(grouped[question.sessionID] || []), question]
  }
  return grouped
}

function browserContextSystem() {
  if (typeof window === 'undefined') return undefined
  const vw = Math.round(window.innerWidth)
  const vh = Math.round(window.innerHeight)
  const dpr = Math.round(window.devicePixelRatio * 100) / 100
  return `Client context: viewport=${vw}x${vh}px, devicePixelRatio=${dpr}`
}

function systemContext(extra?: string) {
  return [browserContextSystem(), extra].filter(Boolean).join('\n') || undefined
}

export const useAgentStore = defineStore('agent', {
  state: () => ({
    connected: false,
    connectionState: 'disconnected' as AgentConnectionState,
    sessions: [] as AgentSession[],
    allSessions: [] as AgentSession[],
    sessionsLoaded: false,
    sessionsError: '',
    currentSessionId: null as string | null,
    conversations: {} as Record<string, ConversationState>,
    sessionsLoading: false,
    sessionStatuses: {} as Record<string, SessionStatus>,
    permissions: {} as Record<string, PermissionRequest[]>,
    questions: {} as Record<string, QuestionRequest[]>,
    sessionErrors: {} as Record<string, AgentSessionError>,
    globalError: null as AgentSessionError | null,
    floatingSessionId: null as string | null,
    floatingMessages: [] as MessageInfo[],
    floatingParts: {} as Record<string, MessagePart[]>,
    floatingSending: false,
    floatingStopping: false,
    floatingNoteTitle: localStorage.getItem(FLOATING_NOTE_KEY) || '',
    messageTombstones: {} as Record<string, Record<string, true>>,
    partTombstones: {} as Record<string, Record<string, Record<string, true>>>,
    eventSource: null as { close: () => void } | null,
  }),

  getters: {
    messages: (state) => (state.currentSessionId ? state.conversations[state.currentSessionId]?.messages || [] : []),
    parts: (state) => (state.currentSessionId ? state.conversations[state.currentSessionId]?.parts || {} : {}),
    sending: (state) => {
      if (!state.currentSessionId) return false
      return (
        state.conversations[state.currentSessionId]?.sending ||
        isBusy(agentSessionTreeStatus(state.allSessions, state.sessionStatuses, state.currentSessionId))
      )
    },
    stopping: (state) =>
      state.currentSessionId ? state.conversations[state.currentSessionId]?.stopping || false : false,
    conversationLoading: (state) =>
      state.currentSessionId ? state.conversations[state.currentSessionId]?.loading || false : false,
    messagesLoading: (state) => {
      if (!state.currentSessionId) return false
      const conversation = state.conversations[state.currentSessionId]
      return !!conversation?.loading && !conversation.loaded
    },
    conversationError: (state) =>
      state.currentSessionId ? state.conversations[state.currentSessionId]?.error || '' : '',
  },

  actions: {
    sessionTreeIDs(id?: string | null) {
      return agentSessionTreeIDs(this.allSessions, id)
    },

    rootSessionID(id?: string | null) {
      return agentRootSessionID(this.allSessions, id)
    },

    pendingPermission(id?: string | null) {
      return agentSessionTreeRequest(this.allSessions, this.permissions, id)
    },

    pendingQuestion(id?: string | null) {
      return agentSessionTreeRequest(this.allSessions, this.questions, id)
    },

    hasPendingRequest(id?: string | null) {
      return !!this.pendingPermission(id) || !!this.pendingQuestion(id)
    },

    statusForSession(id?: string | null) {
      return agentSessionTreeStatus(this.allSessions, this.sessionStatuses, id)
    },

    errorForSession(id?: string | null) {
      return agentSessionTreeValue(this.allSessions, this.sessionErrors, id)
    },

    sessionIndicator(id: string): 'attention' | 'retry' | 'running' | 'error' | undefined {
      if (this.hasPendingRequest(id)) return 'attention'
      const status = this.statusForSession(id)
      if (status?.type === 'retry') return 'retry'
      if (status?.type === 'busy') return 'running'
      if (this.errorForSession(id)) return 'error'
    },

    upsertSession(session: AgentSession) {
      const allIndex = this.allSessions.findIndex((item) => item.id === session.id)
      if (allIndex >= 0) {
        const next = [...this.allSessions]
        next[allIndex] = { ...next[allIndex], ...session }
        this.allSessions = next
      } else {
        this.allSessions = [session, ...this.allSessions]
      }
      if (session.parentID) {
        this.sessions = this.sessions.filter((item) => item.id !== session.id)
        return
      }
      const rootIndex = this.sessions.findIndex((item) => item.id === session.id)
      if (rootIndex >= 0) {
        const next = [...this.sessions]
        next[rootIndex] = { ...next[rootIndex], ...session }
        this.sessions = next
      } else {
        this.sessions = [session, ...this.sessions]
      }
    },

    async ensureSessionLineage(sessionID: string) {
      let id = sessionID
      const seen = new Set<string>()
      while (id && !seen.has(id)) {
        seen.add(id)
        let session = this.allSessions.find((item) => item.id === id)
        if (!session) {
          try {
            const { data } = await agentApi.getSession(id)
            session = data
            this.upsertSession(data)
          } catch {
            return
          }
        }
        id = session.parentID || ''
      }
    },

    async ensureRuntimeLineages() {
      const sessionIDs = new Set<string>()
      for (const [sessionID, requests] of Object.entries(this.permissions)) {
        if (requests?.length) sessionIDs.add(sessionID)
      }
      for (const [sessionID, requests] of Object.entries(this.questions)) {
        if (requests?.length) sessionIDs.add(sessionID)
      }
      for (const [sessionID, status] of Object.entries(this.sessionStatuses)) {
        if (isBusy(status)) sessionIDs.add(sessionID)
      }
      for (const sessionID of Object.keys(this.sessionErrors)) sessionIDs.add(sessionID)
      await Promise.all([...sessionIDs].map((sessionID) => this.ensureSessionLineage(sessionID)))
    },

    clearSessionErrors(id: string) {
      const errors = { ...this.sessionErrors }
      for (const sessionID of this.sessionTreeIDs(id)) delete errors[sessionID]
      this.sessionErrors = errors
    },

    ensureConversation(id: string): ConversationState {
      if (!this.conversations[id]) {
        this.conversations[id] = createConversationState()
      }
      return this.conversations[id]
    },

    removeConversation(id: string) {
      const conversations = { ...this.conversations }
      delete conversations[id]
      this.conversations = conversations
    },

    setConversationCollection(id: string, collection: MessageCollection) {
      const conversation = this.ensureConversation(id)
      conversation.messages = collection.messages
      conversation.parts = collection.parts
      conversation.loaded = true
      conversation.error = ''
      conversation.stale = false
      conversation.contentVersion++
    },

    filterSnapshotCollection(id: string, collection: MessageCollection): MessageCollection {
      const removedMessages = this.messageTombstones[id] || {}
      const removedParts = this.partTombstones[id] || {}
      const messages = collection.messages.filter((message) => !removedMessages[message.id])
      const parts: Record<string, MessagePart[]> = {}
      for (const message of messages) {
        const filtered = (collection.parts[message.id] || []).filter((part) => !removedParts[message.id]?.[part.id])
        if (filtered.length > 0) parts[message.id] = filtered
      }
      return { messages, parts }
    },

    recordMessageRemoval(id: string, messageID: string) {
      this.messageTombstones = {
        ...this.messageTombstones,
        [id]: { ...this.messageTombstones[id], [messageID]: true },
      }
    },

    recordPartRemoval(id: string, messageID: string, partID: string) {
      this.partTombstones = {
        ...this.partTombstones,
        [id]: {
          ...this.partTombstones[id],
          [messageID]: { ...this.partTombstones[id]?.[messageID], [partID]: true },
        },
      }
    },

    clearSessionTombstones(id: string) {
      const messageTombstones = { ...this.messageTombstones }
      const partTombstones = { ...this.partTombstones }
      delete messageTombstones[id]
      delete partTombstones[id]
      this.messageTombstones = messageTombstones
      this.partTombstones = partTombstones
    },

    setSessionSending(id: string, sending: boolean) {
      invalidateSessionSettlement(id)
      const rootID = this.rootSessionID(id) || id
      const statusIDs = sending ? [id] : this.sessionTreeIDs(rootID)
      const statuses = { ...this.sessionStatuses }
      for (const sessionID of statusIDs.length > 0 ? statusIDs : [id]) {
        statuses[sessionID] = { type: sending ? 'busy' : 'idle' }
      }
      this.sessionStatuses = statuses
      if (this.conversations[rootID]) {
        this.conversations[rootID].sending = sending
        if (!sending) this.conversations[rootID].stopping = false
      }
      if (this.floatingSessionId === rootID) {
        this.floatingSending = sending
        if (!sending) this.floatingStopping = false
      }
    },

    setSessionStopping(id: string, stopping: boolean) {
      const rootID = this.rootSessionID(id) || id
      if (this.conversations[rootID]) this.conversations[rootID].stopping = stopping
      if (this.floatingSessionId === rootID) this.floatingStopping = stopping
    },

    async loadConversation(id: string) {
      const conversation = this.ensureConversation(id)
      if (conversation.loading) return
      conversation.loading = true
      conversation.error = ''
      const loadVersion = ++conversation.loadVersion
      try {
        const { data } = await agentApi.getMessages(id)
        if (this.conversations[id] !== conversation || conversation.loadVersion !== loadVersion) return
        const normalized = this.filterSnapshotCollection(id, normalizeMessages(data))
        const collection = mergeMessageCollections(normalized, conversation)
        conversation.messages = collection.messages
        conversation.parts = collection.parts
        conversation.loaded = true
        conversation.stale = false
        conversation.contentVersion++
      } catch (cause) {
        const message = cause instanceof Error ? cause.message : '无法加载对话'
        conversation.error = message
        conversation.stale = conversation.loaded
        if (!conversation.loaded) throw cause
      } finally {
        if (this.conversations[id] === conversation && conversation.loadVersion === loadVersion) {
          conversation.loading = false
        }
      }
    },

    async finalizeSession(id: string) {
      const settlementVersion = invalidateSessionSettlement(id)
      let terminal = false
      try {
        const { data } = await agentApi.getMessages(id)
        if (sessionSettlementVersions.get(id) !== settlementVersion) return
        const snapshot = this.filterSnapshotCollection(id, normalizeMessages(data))
        const conversation = this.conversations[id]
        if (conversation) {
          const collection = mergeMessageCollections(snapshot, conversation)
          this.setConversationCollection(id, collection)
          terminal ||= hasTerminalAgentResponse(collection)
        }
        if (this.floatingSessionId === id) {
          const collection = mergeMessageCollections(snapshot, {
            messages: this.floatingMessages,
            parts: this.floatingParts,
          })
          this.floatingMessages = collection.messages
          this.floatingParts = collection.parts
          terminal ||= hasTerminalAgentResponse(collection)
        }
      } catch (cause) {
        const conversation = this.conversations[id]
        if (conversation) {
          conversation.error = cause instanceof Error ? cause.message : '无法刷新对话'
          conversation.stale = conversation.loaded
        }
      }
      if (sessionSettlementVersions.get(id) !== settlementVersion || isBusy(this.sessionStatuses[id]) || !terminal) {
        return
      }
      if (this.conversations[id]) {
        this.conversations[id].sending = false
        this.conversations[id].stopping = false
      }
      if (this.floatingSessionId === id) {
        this.floatingSending = false
        this.floatingStopping = false
      }
    },

    connect() {
      if (this.eventSource) return
      this.connectionState = 'connecting'
      const es = createSSEConnection(
        (event) => this.handleEvent(event),
        () => {
          this.connected = false
          this.connectionState = 'reconnecting'
        },
      )
      this.eventSource = es
    },

    disconnect() {
      this.eventSource?.close()
      this.eventSource = null
      this.connected = false
      this.connectionState = 'disconnected'
    },

    async reconnect() {
      this.disconnect()
      this.sessionsError = ''
      this.connect()
      await this.loadSessions()
    },

    async loadSessions() {
      if (sessionsLoadPromise) {
        await sessionsLoadPromise
        return
      }
      this.sessionsLoading = true
      this.sessionsError = ''
      const request = (async () => {
        const [sessionRes, statusRes, permissionRes, questionRes] = await Promise.allSettled([
          agentApi.listSessions(),
          agentApi.getSessionStatuses(),
          agentApi.listPermissions(),
          agentApi.listQuestions(),
        ])
        if (sessionRes.status === 'fulfilled') {
          const all = Array.isArray(sessionRes.value.data) ? sessionRes.value.data : []
          this.allSessions = all
          this.sessions = all.filter((session) => !session.parentID)
          this.sessionsLoaded = true
        } else {
          this.sessionsError = sessionRes.reason instanceof Error ? sessionRes.reason.message : '无法加载智能体对话'
        }
        if (statusRes.status === 'fulfilled') {
          this.sessionStatuses = statusRes.value.data || {}
        }
        if (permissionRes.status === 'fulfilled') this.permissions = groupPermissions(permissionRes.value.data)
        if (questionRes.status === 'fulfilled') this.questions = groupQuestions(questionRes.value.data)
        await this.ensureRuntimeLineages()
        const savedCurrent = localStorage.getItem(CURRENT_SESSION_KEY)
        if (!this.currentSessionId && savedCurrent && this.sessions.some((session) => session.id === savedCurrent)) {
          this.currentSessionId = savedCurrent
        }
        for (const [id, conversation] of Object.entries(this.conversations)) {
          if (isBusy(this.statusForSession(id))) {
            invalidateSessionSettlement(id)
            conversation.sending = true
          } else if (conversation.sending) {
            void this.finalizeSession(id)
          }
        }
        if (sessionRes.status === 'rejected' && !this.sessionsLoaded) throw sessionRes.reason
      })()
      sessionsLoadPromise = request
      try {
        await request
      } finally {
        if (sessionsLoadPromise === request) {
          sessionsLoadPromise = null
          this.sessionsLoading = false
        }
      }
    },

    async createSession() {
      const { data } = await agentApi.createSession()
      this.upsertSession(data)
      this.currentSessionId = data.id
      localStorage.setItem(CURRENT_SESSION_KEY, data.id)
      const conversation = this.ensureConversation(data.id)
      conversation.loading = false
      conversation.loaded = true
      this.setSessionSending(data.id, false)
      this.clearSessionErrors(data.id)
      this.permissions = { ...this.permissions, [data.id]: this.permissions[data.id] || [] }
      this.questions = { ...this.questions, [data.id]: this.questions[data.id] || [] }
      return data.id
    },

    async selectSession(id: string) {
      const alreadyCurrent = this.currentSessionId === id
      this.currentSessionId = id
      localStorage.setItem(CURRENT_SESSION_KEY, id)
      const conversation = this.ensureConversation(id)
      const status = this.statusForSession(id)
      if (status) {
        if (isBusy(status)) {
          invalidateSessionSettlement(id)
          conversation.sending = true
        } else if (conversation.sending) {
          void this.finalizeSession(id)
        }
      }
      if (alreadyCurrent && (conversation.loaded || conversation.loading)) return
      await this.loadConversation(id)
    },

    async deleteSession(id: string) {
      await agentApi.deleteSession(id)
      invalidateSessionSettlement(id)
      clearAgentDraft(id)
      const removedIDs = this.sessionTreeIDs(id)
      this.sessions = this.sessions.filter((s) => s.id !== id)
      this.allSessions = this.allSessions.filter((session) => !removedIDs.includes(session.id))
      this.removeConversation(id)
      const permissions = { ...this.permissions }
      const questions = { ...this.questions }
      for (const sessionID of removedIDs) {
        delete permissions[sessionID]
        delete questions[sessionID]
      }
      this.permissions = permissions
      this.questions = questions
      const errors = { ...this.sessionErrors }
      for (const sessionID of removedIDs) delete errors[sessionID]
      this.sessionErrors = errors
      this.clearSessionTombstones(id)
      if (this.currentSessionId === id) {
        this.currentSessionId = null
        localStorage.removeItem(CURRENT_SESSION_KEY)
      }
      if (this.floatingSessionId === id) await this.resetFloatingSession()
    },

    async updateSessionTitle(id: string, title: string) {
      const { data } = await agentApi.updateSession(id, { title })
      this.upsertSession(data)
    },

    async initFloatingSession(noteTitle = '') {
      if (this.floatingSessionId) return
      const { data } = await agentApi.createSession()
      this.upsertSession(data)
      this.floatingSessionId = data.id
      this.floatingNoteTitle = noteTitle
      localStorage.setItem(FLOATING_SESSION_KEY, data.id)
      if (noteTitle) localStorage.setItem(FLOATING_NOTE_KEY, noteTitle)
      else localStorage.removeItem(FLOATING_NOTE_KEY)
      this.floatingMessages = []
      this.floatingParts = {}
      this.setSessionSending(data.id, false)
    },

    setFloatingNoteTitle(noteTitle: string) {
      this.floatingNoteTitle = noteTitle
      if (noteTitle) localStorage.setItem(FLOATING_NOTE_KEY, noteTitle)
      else localStorage.removeItem(FLOATING_NOTE_KEY)
    },

    async sendFloatingMessage(text: string, displayText?: string, context?: string, files: AgentFilePartInput[] = []) {
      const sessionID = this.floatingSessionId
      if (!sessionID) return
      this.globalError = null
      this.clearSessionErrors(sessionID)
      this.setSessionSending(sessionID, true)
      const localId = `local_${uuidv4()}`
      const userMsg = markOptimisticMessage({
        id: localId,
        role: 'user',
        sessionID,
        time: { created: Date.now() },
      })
      const shownText = displayText ?? text
      const localParts: MessagePart[] = [
        ...(shownText ? [{ id: `local_p_${uuidv4()}`, type: 'text', messageID: localId, text: shownText }] : []),
        ...files.map((file) => ({ ...file, id: `local_p_${uuidv4()}`, messageID: localId })),
      ]
      const lastMsg = this.floatingMessages[this.floatingMessages.length - 1]
      const lastIsLocalUser = lastMsg?.role === 'user' && lastMsg.id.startsWith('local_')
      let addedLocalMessage = false
      if (!lastIsLocalUser) {
        this.floatingMessages = [...this.floatingMessages, userMsg]
        this.floatingParts = { ...this.floatingParts, [localId]: localParts }
        addedLocalMessage = true
      }
      try {
        await agentApi.sendMessage(sessionID, {
          parts: [...(text ? [{ type: 'text' as const, text }] : []), ...files],
          agent: 'build',
          system: systemContext(context),
        })
      } catch (error) {
        if (addedLocalMessage) {
          const collection = removeMessage({ messages: this.floatingMessages, parts: this.floatingParts }, localId)
          this.floatingMessages = collection.messages
          this.floatingParts = collection.parts
        }
        this.setSessionSending(sessionID, false)
        throw error
      }
    },

    async abortFloatingSession() {
      const sessionID = this.floatingSessionId
      if (!sessionID || !this.floatingSending || this.floatingStopping) return
      this.setSessionStopping(sessionID, true)
      try {
        await agentApi.abortSession(sessionID)
        this.setSessionSending(sessionID, false)
      } catch (error) {
        this.setSessionStopping(sessionID, false)
        throw error
      }
    },

    async resetFloatingSession() {
      clearAgentDraft(this.floatingSessionId)
      this.floatingSessionId = null
      this.floatingNoteTitle = ''
      localStorage.removeItem(FLOATING_SESSION_KEY)
      localStorage.removeItem(FLOATING_NOTE_KEY)
      this.floatingMessages = []
      this.floatingParts = {}
      this.floatingSending = false
      this.floatingStopping = false
    },

    async restoreFloatingSession() {
      if (this.floatingSessionId) return
      const id = localStorage.getItem(FLOATING_SESSION_KEY)
      if (!id) return
      try {
        const [sessionRes, messageRes, statusRes] = await Promise.all([
          agentApi.getSession(id),
          agentApi.getMessages(id),
          agentApi.getSessionStatuses(),
        ])
        this.floatingSessionId = id
        this.floatingNoteTitle = localStorage.getItem(FLOATING_NOTE_KEY) || ''
        if (sessionRes.data) this.upsertSession(sessionRes.data)
        const normalized = this.filterSnapshotCollection(id, normalizeMessages(messageRes.data))
        const collection = mergeMessageCollections(normalized, {
          messages: this.floatingMessages,
          parts: this.floatingParts,
        })
        this.floatingMessages = collection.messages
        this.floatingParts = collection.parts
        this.sessionStatuses = statusRes.data || {}
        this.floatingSending = isBusy(this.statusForSession(id))
        this.floatingStopping = false
      } catch {
        localStorage.removeItem(FLOATING_SESSION_KEY)
        localStorage.removeItem(FLOATING_NOTE_KEY)
        this.floatingSessionId = null
        this.floatingNoteTitle = ''
      }
    },

    async sendMessage(text: string, files: AgentFilePartInput[] = []) {
      const sessionID = this.currentSessionId
      if (!sessionID) return
      const conversation = this.ensureConversation(sessionID)
      this.globalError = null
      this.clearSessionErrors(sessionID)
      this.setSessionSending(sessionID, true)
      conversation.loaded = true
      const localId = `local_${uuidv4()}`
      const userMsg = markOptimisticMessage({
        id: localId,
        role: 'user',
        sessionID,
        time: { created: Date.now() },
      })
      const localParts: MessagePart[] = [
        ...(text ? [{ id: `local_p_${uuidv4()}`, type: 'text', messageID: localId, text }] : []),
        ...files.map((file) => ({ ...file, id: `local_p_${uuidv4()}`, messageID: localId })),
      ]
      const lastMsg = conversation.messages[conversation.messages.length - 1]
      const lastIsLocalUser = lastMsg?.role === 'user' && lastMsg.id.startsWith('local_')
      let addedLocalMessage = false
      if (!lastIsLocalUser) {
        conversation.messages = [...conversation.messages, userMsg]
        conversation.parts = { ...conversation.parts, [localId]: localParts }
        conversation.contentVersion++
        addedLocalMessage = true
      }
      try {
        await agentApi.sendMessage(sessionID, {
          parts: [...(text ? [{ type: 'text' as const, text }] : []), ...files],
          agent: 'build',
          system: systemContext(),
        })
      } catch (error) {
        if (addedLocalMessage) {
          this.setConversationCollection(sessionID, removeMessage(conversation, localId))
        }
        this.setSessionSending(sessionID, false)
        throw error
      }
    },

    async abortSession() {
      const sessionID = this.currentSessionId
      const conversation = sessionID ? this.conversations[sessionID] : undefined
      if (!sessionID || !conversation?.sending || conversation.stopping) return
      this.setSessionStopping(sessionID, true)
      try {
        await agentApi.abortSession(sessionID)
        this.setSessionSending(sessionID, false)
      } catch (error) {
        this.setSessionStopping(sessionID, false)
        throw error
      }
    },

    async respondPermission(permissionID: string, reply: 'once' | 'always' | 'reject') {
      await agentApi.respondPermission(permissionID, reply)
      for (const sessionID of Object.keys(this.permissions)) {
        this.permissions[sessionID] = (this.permissions[sessionID] || []).filter((p) => p.id !== permissionID)
      }
    },

    async respondQuestion(requestID: string, answers: string[][]) {
      await agentApi.respondQuestion(requestID, answers)
      for (const sessionID of Object.keys(this.questions)) {
        this.questions[sessionID] = (this.questions[sessionID] || []).filter((q) => q.id !== requestID)
      }
    },

    async rejectQuestion(requestID: string) {
      await agentApi.rejectQuestion(requestID)
      for (const sessionID of Object.keys(this.questions)) {
        this.questions[sessionID] = (this.questions[sessionID] || []).filter((q) => q.id !== requestID)
      }
    },

    handleEvent(event: SSEEvent) {
      const { type, properties: props } = event
      const floatId = this.floatingSessionId
      const sessionID = eventSessionID(props)
      const rootID = this.rootSessionID(sessionID) || sessionID
      const isFloat = !!floatId && rootID === floatId

      switch (type) {
        case 'server.connected':
          this.connected = true
          this.connectionState = 'connected'
          this.sessionsError = ''
          this.globalError = null
          void this.refreshRuntime()
          break
        case 'session.created':
          if (props.info) this.upsertSession(props.info as AgentSession)
          break
        case 'session.updated':
          if (props.info) this.upsertSession(props.info as AgentSession)
          break
        case 'session.deleted':
          invalidateSessionSettlement(sessionID)
          clearAgentDraft(sessionID)
          this.sessions = this.sessions.filter((s) => s.id !== sessionID)
          this.allSessions = this.allSessions.filter((session) => session.id !== sessionID)
          this.removeConversation(sessionID)
          {
            const permissions = { ...this.permissions }
            const questions = { ...this.questions }
            const statuses = { ...this.sessionStatuses }
            const errors = { ...this.sessionErrors }
            delete permissions[sessionID]
            delete questions[sessionID]
            delete statuses[sessionID]
            delete errors[sessionID]
            this.permissions = permissions
            this.questions = questions
            this.sessionStatuses = statuses
            this.sessionErrors = errors
          }
          this.clearSessionTombstones(sessionID)
          if (this.currentSessionId === sessionID) {
            this.currentSessionId = null
            localStorage.removeItem(CURRENT_SESSION_KEY)
          }
          if (this.floatingSessionId === sessionID) {
            this.floatingSessionId = null
            this.floatingNoteTitle = ''
            this.floatingMessages = []
            this.floatingParts = {}
            this.floatingSending = false
            this.floatingStopping = false
            localStorage.removeItem(FLOATING_SESSION_KEY)
            localStorage.removeItem(FLOATING_NOTE_KEY)
          }
          break
        case 'session.status':
          this.sessionStatuses = { ...this.sessionStatuses, [sessionID]: props.status }
          if (sessionID && !this.allSessions.some((session) => session.id === sessionID)) {
            void this.ensureSessionLineage(sessionID)
          }
          if (isBusy(props.status)) {
            invalidateSessionSettlement(sessionID)
            if (this.conversations[rootID]) this.conversations[rootID].sending = true
            if (isFloat) this.floatingSending = true
          } else if (sessionID) {
            const targetID = rootID || sessionID
            if (!isBusy(this.statusForSession(targetID))) void this.finalizeSession(targetID)
          }
          break
        case 'session.idle':
          this.sessionStatuses = { ...this.sessionStatuses, [sessionID]: { type: 'idle' } }
          if (sessionID && !this.allSessions.some((session) => session.id === sessionID)) {
            void this.ensureSessionLineage(sessionID)
          }
          if (sessionID && !isBusy(this.statusForSession(rootID || sessionID)))
            void this.finalizeSession(rootID || sessionID)
          break
        case 'session.error': {
          const error = (props as { error?: unknown }).error
          if (isAbortedAgentError(error)) break
          const runtimeError: AgentSessionError = {
            sessionID: sessionID || undefined,
            error,
            message: formatAgentError(error),
            detail: conciseAgentErrorDetail(error),
            time: Date.now(),
          }
          if (!sessionID) {
            this.globalError = runtimeError
            break
          }
          if (!this.allSessions.some((session) => session.id === sessionID)) {
            void this.ensureSessionLineage(sessionID)
          }
          this.sessionErrors = { ...this.sessionErrors, [sessionID]: runtimeError }
          this.sessionStatuses = { ...this.sessionStatuses, [sessionID]: { type: 'idle' } }
          const targetID = rootID || sessionID
          if (this.conversations[targetID]) {
            this.conversations[targetID].sending = false
            this.conversations[targetID].stopping = false
          }
          if (isFloat) {
            this.floatingSending = false
            this.floatingStopping = false
          }
          break
        }
        case 'message.updated': {
          const info = extractInfo(props.info)
          if (!info) break
          const targetSessionID = info.sessionID || sessionID
          if (this.messageTombstones[targetSessionID]?.[info.id]) break
          if (targetSessionID) {
            const conversation = this.ensureConversation(targetSessionID)
            this.setConversationCollection(targetSessionID, updateMessage(conversation, info))
          }
          if (isFloat) {
            const collection = updateMessage({ messages: this.floatingMessages, parts: this.floatingParts }, info)
            this.floatingMessages = collection.messages
            this.floatingParts = collection.parts
          }
          break
        }
        case 'message.part.updated': {
          const part = props.part as MessagePart
          const targetSessionID = (typeof part.sessionID === 'string' && part.sessionID) || sessionID
          if (
            this.messageTombstones[targetSessionID]?.[part.messageID] ||
            this.partTombstones[targetSessionID]?.[part.messageID]?.[part.id]
          ) {
            break
          }
          if (targetSessionID) {
            const conversation = this.ensureConversation(targetSessionID)
            this.setConversationCollection(targetSessionID, updateMessagePart(conversation, part, targetSessionID))
          }
          if (isFloat) {
            const collection = updateMessagePart(
              { messages: this.floatingMessages, parts: this.floatingParts },
              part,
              targetSessionID,
            )
            this.floatingMessages = collection.messages
            this.floatingParts = collection.parts
          }
          break
        }
        case 'message.part.delta': {
          const { messageID, partID, field, delta } = props
          const targetField = field || 'text'
          if (this.messageTombstones[sessionID]?.[messageID] || this.partTombstones[sessionID]?.[messageID]?.[partID]) {
            break
          }
          if (sessionID) {
            const conversation = this.ensureConversation(sessionID)
            this.setConversationCollection(
              sessionID,
              appendMessagePartDelta(conversation, messageID, partID, targetField, delta),
            )
          }
          if (isFloat) {
            const collection = appendMessagePartDelta(
              { messages: this.floatingMessages, parts: this.floatingParts },
              messageID,
              partID,
              targetField,
              delta,
            )
            this.floatingMessages = collection.messages
            this.floatingParts = collection.parts
          }
          break
        }
        case 'message.part.removed': {
          const messageID = props.messageID as string
          if (sessionID) {
            this.recordPartRemoval(sessionID, messageID, props.partID)
            const conversation = this.ensureConversation(sessionID)
            this.setConversationCollection(sessionID, removeMessagePart(conversation, messageID, props.partID))
          }
          if (isFloat) {
            const collection = removeMessagePart(
              { messages: this.floatingMessages, parts: this.floatingParts },
              messageID,
              props.partID,
            )
            this.floatingMessages = collection.messages
            this.floatingParts = collection.parts
          }
          break
        }
        case 'message.removed': {
          const messageID = props.messageID as string
          if (sessionID) {
            this.recordMessageRemoval(sessionID, messageID)
            const conversation = this.ensureConversation(sessionID)
            this.setConversationCollection(sessionID, removeMessage(conversation, messageID))
          }
          if (isFloat) {
            const collection = removeMessage({ messages: this.floatingMessages, parts: this.floatingParts }, messageID)
            this.floatingMessages = collection.messages
            this.floatingParts = collection.parts
          }
          break
        }
        case 'permission.asked':
        case 'permission.v2.asked': {
          const perm = normalizePermission(props)
          const existing = this.permissions[perm.sessionID] || []
          this.permissions = {
            ...this.permissions,
            [perm.sessionID]: [...existing.filter((item) => item.id !== perm.id), perm],
          }
          void this.ensureSessionLineage(perm.sessionID)
          break
        }
        case 'permission.replied':
        case 'permission.v2.replied': {
          const { sessionID, requestID } = props
          this.permissions[sessionID] = (this.permissions[sessionID] || []).filter((p) => p.id !== requestID)
          break
        }
        case 'question.asked':
        case 'question.v2.asked': {
          const q = props as QuestionRequest
          const existing = this.questions[q.sessionID] || []
          this.questions = { ...this.questions, [q.sessionID]: [...existing.filter((item) => item.id !== q.id), q] }
          void this.ensureSessionLineage(q.sessionID)
          break
        }
        case 'question.replied':
        case 'question.rejected':
        case 'question.v2.replied':
        case 'question.v2.rejected': {
          const { sessionID, requestID } = props
          this.questions[sessionID] = (this.questions[sessionID] || []).filter((q) => q.id !== requestID)
          break
        }
      }
    },

    async refreshRuntime() {
      const tasks: Promise<unknown>[] = []
      tasks.push(
        agentApi.listSessions().then(({ data }) => {
          const all = Array.isArray(data) ? data : []
          this.allSessions = all
          this.sessions = all.filter((session) => !session.parentID)
          this.sessionsLoaded = true
          this.sessionsError = ''
        }),
      )
      tasks.push(
        agentApi.getSessionStatuses().then(({ data }) => {
          this.sessionStatuses = data || {}
          for (const [id, conversation] of Object.entries(this.conversations)) {
            if (isBusy(this.statusForSession(id))) {
              invalidateSessionSettlement(id)
              conversation.sending = true
            } else if (conversation.sending) {
              void this.finalizeSession(id)
            }
          }
          if (this.floatingSessionId) {
            if (isBusy(this.statusForSession(this.floatingSessionId))) {
              invalidateSessionSettlement(this.floatingSessionId)
              this.floatingSending = true
            } else if (this.floatingSending) {
              void this.finalizeSession(this.floatingSessionId)
            }
          }
        }),
      )
      const currentSessionID = this.currentSessionId
      if (currentSessionID) {
        tasks.push(this.loadConversation(currentSessionID))
      }
      const floatingSessionID = this.floatingSessionId
      if (floatingSessionID) {
        tasks.push(
          agentApi.getMessages(floatingSessionID).then(({ data }) => {
            if (this.floatingSessionId !== floatingSessionID) return
            const normalized = this.filterSnapshotCollection(floatingSessionID, normalizeMessages(data))
            const collection = mergeMessageCollections(normalized, {
              messages: this.floatingMessages,
              parts: this.floatingParts,
            })
            this.floatingMessages = collection.messages
            this.floatingParts = collection.parts
          }),
        )
      }
      tasks.push(
        agentApi.listPermissions().then(({ data }) => {
          this.permissions = groupPermissions(data)
        }),
      )
      tasks.push(
        agentApi.listQuestions().then(({ data }) => {
          this.questions = groupQuestions(data)
        }),
      )
      await Promise.allSettled(tasks)
      await this.ensureRuntimeLineages()
    },
  },
})
