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
  const { public: { apiBase } } = useRuntimeConfig()

  async function fetchDashboard(): Promise<DashboardData> {
    const { data } = await useFetch<DashboardData>(`${apiBase}/api/dashboard`)
    return data.value ?? { urgent: [], important: [], interesting: [] }
  }

  return { fetchDashboard }
}