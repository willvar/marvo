type Callback = (data: any) => void

let es: EventSource | null = null
let clientId = ''
let connecting = false
let connectWaiters: Array<(id: string) => void> = []
let reconnectAttempts = 0
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
const handlers = new Map<string, Set<Callback>>()
const subscribedNotes = new Set<string>()

function eventsUrl(): string {
  const baseUrl = import.meta.env.VITE_API_BASE || ''
  return `${baseUrl}/api/events?client_id=${encodeURIComponent(clientId)}`
}

function sendUrl(): string {
  const baseUrl = import.meta.env.VITE_API_BASE || ''
  return `${baseUrl}/api/send`
}

function generateId(): string {
  return typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : crypto.getRandomValues(new Uint32Array(4)).join('-')
}

export function connect(): Promise<string> {
  if (!connecting && es?.readyState === EventSource.OPEN && clientId) return Promise.resolve(clientId)
  if (!clientId) clientId = generateId()

  const result = new Promise<string>((resolve) => connectWaiters.push(resolve))
  if (!connecting) {
    connecting = true
    es?.close()
    es = new EventSource(eventsUrl(), { withCredentials: true })
    es.onopen = () => {
      reconnectAttempts = 0
    }
    es.onmessage = (event) => {
      let message: any
      try {
        message = JSON.parse(event.data)
      } catch {
        return
      }
      if (message.action === 'connected') {
        clientId = message.client_id
        connecting = false
        for (const title of subscribedNotes) void sendControl('subscribe', title)
        const waiters = connectWaiters
        connectWaiters = []
        for (const resolve of waiters) resolve(clientId)
        return
      }
      if (message.action) handlers.get(message.action)?.forEach((callback) => callback(message))
    }
    es.onerror = () => {
      es?.close()
      es = null
      connecting = false
      // Disconnected server-side clients are released. A fresh connection
      // re-subscribes and receives a complete note snapshot.
      clientId = ''
      scheduleReconnect()
    }
  }
  return result
}

function scheduleReconnect() {
  if (reconnectTimer) return
  reconnectAttempts++
  const delay = Math.min(1000 * 2 ** (reconnectAttempts - 1), 30000)
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    void connect()
  }, delay)
}

async function sendControl(action: 'subscribe' | 'unsubscribe' | 'close', title = '') {
  if (!clientId) await connect()
  const response = await fetch(sendUrl(), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action, title, client_id: clientId }),
  })
  if (response.status === 404) {
    es?.close()
    es = null
    connecting = false
    clientId = ''
    await connect()
    if (action !== 'close') return sendControl(action, title)
  }
  if (!response.ok) throw new Error(`event control failed: ${response.status}`)
}

export function on(action: string, callback: Callback): () => void {
  if (!handlers.has(action)) handlers.set(action, new Set())
  handlers.get(action)!.add(callback)
  return () => handlers.get(action)?.delete(callback)
}

export function subscribe(title: string) {
  if (!title) return
  subscribedNotes.add(title)
  void connect()
    .then(() => sendControl('subscribe', title))
    .catch(() => {})
}

export function unsubscribe(title: string) {
  subscribedNotes.delete(title)
  if (clientId) void sendControl('unsubscribe', title).catch(() => {})
}

export function disconnect() {
  if (reconnectTimer) clearTimeout(reconnectTimer)
  reconnectTimer = null
  if (clientId) void sendControl('close').catch(() => {})
  es?.close()
  es = null
  connecting = false
  clientId = ''
  connectWaiters = []
  subscribedNotes.clear()
}
