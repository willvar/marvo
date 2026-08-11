import { defineStore } from 'pinia'
import { api, type AgentPersonalizationResponse, type AgentPersonalizationRule } from '../sdk'

let personalizationLoadPromise: Promise<AgentPersonalizationResponse> | null = null

export const useAgentPersonalizationStore = defineStore('agent-personalization', {
  state: () => ({
    snapshot: null as AgentPersonalizationResponse | null,
  }),
  actions: {
    applySnapshot(snapshot: AgentPersonalizationResponse) {
      this.snapshot = {
        ...snapshot,
        rules: Array.isArray(snapshot.rules) ? snapshot.rules.map((rule) => ({ ...rule })) : [],
      }
      return this.snapshot
    },
    async load(force = false): Promise<AgentPersonalizationResponse> {
      if (!force && this.snapshot) return this.snapshot
      if (personalizationLoadPromise) return personalizationLoadPromise
      const request = api
        .get('/api/agent/personalization')
        .then(({ data }) => this.applySnapshot(data as AgentPersonalizationResponse))
      personalizationLoadPromise = request
      try {
        return await request
      } finally {
        if (personalizationLoadPromise === request) personalizationLoadPromise = null
      }
    },
    async save(rules: AgentPersonalizationRule[], revision: string): Promise<AgentPersonalizationResponse> {
      const { data } = await api.put('/api/agent/personalization', { rules, revision })
      return this.applySnapshot(data as AgentPersonalizationResponse)
    },
  },
})
