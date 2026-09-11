// Home dashboard — aggregation of "urgent / important / interesting" (PRD section 53)
export interface DashboardItem {
  id: string
  priority: 'urgent' | 'important' | 'interesting'
  category: string
  title: string
  description: string
  createdAt: string
}

export interface DashboardData {
  urgent: DashboardItem[]
  important: DashboardItem[]
  interesting: DashboardItem[]
}

export function useDashboard() {
  const { authedFetch } = useAuth()

  async function fetchDashboard(): Promise<DashboardData> {
    const res = await authedFetch('/api/dashboard')
    if (!res.ok) {
      return { urgent: [], important: [], interesting: [] }
    }
    return (await res.json()) as DashboardData
  }

  return { fetchDashboard }
}