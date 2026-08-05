import type { SessionStatus } from '@opencode-ai/sdk/v2/client'
import {
  conciseAgentErrorDetail,
  formatAgentError,
  isAbortedAgentError,
  unwrapAgentError,
  type MessageInfo,
  type MessagePart,
} from '../sdk'
import { agentMessageRenderKey } from '../stores/agentMessageState'
import type { XThoughtItem } from './x'
import {
  buildExecutionThoughtChainFromParts,
  formatAgentExecutionDuration,
  isAgentExecutionPart,
} from './agentExecution'

interface AgentUserTimelineItem {
  key: string
  role: 'user'
  text: string
  files: MessagePart[]
  created?: number
  streaming: false
  _signature: string
}

interface AgentTextSegment {
  key: string
  type: 'text'
  text: string
  final: boolean
  streaming: boolean
  _signature: string
}

interface AgentReasoningSegment {
  key: string
  type: 'reasoning'
  text: string
  heading: string
  streaming: boolean
  _signature: string
}

interface AgentActionSegment {
  key: string
  type: 'action'
  items: XThoughtItem[]
  _signature: string
}

interface AgentFilesSegment {
  key: string
  type: 'files'
  files: MessagePart[]
  _signature: string
}

interface AgentErrorSegment {
  key: string
  type: 'error'
  text: string
  detail: string
  _signature: string
}

export interface AgentQuestionAnswerItem {
  question: string
  answers: string[]
}

interface AgentQuestionSegment {
  key: string
  type: 'question'
  status: 'answered' | 'dismissed' | 'failed'
  items: AgentQuestionAnswerItem[]
  message?: string
  _signature: string
}

interface AgentStoppedSegment {
  key: string
  type: 'stopped'
  _signature: string
}

interface AgentThinkingSegment {
  key: string
  type: 'thinking'
  heading: string
  _signature: string
}

interface AgentRetrySegment {
  key: string
  type: 'retry'
  attempt: number
  message: string
  detail: string
  action?: Extract<SessionStatus, { type: 'retry' }>['action']
  next: number
  _signature: string
}

type AgentAssistantSegment =
  | AgentTextSegment
  | AgentReasoningSegment
  | AgentActionSegment
  | AgentFilesSegment
  | AgentErrorSegment
  | AgentQuestionSegment
  | AgentStoppedSegment
  | AgentThinkingSegment
  | AgentRetrySegment

interface AgentAssistantTimelineItem {
  key: string
  role: 'assistant'
  segments: AgentAssistantSegment[]
  copyText: string
  created?: number
  streaming: boolean
  _signature: string
}

interface AgentDividerTimelineItem {
  key: string
  role: 'divider'
  label: string
  streaming: false
  _signature: string
}

export type AgentTimelineItem = AgentUserTimelineItem | AgentAssistantTimelineItem | AgentDividerTimelineItem

interface VisibleMessage {
  message: MessageInfo
  parts: MessagePart[]
}

interface TimelineTurn {
  key: string
  user?: VisibleMessage
  assistants: VisibleMessage[]
}

interface RawTextSegment {
  key: string
  type: 'text'
  text: string
}

interface RawReasoningSegment {
  key: string
  type: 'reasoning'
  parts: MessagePart[]
}

interface RawActionSegment {
  key: string
  type: 'action'
  parts: MessagePart[]
}

interface RawFilesSegment {
  key: string
  type: 'files'
  files: MessagePart[]
}

interface RawErrorSegment {
  key: string
  type: 'error'
  error: unknown
}

interface RawStoppedSegment {
  key: string
  type: 'stopped'
}

interface RawQuestionSegment {
  key: string
  type: 'question'
  part: MessagePart
}

type RawAssistantSegment =
  | RawTextSegment
  | RawReasoningSegment
  | RawActionSegment
  | RawFilesSegment
  | RawErrorSegment
  | RawStoppedSegment
  | RawQuestionSegment

type ActivePhase = 'reasoning' | 'action' | 'text' | 'files' | 'none'

