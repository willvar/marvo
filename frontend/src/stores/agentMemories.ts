import { defineStore } from 'pinia'
import { api, type AgentMemoriesResponse, type AgentMemory } from '../sdk'

let memoriesLoadPromise: Promise<AgentMemoriesResponse> | null = null

export const useAgentMemoriesStore = defineStore('agent-memories', {
  state: () => ({
    snapshot: null as AgentMemoriesResponse | null,
  }),
  actions: {
    applySnapshot(snapshot: AgentMemoriesResponse) {
      this.snapshot = {
        ...snapshot,
        memories: Array.isArray(snapshot.memories) ? snapshot.memories.map((memory) => ({ ...memory })) : [],
      }
      return this.snapshot
    },
    async load(force = false): Promise<AgentMemoriesResponse> {
      if (!force && this.snapshot) return this.snapshot
      if (memoriesLoadPromise) return memoriesLoadPromise
      const request = api
        .get('/api/agent/memories')
        .then(({ data }) => this.applySnapshot(data as AgentMemoriesResponse))
      memoriesLoadPromise = request
      try {
        return await request
      } finally {
        if (memoriesLoadPromise === request) memoriesLoadPromise = null
      }
    },
    async save(memories: AgentMemory[], revision: string): Promise<AgentMemoriesResponse> {
      const { data } = await api.put('/api/agent/memories', { memories, revision })
      return this.applySnapshot(data as AgentMemoriesResponse)
    },
  },
})
