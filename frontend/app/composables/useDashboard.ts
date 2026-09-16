// Home dashboard — aggregation of "urgent / important / interesting" (PRD section 53).
//
// The GET feed is authoritative; the server also pushes incremental
// `dashboard_update` events (S07-01) whose items are merged in by their stable
// ID, so a bid landing mid-session appears without a refetch.
import type { SocketEvent } from './useSocket'

export interface DashboardAction {
  kind: string
  club_id?: string
  player_id?: string
  fixture_id?: string
  bid_id?: string
}

export interface DashboardItem {
  id: string
  priority: 'urgent' | 'important' | 'interesting'
  category: string
  title: string
  description: string
  created_at: string
  action?: DashboardAction
}

export interface DashboardData {
  urgent: DashboardItem[]
  important: DashboardItem[]
  interesting: DashboardItem[]
}

export interface DashboardUpdatePayload {
  category: keyof DashboardData
  items: DashboardItem[]
}

export const emptyDashboard = (): DashboardData => ({
  urgent: [],
  important: [],
  interesting: [],
})

export function useDashboard() {
  const { authedFetch } = useAuth()
  const socket = useSocket()

  const data = ref<DashboardData>(emptyDashboard())
  const loading = ref(false)
  const loaded = ref(false)

  // Merge pushes by stable item ID: new items front the section, known ones
  // are ignored (the GET response is the source of truth for removals).
  function merge(section: keyof DashboardData, incoming: DashboardItem[]) {
    const seen = new Set(data.value[section].map(i => i.id))
    const fresh = incoming.filter(i => i && i.id && !seen.has(i.id))
    if (fresh.length > 0) {
      data.value = { ...data.value, [section]: [...fresh, ...data.value[section]] }
    }
  }

  async function fetchDashboard(): Promise<DashboardData> {
    loading.value = true
    try {
      const res = await authedFetch('/api/dashboard')
      if (res.ok) {
        data.value = (await res.json()) as DashboardData
        loaded.value = true
      }
    } finally {
      loading.value = false
    }
    return data.value
  }

  let off: (() => void) | null = null
  onMounted(() => {
    off = socket.on('dashboard_update', (event: SocketEvent) => {
      const payload = event.payload as DashboardUpdatePayload | null
      if (!payload || !payload.category || !Array.isArray(payload.items)) return
      if (!(payload.category in data.value)) return
      merge(payload.category, payload.items)
    })
    socket.connect()
  })
  onUnmounted(() => off?.())

  return { data, loading, loaded, fetchDashboard }
}