type ConnectorFieldType = 'text' | 'secret' | 'url' | 'number' | 'boolean' | 'select' | 'textarea'

export interface ConnectorFieldOption {
  label: string
  value: string | number | boolean
}

export interface ConnectorField {
  key: string
  label: string
  type: ConnectorFieldType
  required: boolean
  placeholder?: string
  help?: string
  default?: unknown
  options?: ConnectorFieldOption[]
  sensitive?: boolean
}

export interface ConnectorProvider {
  id: string
  name: string
  category: string
  description: string
  keywords: string[]
  documentation?: string
  fields: ConnectorField[]
}

interface ConnectorDeliverySummary {
  pending: number
  sent: number
  failed: number
  last_error?: string
  last_sent_at?: string
}

export interface ActivityConnector {
  id: string
  provider_id: string
  provider_name: string
  name: string
  enabled: boolean
  config: Record<string, unknown>
  secret_configured: Record<string, boolean>
  delivery: ConnectorDeliverySummary
  created_at: string
  updated_at: string
}
