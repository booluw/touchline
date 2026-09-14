// Pinia store: realtime.ts — owns the single WebSocket session lifecycle and
// exposes the latest engine pushes to screens. Screens read this store instead
// of opening sockets; the connection itself lives in composables/useSocket.
import type { SocketEvent } from '~/composables/useSocket'

export interface WorldTickPayload {
  event_id?: string
  granularity?: string
  tick?: number
}

export const useRealtimeStore = defineStore('realtime', {
  state: () => ({
    connected: false,
    wired: false,
    lastEvent: null as null | SocketEvent,
    lastTick: null as null | WorldTickPayload,
  }),

  actions: {
    // connect() is safe to call from every login and screen mount: the
    // underlying session is idempotent and handlers are registered once.
    connect() {
      const socket = useSocket()

      if (!this.wired) {
        this.wired = true
        socket.on('world_tick', (event) => {
          this.lastEvent = event
          this.lastTick = (event.payload as WorldTickPayload) ?? null
        })
        socket.on('error', (event) => {
          this.lastEvent = event
        })
        watch(socket.status, (status) => {
          this.connected = status === 'open'
        })
      }

      socket.connect()
      this.connected = socket.status.value === 'open'
    },

    disconnect() {
      useSocket().disconnect()
      this.connected = false
    },
  },
})