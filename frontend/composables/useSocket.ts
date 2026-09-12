// Single multiplexed WebSocket connection for live World engine pushes
// (match ticks, world ticks, notifications, ...). Mirrors the server envelope
// in pkg/realtime/event.go:
//
//   { type, payload, world_id?, ts? }
//
// One SocketSession exists per page/app, so `useSocket()` from several
// consumers always returns the same session: screens subscribe with on() and
// never open their own sockets. The session reconnects with exponential backoff
// while enabled, ignores unknown event types (after one warning), and routes
// server `error` envelopes straight to subscribers.
export interface SocketEvent {
  type: string
  payload: unknown
  world_id?: string
  ts?: string
}

type Handler = (event: SocketEvent) => void
type ConnectionStatus = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed'

const BASE_RECONNECT_DELAY_MS = 1000
const MAX_RECONNECT_DELAY_MS = 15_000

class SocketSession {
  private handlers = new Map<string, Set<Handler>>()
  private socket: WebSocket | null = null
  private enabled = false
  private retry = 0
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private url: string | null = null

  readonly status = ref<ConnectionStatus>('idle')

  // connect() is the single entry point for the session. Idempotent while the
  // socket is already open or connecting; a closed/errored session restarts.
  connect() {
    this.ensureUrl()
    if (
      this.socket !== null &&
      (this.socket.readyState === WebSocket.OPEN || this.socket.readyState === WebSocket.CONNECTING)
    ) {
      return
    }
    this.enabled = true
    this.open()
  }

  on(type: string, handler: Handler): () => void {
    let set = this.handlers.get(type)
    if (!set) {
      set = new Set()
      this.handlers.set(type, set)
    }
    set.add(handler)
    return () => set!.delete(handler)
  }

  disconnect() {
    this.enabled = false
    this.clearReconnectTimer()
    if (this.socket) {
      const ws = this.socket
      this.socket = null
      ws.onclose = null
      ws.close()
    }
    this.status.value = 'idle'
  }

  private ensureUrl() {
    if (this.url) return
    const { public: { apiBase } } = useRuntimeConfig()
    this.url = `${apiBase.replace(/^http/, 'ws')}/ws`
  }

  private open() {
    this.clearReconnectTimer()
    if (!this.enabled) return

    this.status.value = this.retry > 0 ? 'reconnecting' : 'connecting'
    const ws = new WebSocket(this.url!)
    this.socket = ws

    ws.onopen = () => {
      this.retry = 0
      this.status.value = 'open'
    }
    ws.onmessage = (message) => this.dispatch(message.data)
    // onerror then close: let close drive the reconnect so backoff is uniform.
    ws.onerror = () => ws.close()
    ws.onclose = () => {
      if (this.socket === ws) this.socket = null
      this.scheduleReconnect()
    }
  }

  private dispatch(raw: string) {
    let event: SocketEvent
    try {
      event = JSON.parse(raw)
    } catch {
      console.warn('[realtime] ignoring non-JSON frame')
      return
    }
    if (!event || typeof event.type !== 'string' || event.type === '') {
      console.warn('[realtime] ignoring malformed frame', raw)
      return
    }

    const listeners = this.handlers.get(event.type)
    if (listeners && listeners.size > 0) {
      for (const handler of listeners) {
        try {
          handler(event)
        } catch (err) {
          console.error(`[realtime] handler for "${event.type}" threw`, err)
        }
      }
      return
    }
    // Unknown event types are ignored per S02-04 AC5; one warning aids debugging.
    if (event.type !== 'pong') {
      console.warn(`[realtime] unhandled event type "${event.type}"`)
    }
  }

  private scheduleReconnect() {
    if (!this.enabled) {
      if (this.status.value === 'closed') this.status.value = 'idle'
      return
    }
    const jitter = Math.floor(Math.random() * 250)
    const delay = Math.min(BASE_RECONNECT_DELAY_MS * 2 ** this.retry, MAX_RECONNECT_DELAY_MS) + jitter
    this.retry += 1
    this.status.value = 'reconnecting'
    this.reconnectTimer = setTimeout(() => this.open(), delay)
  }

  private clearReconnectTimer() {
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
  }
}

// Single session per page/app; consumers always share it.
const session = new SocketSession()

export function useSocket() {
  return {
    status: session.status,
    connect: session.connect.bind(session),
    disconnect: session.disconnect.bind(session),
    on: session.on.bind(session),
  }
}