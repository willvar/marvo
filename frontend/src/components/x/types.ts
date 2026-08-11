import type { Component } from 'vue'

export interface XAttachmentItem {
  key: string
  name: string
  size?: number
  mime?: string
  url?: string
  file?: File
  status?: 'ready' | 'preparing' | 'error'
  statusText?: string
}

export interface XConversationItem {
  key: string
  label: string
  disabled?: boolean
  status?: 'attention' | 'retry' | 'running' | 'error'
  statusLabel?: string
}

export interface XConversationAction {
  key: string
  label: string
  icon?: Component
  danger?: boolean
}

export interface XPromptItem {
  key: string
  label: string
  description?: string
  icon?: Component
  disabled?: boolean
}

export type XThoughtStatus = 'loading' | 'success' | 'warning' | 'error' | 'stopped' | 'default'

export type XSubtaskStatus = 'running' | 'retry' | 'success' | 'error' | 'stopped' | 'default'

export interface XThoughtItem {
  key: string
  title: string
  description?: string
  status?: XThoughtStatus
  collapsible?: boolean
  children?: XThoughtItem[]
}
