import { defineStore } from 'pinia'
import { api, type ActivityCounts, type ActivityItem, type ActivityPage } from '../sdk'

let activityLoadPromise: Promise<void> | null = null
let activityCountsPromise: Promise<void> | null = null

function normalizeActivity(item: ActivityItem): ActivityItem {
  return {
    ...item,
    choices: Array.isArray(item.choices) ? [...item.choices] : [],
    response_choices: Array.isArray(item.response_choices) ? [...item.response_choices] : [],
    multiple: item.multiple === true,
    replying: item.replying === true,
  }
}

export const useActivityStore = defineStore('activity', {
  state: () => ({
    activities: [] as ActivityItem[],
    nextCursor: '',
    unread: 0,
    pending: 0,
    loaded: false,
    loading: false,
    loadingMore: false,
    error: '',
  }),
  actions: {
    applyCounts(counts: Partial<ActivityCounts>) {
      if (typeof counts.unread === 'number') this.unread = Math.max(0, counts.unread)
      if (typeof counts.pending === 'number') this.pending = Math.max(0, counts.pending)
    },
    applyPage(page: ActivityPage, append: boolean) {
      const incoming = Array.isArray(page.activities) ? page.activities.map(normalizeActivity) : []
      if (append) {
        const known = new Set(this.activities.map((item) => item.id))
        this.activities = [...this.activities, ...incoming.filter((item) => !known.has(item.id))]
      } else {
        this.activities = incoming
      }
      this.nextCursor = page.next_cursor || ''
      this.applyCounts(page)
      this.loaded = true
    },
    async load(force = false) {
      if (activityLoadPromise) {
        await activityLoadPromise
        if (!force) return
      }
      if (!force && this.loaded) return
      this.loading = true
      this.error = ''
      const request = (async () => {
        const { data } = await api.get('/api/activity', {
          params: { limit: 30 },
        })
        this.applyPage(data as ActivityPage, false)
      })()
      activityLoadPromise = request
      try {
        await request
      } catch (cause) {
        this.error = cause instanceof Error ? cause.message : '无法加载活动'
        throw cause
      } finally {
        if (activityLoadPromise === request) activityLoadPromise = null
        this.loading = false
      }
    },
    async loadMore() {
      if (!this.nextCursor || this.loadingMore) return
      const cursor = this.nextCursor
      this.loadingMore = true
      this.error = ''
      try {
        const { data } = await api.get('/api/activity', {
          params: { limit: 30, cursor },
        })
        if (this.nextCursor === cursor) this.applyPage(data as ActivityPage, true)
      } catch (cause) {
        this.error = cause instanceof Error ? cause.message : '无法加载更多活动'
        throw cause
      } finally {
        this.loadingMore = false
      }
    },
    async loadOne(id: string) {
      const existing = this.activities.find((item) => item.id === id)
      if (existing) return existing
      const { data } = await api.get(`/api/activity/${encodeURIComponent(id)}`)
      const item = normalizeActivity(data as ActivityItem)
      this.activities = [...this.activities, item].sort((left, right) => {
        const created = new Date(right.created_at).getTime() - new Date(left.created_at).getTime()
        return created || right.id.localeCompare(left.id)
      })
      return item
    },
    async loadCounts() {
      if (activityCountsPromise) return activityCountsPromise
      const request = api
        .get('/api/activity/counts')
        .then(({ data }) => this.applyCounts(data as ActivityCounts))
        .finally(() => {
          if (activityCountsPromise === request) activityCountsPromise = null
        })
      activityCountsPromise = request
      return request
    },
    async handleChanged(counts?: Partial<ActivityCounts>) {
      if (counts) this.applyCounts(counts)
      else await this.loadCounts().catch(() => undefined)
      if (this.loaded) await this.load(true).catch(() => undefined)
    },
    async markRead(ids: string[]) {
      const unreadIDs = [...new Set(ids)].filter((id) =>
        this.activities.some((item) => item.id === id && !item.read_at),
      )
      if (unreadIDs.length === 0) return
      const readAt = new Date().toISOString()
      const unreadSet = new Set(unreadIDs)
      this.activities = this.activities.map((item) =>
        unreadSet.has(item.id) && !item.read_at ? { ...item, read_at: readAt } : item,
      )
      this.unread = Math.max(0, this.unread - unreadIDs.length)
      try {
        await api.post('/api/activity/read', { ids: unreadIDs })
      } catch (cause) {
        await this.load(true).catch(() => undefined)
        throw cause
      }
    },
    async deleteActivity(id: string) {
      const existing = this.activities.find((item) => item.id === id)
      await api.delete(`/api/activity/${encodeURIComponent(id)}`)
      this.activities = this.activities.filter((item) => item.id !== id)
      if (existing && !existing.read_at) this.unread = Math.max(0, this.unread - 1)
      if (existing?.kind === 'choice' && !existing.responded_at) this.pending = Math.max(0, this.pending - 1)
      if (this.activities.length === 0 && this.nextCursor) await this.load(true)
    },
  },
})
