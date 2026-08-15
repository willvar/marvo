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
