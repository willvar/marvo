import { defineStore } from 'pinia'
import {
  ApiError,
  api,
  on,
  subscribe,
  unsubscribe,
  type NoteDetail,
  type NoteInfo,
  type MediaAsset,
  type TrashEntry,
  type SearchResult,
} from '../sdk'

export const useNoteStore = defineStore('note', {
  state: () => ({
    notes: [] as NoteInfo[],
    currentNote: null as NoteDetail | null,
    latestRemote: null as NoteDetail | null,
    searchResults: [] as SearchResult[],
    mediaAssets: {} as Record<string, MediaAsset>,
    trash: [] as TrashEntry[],
    loading: false,
  }),

  actions: {
    async fetchNotes() {
      this.loading = true
      try {
        const { data } = await api.get('/api/notes')
        this.notes = Array.isArray(data) ? data : []
      } finally {
        this.loading = false
      }
    },

    async getNote(title: string) {
      this.loading = true
      try {
        const { data } = await api.get(`/api/notes/${encodeURIComponent(title)}`)
        this.currentNote = data as NoteDetail
        this.latestRemote = data as NoteDetail
        this.mediaAssets = {}
        return data as NoteDetail
      } finally {
        this.loading = false
      }
    },

    async createNote(title: string, tags: string[] = [], content = '') {
      const { data } = await api.post('/api/notes', { title, tags, content })
      this.currentNote = data as NoteDetail
      this.latestRemote = data as NoteDetail
      await this.fetchNotes()
      return data as NoteDetail
    },

    subscribeNote(title: string) {
      subscribe(title)
    },
    unsubscribeNote(title: string) {
      unsubscribe(title)
    },

    async updateContent(title: string, content: string, base: NoteDetail) {
      const { data } = await api.put(`/api/notes/${encodeURIComponent(title)}/content`, {
        content,
        base_revision: base.content_revision,
        instance_token: base.instance_token,
      })
      this.currentNote = data as NoteDetail
      this.latestRemote = data as NoteDetail
      return data as NoteDetail
    },

    async updateMeta(title: string, patch: { tags: string[] }, base?: NoteDetail) {
      const snapshot = base || this.currentNote
      if (!snapshot) throw new Error('note is not loaded')
      const { data } = await api.put(`/api/notes/${encodeURIComponent(title)}/meta`, {
        ...patch,
        base_revision: snapshot.meta_revision,
        instance_token: snapshot.instance_token,
      })
      this.currentNote = data as NoteDetail
      this.latestRemote = data as NoteDetail
      await this.fetchNotes()
      return data as NoteDetail
    },

    async deleteNote(title: string, instanceToken?: string) {
      const token = instanceToken || this.currentNote?.instance_token
      if (!token) throw new Error('note instance is not loaded')
      await api.delete(`/api/notes/${encodeURIComponent(title)}`, { instance_token: token })
      if (this.currentNote?.instance_token === token) this.currentNote = null
      await this.fetchNotes()
    },

    async search(query: string) {
      if (!query.trim()) {
        this.searchResults = []
        return []
      }
      const { data } = await api.get('/api/search', { params: { q: query.trim() } })
      this.searchResults = Array.isArray(data) ? data : []
      return this.searchResults
    },

    trackMediaAsset(asset: MediaAsset) {
      this.mediaAssets[asset.id] = asset
    },

    async fetchMediaAssets(title: string, instanceToken: string) {
      const { data } = await api.get(`/api/notes/${encodeURIComponent(title)}/assets`, {
        params: { instance_token: instanceToken },
      })
      const next: Record<string, MediaAsset> = {}
      if (Array.isArray(data)) {
        for (const asset of data as MediaAsset[]) next[asset.id] = asset
      }
      this.mediaAssets = next
      return Object.values(next)
    },

    async reserveMediaAsset(title: string, instanceToken: string, assetID: string, file: File) {
      const { data } = await api.post(`/api/notes/${encodeURIComponent(title)}/assets/reserve`, {
        asset_id: assetID,
        original_name: file.name,
        content_type: file.type,
        instance_token: instanceToken,
      })
      this.trackMediaAsset(data as MediaAsset)
      return data as MediaAsset
    },

    async uploadMediaAsset(title: string, instanceToken: string, assetID: string, file: File) {
      const { data } = await api.raw(
        'PUT',
        `/api/notes/${encodeURIComponent(title)}/assets/${encodeURIComponent(assetID)}/content`,
        file,
        {
          headers: {
            'X-Marvo-Instance-Token': instanceToken,
            'Content-Type': file.type || 'application/octet-stream',
          },
        },
      )
      this.trackMediaAsset(data as MediaAsset)
      return data as MediaAsset
    },

    async abandonMediaAsset(title: string, instanceToken: string, assetID: string) {
      try {
        const { data } = await api.delete(
          `/api/notes/${encodeURIComponent(title)}/assets/${encodeURIComponent(assetID)}?instance_token=${encodeURIComponent(instanceToken)}`,
        )
        this.trackMediaAsset(data as MediaAsset)
        return data as MediaAsset
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null
        throw error
      }
    },

    async renameNote(oldTitle: string, newTitle: string, instanceToken?: string) {
      const token = instanceToken || this.currentNote?.instance_token
      if (!token) throw new Error('note instance is not loaded')
      const { data } = await api.put(`/api/notes/${encodeURIComponent(oldTitle)}/rename`, {
        new_title: newTitle,
        instance_token: token,
      })
      this.currentNote = data as NoteDetail
      this.latestRemote = data as NoteDetail
      await this.fetchNotes()
      return data as NoteDetail
    },

    async fetchTrash() {
      const { data } = await api.get('/api/trash')
      this.trash = Array.isArray(data) ? (data as TrashEntry[]) : []
      return this.trash
    },

    async restoreTrash(id: string, newTitle: string) {
      const { data } = await api.post(`/api/trash/${encodeURIComponent(id)}/restore`, {
        new_title: newTitle,
      })
      await Promise.all([this.fetchTrash(), this.fetchNotes()])
      return data as NoteDetail
    },

    async permanentlyDeleteTrash(id: string) {
      await api.delete(`/api/trash/${encodeURIComponent(id)}`)
      await this.fetchTrash()
    },

    async emptyTrash() {
      await api.delete('/api/trash')
      this.trash = []
    },

    async getOrCreateNote(title: string) {
      try {
        return await this.getNote(title)
      } catch (error) {
        if (!(error instanceof ApiError) || error.status !== 404) throw error
        return this.createNote(title)
      }
    },
  },
})

on('note_list_changed', () => {
  void useNoteStore().fetchNotes()
})

on('note_changed', (message: any) => {
  const store = useNoteStore()
  const snapshot =
    message.note && message.instance_token
      ? ({
          note: message.note,
          content: message.content,
          content_revision: message.content_revision,
          meta_revision: message.meta_revision,
          instance_token: message.instance_token,
        } as NoteDetail)
      : null
  if (!snapshot) return
  if (store.currentNote?.note.title === snapshot.note.title) store.latestRemote = snapshot
  void store.fetchNotes()
})

on('subscribed', (message: any) => {
  const store = useNoteStore()
  if (!store.currentNote || message.title !== store.currentNote.note.title || !message.note) return
  store.latestRemote = {
    note: message.note,
    content: message.content,
    content_revision: message.content_revision,
    meta_revision: message.meta_revision,
    instance_token: message.instance_token,
  }
})

on('asset_changed', (message: any) => {
  const store = useNoteStore()
  if (!message.asset?.id || store.currentNote?.note.title !== message.title) return
  store.trackMediaAsset(message.asset as MediaAsset)
})
