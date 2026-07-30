import { create } from 'zustand'
import api from '../api/useApi'
import { connect, on, send, subscribe, unsubscribe, getClientId } from '../hooks/useWebSocket'
import type { NoteInfo, NoteDetail, SearchResult } from '../types'

interface NoteState {
  notes: NoteInfo[]
  currentNote: NoteDetail | null
  searchResults: SearchResult[]
  loading: boolean
  version: number

  fetchNotes: () => Promise<void>
  getNote: (title: string) => Promise<NoteDetail>
  createNote: (title: string, tags?: string[]) => Promise<NoteDetail>
  sendSteps: (title: string, version: number, steps: any[], content: string) => void
  subscribeNote: (title: string) => void
  unsubscribeNote: (title: string) => void
  updateMeta: (title: string, tags: string[]) => Promise<void>
  deleteNote: (title: string) => Promise<void>
  search: (query: string, limit?: number) => Promise<void>
  uploadAttachment: (title: string, file: File) => Promise<any>
  renameNote: (oldTitle: string, newTitle: string) => Promise<void>
  getOrCreateNote: (title: string) => Promise<NoteDetail>
}

export const useNoteStore = create<NoteState>((set, get) => {
  on('ot_snapshot', (msg: any) => {
    const current = get().currentNote
    if (current && current.note.title === msg.title) {
      set({
        currentNote: { ...current, content: msg.content },
        version: msg.version,
      })
    }
  })

  on('ot_steps', (msg: any) => {
    const current = get().currentNote
    if (current && current.note.title === msg.title) {
      set({
        currentNote: msg.content ? { ...current, content: msg.content, version: msg.new_version } : current,
        version: msg.new_version,
      })
    }
  })

  on('ot_rebase', (msg: any) => {
    const current = get().currentNote
    if (current && current.note.title === msg.title) {
      set({ version: msg.new_version })
    }
  })

  on('subscribed', (msg: any) => {
    set({ version: msg.version })
  })

  return {
    notes: [],
    currentNote: null,
    searchResults: [],
    loading: false,
    version: 0,

    fetchNotes: async () => {
      set({ loading: true })
      try {
        const { data } = await api.get('/api/notes')
        set({ notes: data })
      } finally {
        set({ loading: false })
      }
    },

    getNote: async (title: string) => {
      set({ loading: true })
      try {
        const { data } = await api.get(`/api/notes/${encodeURIComponent(title)}`)
        set({ currentNote: data, version: data.version ?? 0 })
        return data
      } finally {
        set({ loading: false })
      }
    },

    createNote: async (title: string, tags: string[] = []) => {
      const { data } = await api.post('/api/notes', { title, tags })
      set({ currentNote: data, version: data.version ?? 0 })
      await get().fetchNotes()
      return data
    },

    sendSteps: (title: string, version: number, steps: any[], content: string) => {
      send({
        action: 'ot_steps',
        title,
        client_id: getClientId(),
        data: {
          version,
          steps,
          content,
        },
      })
    },

    subscribeNote: (title: string) => {
      subscribe(title)
    },

    unsubscribeNote: (title: string) => {
      unsubscribe(title)
    },

    updateMeta: async (title: string, tags: string[]) => {
      await api.put(`/api/notes/${encodeURIComponent(title)}/meta`, { tags })
    },

    deleteNote: async (title: string) => {
      await api.delete(`/api/notes/${encodeURIComponent(title)}`)
      const current = get().currentNote
      if (current?.note.title === title) {
        set({ currentNote: null })
      }
      await get().fetchNotes()
    },

    search: async (query: string, limit = 20) => {
      if (!query) {
        set({ searchResults: [] })
        return
      }
      const { data } = await api.get('/api/search', { params: { q: query, limit } })
      set({ searchResults: data.results })
    },

    uploadAttachment: async (title: string, file: File) => {
      const form = new FormData()
      form.append('file', file)
      const { data } = await api.post(
        `/api/notes/${encodeURIComponent(title)}/assets`,
        form,
      )
      return data
    },

    renameNote: async (oldTitle: string, newTitle: string) => {
      await api.put(
        `/api/notes/${encodeURIComponent(oldTitle)}/rename`,
        { new_title: newTitle },
      )
      await get().fetchNotes()
    },

    getOrCreateNote: async (title: string) => {
      try {
        return await get().getNote(title)
      } catch {
        return await get().createNote(title)
      }
    },
  }
})

connect()
