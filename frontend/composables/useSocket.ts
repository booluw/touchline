// Single multiplexed WebSocket connection for live match ticks and notifications.
// Opened once after auth; events fanned out by type to whoever is subscribed.
export interface SocketEvent {
  type: string
  payload: unknown
}

type Handler = (event: SocketEvent) => void

export function useSocket() {
  const { public: { apiBase } } = useRuntimeConfig()
  const wsUrl = apiBase.replace(/^http/, 'ws')

  const handlers = new Map<string, Set<Handler>>()
  let socket: WebSocket | null = null

  function connect() {
    if (socket && socket.readyState === WebSocket.OPEN) return

    socket = new WebSocket(`${wsUrl}/ws`)

    socket.onmessage = (message) => {
      const event: SocketEvent = JSON.parse(message.data)
      const listeners = handlers.get(event.type)
      listeners?.forEach(handler => handler(event))
    }
  }

  function on(type: string, handler: Handler) {
    if (!handlers.has(type)) handlers.set(type, new Set())
    handlers.get(type)!.add(handler)
  }

  function disconnect() {
    socket?.close()
    socket = null
  }

  return { connect, on, disconnect }
}