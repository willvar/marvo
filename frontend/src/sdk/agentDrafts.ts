const AGENT_DRAFT_PREFIX = 'marvo.agent.draft.'

function storageKey(sessionID: string) {
  return `${AGENT_DRAFT_PREFIX}${encodeURIComponent(sessionID)}`
}

export function loadAgentDraft(sessionID?: string | null) {
  if (!sessionID || typeof localStorage === 'undefined') return ''
  try {
    return localStorage.getItem(storageKey(sessionID)) || ''
  } catch {
    return ''
  }
}

export function saveAgentDraft(sessionID: string | null | undefined, text: string) {
  if (!sessionID || typeof localStorage === 'undefined') return
  try {
    if (text) localStorage.setItem(storageKey(sessionID), text)
    else localStorage.removeItem(storageKey(sessionID))
  } catch {
    // A draft is a convenience only; storage failures must not block sending.
  }
}

export function clearAgentDraft(sessionID?: string | null) {
  saveAgentDraft(sessionID, '')
}
