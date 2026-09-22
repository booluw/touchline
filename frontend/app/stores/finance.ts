// Pinia store: finance — ledger, cash vs budget breakdown (PRD section 36).
// The API shapes mirror internal/finance read models (S05-02); money is in
// whole currency units.
export interface FinanceFactor { label: string; amount: number }
export interface BudgetLine { season: number; allocated: number; committed: number; available: number }
export interface CommitmentLine { count: number; weekly_wage: number; annual_wage: number }
export interface FinanceSummary {
  currency: string
  cash: number
  operating_profit: number
  transfer_budget: BudgetLine
  wage_budget: BudgetLine
  wage_commitments: CommitmentLine
  committed_spending: number
  projected_revenue: number
  projected_year_end_balance: number
  future_installments: number
  debt: number
  factors: FinanceFactor[]
}
export interface LedgerEntry {
  id: string
  entry_type: 'credit' | 'debit'
  category: string
  amount: number
  description: string
  related_event_id?: string
  occurred_at: string
}
export interface ContractView {
  id: string
  player: { id: string; name: string }
  weekly_wage: number
  signing_bonus: number
  start_date: string
  end_date: string
  release_clause?: number
  status: string
}

export const useFinanceStore = defineStore('finance', {
  state: () => ({
    summary: null as FinanceSummary | null,
    ledger: [] as LedgerEntry[],
    contracts: [] as ContractView[],
    error: '',
    loading: false,
  }),
  actions: {
    async load(clubId: string) {
      const { authedFetch } = useAuth()
      this.loading = true
      try {
        const [sum, led, con] = await Promise.all([
          authedFetch(`/api/clubs/${clubId}/finances`),
          authedFetch(`/api/clubs/${clubId}/ledger`),
          authedFetch(`/api/clubs/${clubId}/contracts`),
        ])
        if (!sum.ok || !led.ok || !con.ok) throw new Error('Could not load finances.')
        this.summary = await sum.json()
        this.ledger = await led.json()
        this.contracts = await con.json()
      } finally {
        this.loading = false
      }
    },
  },
})