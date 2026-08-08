export { api, ApiError } from './api/useApi'
export { agentApi, createSSEConnection, type AgentFilePartInput, type SSEEvent } from './api/agentApi'
export { setUnauthorizedHandler } from './api/unauthorized'
export { connect, disconnect, on, subscribe, unsubscribe } from './collab'
export { type NoteInfo, type NoteDetail, type SearchResult, type MediaAsset, type TrashEntry } from './types'
export {
  type AgentSession,
  type AgentSessionError,
  type AgentConnectionState,
  type MessageInfo,
  type MessagePart,
  type PermissionRequest,
  type QuestionRequest,
  type AgentModelSelection,
  type AgentModelOption,
  type AgentSettingsResponse,
  type AgentSettingsUpdate,
} from './types/agent'
export { conciseAgentErrorDetail, formatAgentError, isAbortedAgentError, unwrapAgentError } from './agentErrors'
export { toMarkdownAssetPath, toNoteAssetUrl, toRelativeAssetPath } from './utils/noteAssets'
export { renderMarkdown } from './markdown'
export { clearAgentDraft, loadAgentDraft, saveAgentDraft } from './agentDrafts'
export { currentDraftId, getDraft, listBranchDrafts, removeDraft, saveDraft, type NoteDraft } from './drafts'
export { resolveMerge, threeWayMerge } from './merge'
export { prepareNoteForAgent, registerEditorPreparation } from './editorCoordinator'
export {
  DEFAULT_ACCENT_COLOR,
  DEFAULT_CONTENT_FONT_SIZE,
  DEFAULT_FONT_FAMILY,
  DEFAULT_FONT_SIZE,
  normalizeTheme,
  type ThemeFile,
} from './theme'
