import { create } from 'zustand'
import api from '../api/useApi'

interface AuthState {
  isAuthenticated: boolean
  login: (password: string) => Promise<void>
  logout: () => Promise<void>
  check: () => Promise<void>
}

export const useAuthStore = create<AuthState>((set) => ({
  isAuthenticated: false,

  login: async (password: string) => {
    const { data } = await api.post('/api/auth/verify', { password })
    const token = data.challenge_token
    await api.post('/api/auth', { challenge_token: token })
    set({ isAuthenticated: true })
  },

  logout: async () => {
    await api.post('/api/auth/logout')
    set({ isAuthenticated: false })
  },

  check: async () => {
    try {
      await api.get('/api/notes')
      set({ isAuthenticated: true })
    } catch {
      set({ isAuthenticated: false })
    }
  },
}))
