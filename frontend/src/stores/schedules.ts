import { defineStore } from 'pinia'
import { api, type AutomaticTask, type ScheduleInput, type ScheduleRun } from '../sdk'

let schedulesLoadPromise: Promise<void> | null = null
let schedulesReloadRequested = false

function normalizeTask(task: AutomaticTask): AutomaticTask {
  return {
    ...task,
    schedule: { ...task.schedule, spec: { ...task.schedule?.spec } },
    active_run: task.active_run ? { ...task.active_run } : undefined,
  }
}

function sortTasks(tasks: AutomaticTask[]) {
  const statusOrder: Record<string, number> = { active: 0, paused: 1, completed: 2 }
  return [...tasks].sort((left, right) => {
    const status = (statusOrder[left.status] ?? 3) - (statusOrder[right.status] ?? 3)
    if (status !== 0) return status
    if (left.next_run_at && right.next_run_at) {
      const next = new Date(left.next_run_at).getTime() - new Date(right.next_run_at).getTime()
      if (next !== 0) return next
    } else if (left.next_run_at || right.next_run_at) {
      return left.next_run_at ? -1 : 1
    }
    const updated = new Date(right.updated_at).getTime() - new Date(left.updated_at).getTime()
    return updated || left.id.localeCompare(right.id)
  })
}

export const useSchedulesStore = defineStore('schedules', {
  state: () => ({
    tasks: [] as AutomaticTask[],
    runs: {} as Record<string, ScheduleRun[]>,
    loaded: false,
    loading: false,
    error: '',
  }),
  actions: {
    applyTask(task: AutomaticTask, preserveRun = false) {
      const normalized = normalizeTask(task)
      const index = this.tasks.findIndex((candidate) => candidate.id === normalized.id)
      if (index < 0) {
        this.tasks = sortTasks([...this.tasks, normalized])
        return normalized
      }
      if (preserveRun && !normalized.active_run) normalized.active_run = this.tasks[index].active_run
      this.tasks.splice(index, 1, normalized)
      this.tasks = sortTasks(this.tasks)
      return normalized
    },
    async load(force = false) {
      if (schedulesLoadPromise) {
        if (force) schedulesReloadRequested = true
        return schedulesLoadPromise
      }
      if (!force && this.loaded) return
      this.loading = true
      const request = (async () => {
        do {
          schedulesReloadRequested = false
          this.error = ''
          const { data } = await api.get('/api/schedules')
          this.tasks = Array.isArray(data.tasks)
            ? sortTasks(data.tasks.map((task: AutomaticTask) => normalizeTask(task)))
            : []
          this.loaded = true
        } while (schedulesReloadRequested)
      })()
      schedulesLoadPromise = request
      try {
        await request
      } catch (cause) {
        this.error = cause instanceof Error ? cause.message : '无法加载自动任务'
        throw cause
      } finally {
        if (schedulesLoadPromise === request) schedulesLoadPromise = null
        this.loading = false
      }
    },
    async handleChanged() {
      if (this.loaded || this.loading) await this.load(true).catch(() => undefined)
    },
    async create(input: ScheduleInput) {
      const { data } = await api.post('/api/schedules', input)
      return this.applyTask(data as AutomaticTask)
    },
    async update(id: string, revision: number, input: ScheduleInput) {
      const { data } = await api.put(`/api/schedules/${encodeURIComponent(id)}`, { revision, ...input })
      return this.applyTask(data as AutomaticTask, true)
    },
    async pause(task: AutomaticTask) {
      const { data } = await api.post(`/api/schedules/${encodeURIComponent(task.id)}/pause`, {
        revision: task.revision,
      })
      return this.applyTask(data as AutomaticTask, true)
    },
    async resume(task: AutomaticTask) {
      const { data } = await api.post(`/api/schedules/${encodeURIComponent(task.id)}/resume`, {
        revision: task.revision,
      })
      return this.applyTask(data as AutomaticTask, true)
    },
    async runNow(task: AutomaticTask) {
      const { data } = await api.post(`/api/schedules/${encodeURIComponent(task.id)}/run`)
      const run = data.run as ScheduleRun
      const current = this.tasks.find((candidate) => candidate.id === task.id)
      if (current) current.active_run = { ...run }
      return run
    },
    async stop(task: AutomaticTask) {
      if (!task.active_run?.id) throw new Error('本轮运行已经发生变化，请刷新后重试')
      await api.post(`/api/schedules/${encodeURIComponent(task.id)}/stop`, { run_id: task.active_run.id })
    },
    async remove(task: AutomaticTask) {
      await api.delete(`/api/schedules/${encodeURIComponent(task.id)}`, { revision: task.revision })
      this.tasks = this.tasks.filter((candidate) => candidate.id !== task.id)
      delete this.runs[task.id]
    },
    async loadRuns(id: string, force = false) {
      if (!force && this.runs[id]) return this.runs[id]
      const { data } = await api.get(`/api/schedules/${encodeURIComponent(id)}/runs`, { params: { limit: 30 } })
      const runs = Array.isArray(data.runs) ? data.runs.map((run: ScheduleRun) => ({ ...run })) : []
      this.runs[id] = runs
      return runs
    },
  },
})