export function buildAgentTimeline(
  messages: MessageInfo[],
  partsByMessage: Record<string, MessagePart[]>,
  options: { running: boolean; unsettled?: boolean; status?: SessionStatus },
): AgentTimelineItem[] {
  const turns = constructTurns(messages, partsByMessage)
  const unsettled = options.unsettled ?? options.running
  if (turns.length === 0 && unsettled) turns.push({ key: 'active', assistants: [] })
  const activeTurn = unsettled ? turns[turns.length - 1] : undefined
  const items: AgentTimelineItem[] = []

  for (const turn of turns) {
    if (turn.user) items.push(buildUserItem(turn.user))
    const assistant = buildAssistantItem(
      turn,
      turn === activeTurn && options.running,
      turn === activeTurn && unsettled,
      options.status,
    )
    if (assistant) items.push(...splitInterruptedAssistant(assistant))
  }
  return items
}

function splitInterruptedAssistant(item: AgentAssistantTimelineItem): AgentTimelineItem[] {
  if (!item.segments.some((segment) => segment.type === 'stopped')) return [item]

  const items: AgentTimelineItem[] = []
  let segments: AgentAssistantSegment[] = []
  let groupIndex = 0

  const flushAssistant = () => {
    if (segments.length === 0) return
    const key = groupIndex === 0 ? item.key : `${item.key}-continuation-${groupIndex}`
    const copyText = item.streaming ? '' : assistantCopyText(segments)
    items.push({
      ...item,
      key,
      segments,
      copyText,
      _signature: [
        'assistant',
        item.created || '',
        item.streaming ? 'streaming' : 'complete',
        ...segments.map(signature),
      ].join('|'),
    })
    segments = []
    groupIndex++
  }

  for (const segment of item.segments) {
    if (segment.type !== 'stopped') {
      segments.push(segment)
      continue
    }
    flushAssistant()
    items.push({
      key: segment.key,
      role: 'divider',
      label: '已中断',
      streaming: false,
      _signature: 'divider|已中断',
    })
  }
  flushAssistant()
  return items
}

function constructTurns(messages: MessageInfo[], partsByMessage: Record<string, MessagePart[]>): TimelineTurn[] {
  const turns: TimelineTurn[] = []
  const turnByMessageID = new Map<string, TimelineTurn>()
  let latestVisibleTurn: TimelineTurn | undefined
  let orphanTurn: TimelineTurn | undefined

  for (const message of messages) {
    const parts = visibleParts(message, partsByMessage[message.id] || [])
    if (message.role === 'user') {
      if (!parts.some((part) => part.type === 'text' || part.type === 'file')) {
        if (latestVisibleTurn) turnByMessageID.set(message.id, latestVisibleTurn)
        continue
      }
      const turn: TimelineTurn = { key: agentMessageRenderKey(message), user: { message, parts }, assistants: [] }
      turns.push(turn)
      turnByMessageID.set(message.id, turn)
      latestVisibleTurn = turn
      orphanTurn = undefined
      continue
    }

    if (message.role !== 'assistant' || isInternalAssistant(message)) continue
    const parentID = typeof message.parentID === 'string' ? message.parentID : ''
    let turn = (parentID && turnByMessageID.get(parentID)) || latestVisibleTurn
    if (!turn) {
      orphanTurn ||= { key: `orphan-${parentID || message.id}`, assistants: [] }
      if (!turns.includes(orphanTurn)) turns.push(orphanTurn)
      turn = orphanTurn
    }
    turn.assistants.push({ message, parts })
    turnByMessageID.set(message.id, turn)
  }

  return turns
}

function buildUserItem(entry: VisibleMessage): AgentUserTimelineItem {
  const text = textContent(entry.parts)
  const files = entry.parts.filter((part) => part.type === 'file')
  const created = entry.message.time?.created
  return {
    key: agentMessageRenderKey(entry.message),
    role: 'user',
    text,
    files,
    created,
    streaming: false,
    _signature: ['user', created || '', text, fileSignature(files)].join('|'),
  }
}

