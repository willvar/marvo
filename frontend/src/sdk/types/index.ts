export interface NoteInfo {
  title: string
  tags: string[]
  created_at: string
  updated_at: string
}

export interface NoteDetail {
  note: NoteInfo
  content: string
  content_revision: string
  meta_revision: string
  instance_token: string
}

export type SearchResult = NoteInfo

type MediaAssetState =
  'reserved' | 'uploading' | 'queued' | 'probing' | 'transcoding' | 'ready' | 'abandoned' | 'failed'

export interface MediaAsset {
  id: string
  kind: 'image' | 'video'
  state: MediaAssetState
  original_name: string
  content_type?: string
  filename?: string
  url?: string
  error?: string
  created_at?: string
  updated_at?: string
}

export interface TrashEntry {
  id: string
  title: string
  tags: string[]
  deleted_at: string
}

type ActivityKind = 'notice' | 'choice'

export interface ActivityItem {
  id: string
  kind: ActivityKind
  title: string
  content: string
  choices: string[]
  multiple: boolean
  created_at: string
  read_at: string | null
  responded_at: string | null
  response_text?: string
  response_choices?: string[]
  reply_session_id?: string
  replying: boolean
}

export interface ActivityPage {
  activities: ActivityItem[]
  next_cursor?: string
  unread: number
  pending: number
}

export interface ActivityCounts {
  unread: number
  pending: number
}

type ScheduleKind = 'at' | 'every' | 'cron' | 'adaptive'
type ScheduleStatus = 'active' | 'paused' | 'completed'
type ScheduleRunStatus = 'queued' | 'running' | 'waiting_retry' | 'succeeded' | 'failed' | 'timed_out' | 'cancelled'

interface ScheduleSpec {
  at?: string
  every_seconds?: number
  anchor?: string
  expression?: string
  minimum_seconds?: number
  maximum_seconds?: number
  default_seconds?: number
}

export interface ScheduleDefinition {
  kind: ScheduleKind
  spec: ScheduleSpec
  timezone?: string
}

export interface ScheduleRun {
  id: string
  schedule_id: string
  schedule_revision: number
  trigger: 'scheduled' | 'manual'
  scheduled_for: string
  status: ScheduleRunStatus
  attempt: number
  next_attempt_at?: string
  session_id?: string
  message_id?: string
  error?: string
  created_at: string
  started_at?: string
  finished_at?: string
  updated_at: string
}

export interface AutomaticTask {
  id: string
  name: string
  instruction: string
  schedule: ScheduleDefinition
  status: ScheduleStatus
  next_run_at: string | null
  session_id?: string
  revision: number
  consecutive_failures: number
  last_error?: string
  last_run_at: string | null
  paused_reason?: string
  created_at: string
  updated_at: string
  active_run?: ScheduleRun
}

export interface ScheduleInput {
  name: string
  instruction: string
  schedule: ScheduleDefinition
}
