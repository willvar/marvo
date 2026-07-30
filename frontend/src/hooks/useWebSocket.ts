type WSCallback = (data: any) => void

let ws: WebSocket | null = null
let connecting = false
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectDelay = 1000

const handlers = new Map<string, Set<WSCallback>>()
const pending: object[] = []
const subscribedNotes = new Set<string>()
let clientId = ''

function getWSUrl(): string {
  const base = import.meta.env.VITE_WS_BASE || `ws://${window.location.host}`
  return `${base}/api/ws`
}

function connect(): Promise<string> {
  return new Promise((resolve, reject) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      resolve(clientId)
      return
    }
    if (connecting) {
      const wait = setInterval(() => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          clearInterval(wait)
          resolve(clientId)
        }
      }, 100)
      return
    }
    connecting = true

    const socket = new WebSocket(getWSUrl())

    socket.onopen = () => {
      ws = socket
      connecting = false
      reconnectDelay = 1000
    }

    socket.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)

        if (msg.action === 'connected') {
          clientId = msg.client_id
          for (const m of pending) {
            socket.send(JSON.stringify(m))
          }
          pending.length = 0
          for (const title of subscribedNotes) {
            socket.send(JSON.stringify({ action: 'subscribe', title, client_id: clientId }))
          }
          resolve(clientId)
          return
        }

        if (msg.action && handlers.has(msg.action)) {
          handlers.get(msg.action)!.forEach(cb => cb(msg))
        }
      } catch { /* ignore */ }
    }

    socket.onclose = () => {
      ws = null
      connecting = false
      if (reconnectTimer) clearTimeout(reconnectTimer)
      reconnectTimer = setTimeout(() => {
        reconnectDelay = Math.min(reconnectDelay * 2, 30000)
        connect()
      }, reconnectDelay)
    }

    socket.onerror = () => {
      connecting = false
      reject(new Error('WebSocket connection failed'))
    }
  })
}

function on(action: string, callback: WSCallback): () => void {
  if (!handlers.has(action)) {
    handlers.set(action, new Set())
  }
  handlers.get(action)!.add(callback)

  return () => {
    handlers.get(action)?.delete(callback)
  }
}

function send(data: object) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(data))
  } else {
    pending.push(data)
  }
}

function subscribe(title: string) {
  subscribedNotes.add(title)
  const msg = { action: 'subscribe', title, client_id: clientId }
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(msg))
  }
}

function unsubscribe(title: string) {
  subscribedNotes.delete(title)
}

function getClientId(): string {
  return clientId
}

export { connect, on, send, subscribe, unsubscribe, getClientId }