function buildAssistantItem(
  turn: TimelineTurn,
  active: boolean,
  unsettled: boolean,
  status?: SessionStatus,
): AgentAssistantTimelineItem | undefined {
  const raw: RawAssistantSegment[] = []
  let activePhase: ActivePhase = 'none'

  for (const entry of turn.assistants) {
    for (const part of entry.parts) {
      if (part.type === 'reasoning') {
        if (part.text) appendReasoning(raw, part)
        activePhase = 'reasoning'
        continue
      }
      if (part.type === 'text') {
        if (!part.text) continue
        appendText(raw, part)
        activePhase = 'text'
        continue
      }
      if (part.type === 'file') {
        appendFiles(raw, part)
        activePhase = 'files'
        continue
      }
      if (isQuestionToolPart(part)) {
        const toolStatus = part.state?.status
        if (toolStatus !== 'pending' && toolStatus !== 'running') {
          raw.push({ key: part.callID || part.id, type: 'question', part })
          activePhase = 'none'
        }
        continue
      }
      if (isAgentExecutionPart(part) && part.type !== 'retry') {
        appendAction(raw, part)
        activePhase = 'action'
      }
    }

    if (entry.message.error) {
      if (isAbortedAgentError(entry.message.error)) raw.push({ key: `${entry.message.id}-stopped`, type: 'stopped' })
      else {
        for (let index = raw.length - 1; index >= 0; index--) {
          if (raw[index].type === 'error') raw.splice(index, 1)
        }
        raw.push({ key: `${entry.message.id}-error`, type: 'error', error: entry.message.error })
      }
      activePhase = 'none'
    }
  }

  const duration = assistantDuration(turn.assistants)
  const actionCount = raw.filter((segment) => segment.type === 'action').length
  const lastRaw = raw[raw.length - 1]
  const segments: AgentAssistantSegment[] = []

  for (const [rawIndex, segment] of raw.entries()) {
    if (segment.type === 'reasoning') {
      const text = segment.parts
        .map((part) => part.text || '')
        .filter(Boolean)
        .join('\n')
      const heading = [...segment.parts]
        .reverse()
        .map((part) => extractReasoningHeading(part.text || ''))
        .find(Boolean)
      const streaming = active && status?.type !== 'retry' && activePhase === 'reasoning' && segment === lastRaw
      segments.push({
        key: segment.key,
        type: 'reasoning',
        text,
        heading: heading || '',
        streaming,
        _signature: ['reasoning', text, heading || '', streaming ? 'streaming' : 'complete'].join('|'),
      })
      continue
    }
    if (segment.type === 'text') {
      const streaming = active && status?.type !== 'retry' && activePhase === 'text' && segment === lastRaw
      segments.push({
        ...segment,
        final: false,
        streaming,
        _signature: ['text', segment.text, streaming ? 'streaming' : 'complete'].join('|'),
      })
      continue
    }
    if (segment.type === 'action') {
      const streaming = active && status?.type !== 'retry' && activePhase === 'action' && segment === lastRaw
      const items = buildExecutionThoughtChainFromParts(
        segment.parts,
        segment.key,
        executionOutcome(raw, rawIndex, streaming),
        actionCount === 1 ? duration : '',
      )
      if (items.length > 0) {
        segments.push({
          key: segment.key,
          type: 'action',
          items,
          _signature: `action|${JSON.stringify(items)}`,
        })
      }
      continue
    }
    if (segment.type === 'files') {
      segments.push({ ...segment, _signature: `files|${fileSignature(segment.files)}` })
      continue
    }
    if (segment.type === 'stopped') {
      segments.push({ ...segment, _signature: 'stopped' })
      continue
    }
    if (segment.type === 'question') {
      const question = questionResult(segment.part)
      segments.push({
        key: segment.key,
        type: 'question',
        ...question,
        _signature: `question|${question.status}|${JSON.stringify(question.items)}|${question.message || ''}`,
      })
      continue
    }
    const text = formatAgentError(segment.error)
    const detail = conciseAgentErrorDetail(segment.error)
    segments.push({ key: segment.key, type: 'error', text, detail, _signature: `error|${text}|${detail}` })
  }

  const lastText = [...segments].reverse().find((segment): segment is AgentTextSegment => segment.type === 'text')
  const finalText = !active || (status?.type !== 'retry' && activePhase === 'text') ? lastText : undefined
  if (finalText) {
    finalText.final = true
    finalText._signature += '|final'
  }

  if (active && status?.type === 'retry') {
    const message = formatAgentError(status.message)
    const detail = conciseAgentErrorDetail(status.message)
    segments.push({
      key: `${turn.key}-retry`,
      type: 'retry',
      attempt: status.attempt,
      message,
      detail,
      action: status.action,
      next: status.next,
      _signature: ['retry', status.attempt, status.next, message, detail, JSON.stringify(status.action || null)].join(
        '|',
      ),
    })
  } else if (
    active &&
    ((activePhase === 'reasoning' && lastRaw?.type !== 'reasoning') ||
      (activePhase !== 'action' && activePhase !== 'text' && activePhase !== 'reasoning'))
  ) {
    segments.push({
      key: `${turn.key}-thinking`,
      type: 'thinking',
      heading: '',
      _signature: 'thinking',
    })
  }

  if (segments.length === 0) return undefined
  const created = turn.assistants[turn.assistants.length - 1]?.message.time?.created ?? turn.user?.message.time?.created
  const streaming = unsettled
  const copyText = unsettled ? '' : assistantCopyText(segments)
  return {
    key: `${turn.key}-assistant`,
    role: 'assistant',
    segments,
    copyText,
    created,
    streaming,
    _signature: ['assistant', created || '', streaming ? 'streaming' : 'complete', ...segments.map(signature)].join(
      '|',
    ),
  }
}

