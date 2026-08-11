import type { PermissionRequest, QuestionRequest } from '@opencode-ai/sdk/v2/client'

export interface AgentSession {
  id: string
  title: string
  parentID?: string
  time: { created: number; updated: number }
  revert?: { messageID: string; partID?: string }
  [key: string]: any
}

export interface AgentSessionError {
  sessionID?: string
  error: unknown
  message: string
  detail?: string
  time: number
}

export type AgentConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting'

export interface MessageInfo {
  id: string
  role: string
  sessionID: string
  modelID?: string
  providerID?: string
  _marvoRenderKey?: string
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

export interface AgentModelSelection {
  provider_id: string
  model_id: string
}

interface AgentModelIOCapabilities {
  text: boolean
  audio: boolean
  image: boolean
  video: boolean
  pdf: boolean
}

interface AgentModelCapabilities {
  attachment: boolean
  reasoning: boolean
  tools: boolean
  input: AgentModelIOCapabilities
  output: AgentModelIOCapabilities
}

export interface AgentModelOption {
  provider_id: string
  provider_name: string
  model_id: string
  name: string
  family?: string
  status: string
  capabilities: AgentModelCapabilities
  variants: string[]
  context_limit?: number
  output_limit?: number
}

export interface AgentSettingsResponse {
  model: AgentModelSelection | null
  variant: string
  global_prompt: string
  global_prompt_pending: boolean
  models: AgentModelOption[]
  model_available: boolean
  source: 'saved' | 'opencode' | 'none'
}

export interface AgentSettingsUpdate {
  model: AgentModelSelection | null
  variant: string
  global_prompt: string
}

export interface AgentPersonalizationRule {
  id: string
  text: string
}

export interface AgentPersonalizationResponse {
  rules: AgentPersonalizationRule[]
  revision: string
  prompt_pending: boolean
}

export interface AgentProviderPromptOption {
  label: string
  value: string
  hint?: string
}

interface AgentProviderPromptWhen {
  key: string
  op: string
  value: string
}

interface AgentProviderPrompt {
  type: 'text' | 'select'
  key: string
  message: string
  placeholder?: string
  options?: AgentProviderPromptOption[]
  when?: AgentProviderPromptWhen
}

export interface AgentProviderMethod {
  index: number
  type: 'api' | 'oauth'
  label: string
  prompts: AgentProviderPrompt[]
  available: boolean
  unavailable_reason?: string
}

export interface AgentProvider {
  id: string
  name: string
  source: string
  connected: boolean
  can_disconnect: boolean
  model_count: number
  methods: AgentProviderMethod[]
}

type AgentProviderOAuthStatus = 'pending' | 'succeeded' | 'failed' | 'expired' | 'cancelled'

export interface AgentProviderOAuthAttempt {
  id: string
  provider_id: string
  provider_name: string
  method_label: string
  mode: 'auto' | 'code'
  url: string
  instructions: string
  code?: string
  status: AgentProviderOAuthStatus
  error?: string
  created_at: number
  expires_at: number
}

export type { PermissionRequest, QuestionRequest }
