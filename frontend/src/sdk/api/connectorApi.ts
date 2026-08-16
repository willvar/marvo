import { createHTTPNotixClient } from '@willvar/notix-vue'

import { apiFetch, apiRequestURL } from './useApi'

export function createConnectorClient() {
  return createHTTPNotixClient({
    baseURL: apiRequestURL('/api/admin/connectors'),
    fetch: apiFetch,
    credentials: 'include',
  })
}