function assistantCopyText(segments: AgentAssistantSegment[]) {
  return segments
    .map((segment) => {
      if (segment.type === 'text') return segment.text.trim()
      if (segment.type !== 'question') return ''
      if (segment.status === 'dismissed') return '已取消提问'
      if (segment.status === 'failed') return '提问未完成'
      return segment.items
        .map((item) => {
          const answers = item.answers.map((answer) => answer.trim()).filter(Boolean)
          return `问题：${item.question.trim()}\n回答：${answers.join('、') || '未填写'}`
        })
        .filter(Boolean)
        .join('\n\n')
    })
    .filter(Boolean)
    .join('\n\n')
}

function appendText(segments: RawAssistantSegment[], part: MessagePart) {
  const previous = segments[segments.length - 1]
  const text = part.text || ''
  if (previous?.type === 'text') {
    previous.text = joinText(previous.text, text)
    return
  }
  segments.push({ key: part.id || `${part.messageID}-text`, type: 'text', text })
}

function appendReasoning(segments: RawAssistantSegment[], part: MessagePart) {
  const previous = segments[segments.length - 1]
  if (previous?.type === 'reasoning') {
    previous.parts.push(part)
    return
  }
  segments.push({ key: part.id || `${part.messageID}-reasoning`, type: 'reasoning', parts: [part] })
}

function appendAction(segments: RawAssistantSegment[], part: MessagePart) {
  const previous = segments[segments.length - 1]
  if (previous?.type === 'action') {
    previous.parts.push(part)
    return
  }
  segments.push({ key: part.callID || part.id || `${part.messageID}-action`, type: 'action', parts: [part] })
}

function appendFiles(segments: RawAssistantSegment[], part: MessagePart) {
  const previous = segments[segments.length - 1]
  if (previous?.type === 'files') {
    previous.files.push(part)
    return
  }
  segments.push({ key: part.id || `${part.messageID}-files`, type: 'files', files: [part] })
}

function visibleParts(message: MessageInfo, parts: MessagePart[]) {
  if (isInternalAssistant(message)) return []
  return parts.filter(
    (part) => part.type !== 'compaction' && part.ignored !== true && !(part.type === 'text' && part.synthetic === true),
  )
}

function isInternalAssistant(message: MessageInfo) {
  return (
    message.role === 'assistant' &&
    (message.mode === 'compaction' || message.agent === 'compaction' || message.summary === true)
  )
}

function executionOutcome(segments: RawAssistantSegment[], index: number, streaming: boolean) {
  if (streaming) return { streaming: true }
  for (const segment of segments.slice(index + 1)) {
    if (segment.type === 'files' || segment.type === 'question') continue
    if (segment.type === 'error') return { streaming: false, failed: true }
    if (segment.type === 'stopped') return { streaming: false, aborted: true }
    break
  }
  return { streaming: false }
}

function isQuestionToolPart(part: MessagePart) {
  return (
    ['tool', 'tool_use', 'tool_result'].includes(part.type) &&
    String(part.tool || part.name || '').toLowerCase() === 'question'
  )
}

