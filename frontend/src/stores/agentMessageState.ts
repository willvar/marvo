import type { MessageInfo, MessagePart } from '../sdk'

const LOCAL_ID_PREFIX = 'local_'

export interface MessageCollection {
  messages: MessageInfo[]
  parts: Record<string, MessagePart[]>
}

export function agentMessageRenderKey(message: MessageInfo) {
  return typeof message._marvoRenderKey === 'string' && message._marvoRenderKey ? message._marvoRenderKey : message.id
}

export function markOptimisticMessage(message: MessageInfo): MessageInfo {
  return { ...message, _marvoRenderKey: message.id }
}

export function adoptOptimisticUserMessage(
  collection: MessageCollection,
  info: MessageInfo,
): MessageCollection | undefined {
  if (info.role !== 'user') return
  const localIndex = findOptimisticUserIndex(collection.messages)
  if (localIndex < 0) return

  const messages = [...collection.messages]
  const optimistic = messages[localIndex]
  const localID = optimistic.id
  messages[localIndex] = {
    ...optimistic,
    ...info,
    _marvoRenderKey: agentMessageRenderKey(optimistic),
  }

  const parts = { ...collection.parts }
  if (parts[localID]) {
    parts[info.id] = parts[localID].map((part) => ({ ...part, messageID: info.id }))
    delete parts[localID]
  }
  return { messages, parts }
}

export function mergeMessageCollections(snapshot: MessageCollection, live: MessageCollection): MessageCollection {
  const reconciledLive = reconcileOptimisticUsers(snapshot, live)
  const liveMessages = new Map(reconciledLive.messages.map((message) => [message.id, message]))
  const snapshotIDs = new Set(snapshot.messages.map((message) => message.id))
  const messages = snapshot.messages.map((message) => {
    const current = liveMessages.get(message.id)
    return current ? mergeMessageInfo(message, current) : message
  })
  for (const message of reconciledLive.messages) {
    if (!snapshotIDs.has(message.id)) messages.push(message)
  }

  const parts: Record<string, MessagePart[]> = {}
  const messageIDs = new Set([...Object.keys(snapshot.parts), ...Object.keys(reconciledLive.parts)])
  for (const messageID of messageIDs) {
    const snapshotParts = snapshot.parts[messageID] || []
    const liveParts = reconciledLive.parts[messageID] || []
    const liveByID = new Map(liveParts.map((part) => [part.id, part]))
    const snapshotPartIDs = new Set(snapshotParts.map((part) => part.id))
    const merged = snapshotParts.map((part) => {
      const current = liveByID.get(part.id)
      return current ? mergeAgentMessagePart(part, current) : part
    })
    for (const part of liveParts) {
      if (!snapshotPartIDs.has(part.id) && keepLiveOnlyPart(part, snapshotParts, liveParts)) merged.push(part)
    }
    if (merged.length > 0) parts[messageID] = merged
  }
  return { messages, parts }
}

export function hasTerminalAgentResponse(collection: MessageCollection) {
  const latestUserIndex = findLatestVisibleUserIndex(collection)
  if (latestUserIndex < 0) return false
  const assistants = collection.messages.slice(latestUserIndex + 1).filter((message) => {
    return (
      message.role === 'assistant' &&
      message.mode !== 'compaction' &&
      message.agent !== 'compaction' &&
      message.summary !== true
    )
  })
  return assistants.some(
    (assistant) =>
      !!assistant.error ||
      (collection.parts[assistant.id] || []).some(
        (part) => part.type === 'step-finish' && typeof part.reason === 'string' && part.reason !== 'tool-calls',
      ),
  )
}

function reconcileOptimisticUsers(snapshot: MessageCollection, live: MessageCollection) {
  let reconciled = live
  for (const message of snapshot.messages) {
    if (reconciled.messages.some((current) => current.id === message.id)) continue
    reconciled = adoptOptimisticUserMessage(reconciled, message) || reconciled
  }
  return reconciled
}

