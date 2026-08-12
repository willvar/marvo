import { defineStore } from 'pinia'

export type AgentAssistantDisplayMode = 'floating' | 'sidebar'

export const AGENT_DISPLAY_MODE_STORAGE_KEY = 'marvo.ui.agentAssistantDisplayMode'

function storedAgentDisplayMode(): AgentAssistantDisplayMode {
  if (typeof window === 'undefined') return 'floating'
  try {
    return window.localStorage.getItem(AGENT_DISPLAY_MODE_STORAGE_KEY) === 'sidebar' ? 'sidebar' : 'floating'
  } catch {
    return 'floating'
  }
}

export const useUIPreferencesStore = defineStore('ui-preferences', {
  state: () => ({
    agentAssistantDisplayMode: storedAgentDisplayMode() as AgentAssistantDisplayMode,
  }),

  actions: {
    syncAgentAssistantDisplayMode() {
      this.agentAssistantDisplayMode = storedAgentDisplayMode()
    },

    setAgentAssistantDisplayMode(mode: AgentAssistantDisplayMode) {
      this.agentAssistantDisplayMode = mode
      try {
        window.localStorage.setItem(AGENT_DISPLAY_MODE_STORAGE_KEY, mode)
      } catch {
        /* Keep the in-memory preference when browser storage is unavailable. */
      }
    },
  },
})
