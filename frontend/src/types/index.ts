export interface NoteInfo {
  title: string
  tags: string[]
  created_at: string
  updated_at: string
}

export interface NoteDetail {
  note: NoteInfo
  content: string
  version: number
}

export interface SearchResult {
  title: string
  content: string
  score: number
}
