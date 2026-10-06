export interface ManagerDashboardItem<Type> {
  id: string
  priority: Type
  category: "match" | string
  title: string
  description: string
  created_at: string
  action: {
    kind: "set_lineup" | "view_fixture" | "view_standings" | "respond_bid" | "renew_contract" | "view_board" | "view_finances" | "view_player"
    club_id?: string
    fixture_id?: string
    bid_id?: string
    player_id?: string
  }
  explanation?: {
    factors: {
      delta: number
      label: number
    }
    score: number
    subject: string
  }[]
}

export interface ManagerDashboardSummary {
  club_id: string
  board: {
    confidence: number
    change: number
  }
  morale: {
    average: number
    unhappy: number
    players: number
  }
  finance: {
    cash: number,
    weekly_wage_bill: number
    season_wage_budget: number
  }
  league: {
    position: number
    points: number
    played: number
    season_label: string
  }
}

export interface ManagerDashboard {
  urgent: ManagerDashboardItem<"urgent">[]
  important: ManagerDashboardItem<"important">[]
  interesting: ManagerDashboardItem<"interesting">[]
  summary: ManagerDashboardSummary[]
  counts: {
    urgent: number
    important: number
    interesting: number
    by_category: {
      match: number
      rivals: number
      standings: number
    }
  }
}