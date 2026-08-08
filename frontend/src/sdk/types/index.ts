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