function questionResult(part: MessagePart): {
  status: AgentQuestionSegment['status']
  items: AgentQuestionAnswerItem[]
  message?: string
} {
  const state = typeof part.state === 'object' && part.state ? part.state : { status: '' }
  const rawInput = state.input ?? part.input
  const rawMetadata = state.metadata ?? part.metadata
  const input = typeof rawInput === 'object' && rawInput ? rawInput : {}
  const metadata = typeof rawMetadata === 'object' && rawMetadata ? rawMetadata : {}
  const questions = Array.isArray((input as Record<string, unknown>).questions)
    ? ((input as Record<string, unknown>).questions as Array<Record<string, unknown>>)
    : []
  const answers = Array.isArray((metadata as Record<string, unknown>).answers)
    ? ((metadata as Record<string, unknown>).answers as unknown[])
    : []
  const items = questions.map((question, index) => ({
    question: typeof question.question === 'string' ? question.question : '智能体提问',
    answers: Array.isArray(answers[index])
      ? (answers[index] as unknown[]).filter((answer): answer is string => typeof answer === 'string')
      : [],
  }))
  if (state.status === 'error') {
    const error = unwrapAgentError(state.error)
    if (/dismissed this question|question dismissed|rejected/i.test(error)) return { status: 'dismissed', items }
    return { status: 'failed', items, message: formatAgentError(state.error) }
  }
  return { status: 'answered', items }
}

function extractReasoningHeading(text: string) {
  const markdown = text.replace(/\r\n?/g, '\n')
  const candidates = [
    markdown.match(/<h[1-6][^>]*>([\s\S]*?)<\/h[1-6]>/i)?.[1]?.replace(/<[^>]+>/g, ' '),
    markdown.match(/^\s{0,3}#{1,6}[ \t]+(.+?)(?:[ \t]+#+[ \t]*)?$/m)?.[1],
    markdown.match(/^([^\n]+)\n(?:=+|-+)\s*$/m)?.[1],
    markdown.match(/^\s*(?:\*\*|__)(.+?)(?:\*\*|__)\s*$/m)?.[1],
  ]
  return cleanHeading(candidates.find((value) => !!value) || '')
}

function cleanHeading(value: string) {
  const text = value
    .replace(/`([^`]+)`/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/[*_~]+/g, '')
    .replace(/\s+/g, ' ')
    .trim()
  return text.length > 72 ? `${text.slice(0, 71)}…` : text
}

function assistantDuration(entries: VisibleMessage[]) {
  const created = entries[0]?.message.time?.created
  const completed = entries[entries.length - 1]?.message.time?.completed
  if (typeof created !== 'number' || typeof completed !== 'number' || completed < created) return ''
  return formatAgentExecutionDuration(completed - created)
}

function textContent(parts: MessagePart[]) {
  return parts
    .filter((part) => part.type === 'text')
    .map((part) => part.text || '')
    .filter(Boolean)
    .join('\n')
}

function joinText(left: string, right: string) {
  if (!left) return right
  if (!right) return left
  return `${left}\n${right}`
}

function fileSignature(files: MessagePart[]) {
  return files
    .map((part) =>
      [part.id, part.filename || '', part.mime || '', typeof part.url === 'string' ? part.url.length : 0].join(':'),
    )
    .join(',')
}

function signature(segment: AgentAssistantSegment) {
  return `${segment.key}:${segment._signature}`
}

export function reconcileAgentTimeline(previous: AgentTimelineItem[], next: AgentTimelineItem[]) {
  const previousByKey = new Map(previous.map((item) => [item.key, item]))
  return next.map((item) => {
    const current = previousByKey.get(item.key)
    if (!current || current.role !== item.role) return item
    if (item.role !== 'assistant' || current.role !== 'assistant') {
      return current._signature === item._signature ? current : item
    }

    const segmentsByKey = new Map(current.segments.map((segment) => [segment.key, segment]))
    const segments = item.segments.map((segment) => {
      const existing = segmentsByKey.get(segment.key)
      return existing?._signature === segment._signature ? existing : segment
    })
    const reconciled = { ...item, segments }
    return current._signature === reconciled._signature ? current : reconciled
  })
}
