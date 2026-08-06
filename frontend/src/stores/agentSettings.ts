import { defineStore } from 'pinia'
import { api, type AgentSettingsResponse, type AgentSettingsUpdate } from '../sdk'

let settingsLoadPromise: Promise<AgentSettingsResponse> | null = null

export const useAgentSettingsStore = defineStore('agent-settings', {
  state: () => ({
    settings: null as AgentSettingsResponse | null,
    loaded: false,
    loading: false,
  }),

  actions: {
    applySettings(settings: AgentSettingsResponse) {
      this.settings = settings
      this.loaded = true
    },

    async load(force = false): Promise<AgentSettingsResponse> {
      if (!force && this.loaded && this.settings) return this.settings
      if (settingsLoadPromise) {
        const settings = await settingsLoadPromise
        this.applySettings(settings)
        return settings
      }

      this.loading = true
      const request = api.get('/api/agent/settings').then(({ data }) => data as AgentSettingsResponse)
      settingsLoadPromise = request
      try {
        const settings = await request
        this.applySettings(settings)
        return settings
      } finally {
        if (settingsLoadPromise === request) settingsLoadPromise = null
        this.loading = false
      }
    },

    async save(update: AgentSettingsUpdate): Promise<AgentSettingsResponse> {
      const { data } = await api.put('/api/agent/settings', update)
      const settings = data as AgentSettingsResponse
      this.applySettings(settings)
      return settings
    },
  },
})