function findOptimisticUserIndex(messages: MessageInfo[]) {
  for (let index = messages.length - 1; index >= 0; index--) {
    const candidate = messages[index]
    if (candidate.role !== 'user' || !candidate.id.startsWith(LOCAL_ID_PREFIX)) continue
    return index
  }
  return -1
}

function mergeMessageInfo(snapshot: MessageInfo, live: MessageInfo): MessageInfo {
  const merged: MessageInfo = { ...live, ...snapshot }
  if (live.time || snapshot.time) merged.time = { ...live.time, ...snapshot.time } as MessageInfo['time']
  if (live._marvoRenderKey) merged._marvoRenderKey = live._marvoRenderKey
  return merged
}

export function mergeAgentMessagePart(snapshot: MessagePart, live: MessagePart): MessagePart {
  const merged: MessagePart = { ...live, ...snapshot }
  if (typeof live.text === 'string' || typeof snapshot.text === 'string') {
    merged.text = progressiveText(live.text, snapshot.text)
  }
  if (live.state || snapshot.state) merged.state = mergePartState(live.state, snapshot.state)
  if (live.time || snapshot.time) merged.time = { ...live.time, ...snapshot.time }
  return merged
}

function findLatestVisibleUserIndex(collection: MessageCollection) {
  for (let index = collection.messages.length - 1; index >= 0; index--) {
    const message = collection.messages[index]
    if (message.role !== 'user') continue
    const visible = (collection.parts[message.id] || []).some(
      (part) =>
        part.type === 'file' ||
        (part.type === 'text' && part.synthetic !== true && typeof part.text === 'string' && part.text.length > 0),
    )
    if (visible) return index
  }
  return -1
}

function mergePartState(live: MessagePart['state'], snapshot: MessagePart['state']) {
  if (!live) return snapshot
  if (!snapshot) return live
  const liveRank = stateRank(live.status)
  const snapshotRank = stateRank(snapshot.status)
  const preferred = snapshotRank >= liveRank ? snapshot : live
  const fallback = preferred === snapshot ? live : snapshot
  const merged = { ...fallback, ...preferred }
  if (typeof live.output === 'string' || typeof snapshot.output === 'string') {
    merged.output = progressiveText(live.output, snapshot.output)
  }
  if (live.time || snapshot.time) merged.time = { ...live.time, ...snapshot.time }
  if (live.metadata || snapshot.metadata) merged.metadata = { ...live.metadata, ...snapshot.metadata }
  return merged
}

function stateRank(status?: string) {
  switch (status) {
    case 'completed':
    case 'error':
    case 'failed':
      return 2
    case 'running':
      return 1
    default:
      return 0
  }
}

function progressiveText(live?: string, snapshot?: string) {
  if (typeof live !== 'string') return snapshot || ''
  if (typeof snapshot !== 'string') return live
  if (live.startsWith(snapshot)) return live
  if (snapshot.startsWith(live)) return snapshot
  return snapshot.length >= live.length ? snapshot : live
}

function keepLiveOnlyPart(part: MessagePart, snapshotParts: MessagePart[], liveParts: MessagePart[]) {
  if (!part.id.startsWith(LOCAL_ID_PREFIX)) return true
  const materialSnapshotParts = snapshotParts.filter(
    (candidate) => candidate.type === part.type && isMaterialPart(candidate),
  )
  if (materialSnapshotParts.length === 0) return true
  const optimisticTypeIndex = liveParts.filter(
    (candidate) =>
      candidate.id.startsWith(LOCAL_ID_PREFIX) &&
      candidate.type === part.type &&
      liveParts.indexOf(candidate) <= liveParts.indexOf(part),
  ).length
  return optimisticTypeIndex > materialSnapshotParts.length
}

function isMaterialPart(part: MessagePart) {
  if (part.type === 'text' || part.type === 'reasoning') return typeof part.text === 'string' && part.text.length > 0
  return true
}
