import type { SessionStatus } from '@opencode-ai/sdk/v2/client'
import type { AgentSession } from '../sdk'

export type AgentRequestMap<T> = Record<string, T[] | undefined>

export function agentSessionTreeIDs(sessions: AgentSession[], sessionID?: string | null) {
  if (!sessionID) return []
  const children = new Map<string, string[]>()
  for (const session of sessions) {
    if (!session.parentID) continue
    const current = children.get(session.parentID) || []
    current.push(session.id)
    children.set(session.parentID, current)
  }

  const result = [sessionID]
  const seen = new Set(result)
  for (const id of result) {
    for (const child of children.get(id) || []) {
      if (seen.has(child)) continue
      seen.add(child)
      result.push(child)
    }
  }
  return result
}

export function agentRootSessionID(sessions: AgentSession[], sessionID?: string | null) {
  if (!sessionID) return ''
  const byID = new Map(sessions.map((session) => [session.id, session]))
  let id = sessionID
  const seen = new Set<string>()
  while (!seen.has(id)) {
    seen.add(id)
    const parentID = byID.get(id)?.parentID
    if (!parentID) return id
    id = parentID
  }
  return sessionID
}

export function agentSessionTreeRequest<T extends { sessionID: string }>(
  sessions: AgentSession[],
  requests: AgentRequestMap<T>,
  sessionID?: string | null,
) {
  for (const id of agentSessionTreeIDs(sessions, sessionID)) {
    const request = requests[id]?.[0]
    if (request) return request
  }
}

export function agentSessionTreeStatus(
  sessions: AgentSession[],
  statuses: Record<string, SessionStatus | undefined>,
  sessionID?: string | null,
): SessionStatus | undefined {
  const values = agentSessionTreeIDs(sessions, sessionID)
    .map((id) => statuses[id])
    .filter(Boolean) as SessionStatus[]
  return (
    values.find((status) => status.type === 'retry') || values.find((status) => status.type === 'busy') || values[0]
  )
}

export function agentSessionTreeValue<T>(
  sessions: AgentSession[],
  values: Record<string, T | undefined>,
  sessionID?: string | null,
) {
  for (const id of agentSessionTreeIDs(sessions, sessionID)) {
    const value = values[id]
    if (value !== undefined) return value
  }
}
