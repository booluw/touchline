export interface Country {
  id: string
  world_id: string
  code: string
  name: string
}

export interface League {
  id: string
  world_id: string
  country_id: string
  name: string
  tier: number
  team_count: number
  status: string
  promotions: number
  relegations: number
  promotes_to: string | null
  relegates_to: string | null
}

export interface LeagueSeed {
  league_id: string
  name: string
  tier: number
  team_count: number
  season_id: string
  season_label: string
  fixture_count: number
  matchdays: number
}

export interface SeedResult {
  world_id: string
  country_id: string
  leagues: LeagueSeed[]
}

export interface Fixture {
  id: string
  home_club_id: string
  home_club_name?: string
  away_club_id: string
  away_club_name?: string
  matchday: number
  scheduled_at: string
  status: string
  home_score: number | null
  away_score: number | null
}

export interface StandingRow {
  club_name: string
  club_short: string
  played: number
  won: number
  drawn: number
  lost: number
  goals_for: number
  goals_against: number
  points: number
}

export interface Standings {
  season_id: string
  season_label: string
  season_number: number
  status: string
  rows: StandingRow[]
}

export interface ClubNamePools {
  stems: string[]
  suffixes: string[]
}

export function useCompetition() {
  const { authedFetch } = useAuth()

  // ---------- Admin (world-scoped via body) ----------

  async function createCountry(worldId: string, code: string, name: string): Promise<Country> {
    const res = await authedFetch('/api/admin/countries', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ world_id: worldId, code, name }),
    })
    if (!res.ok) throw new Error((await res.json().catch(() => ({})))?.error ?? 'Failed')
    return res.json()
  }

  async function listCountries(worldId: string): Promise<Country[]> {
    const res = await authedFetch(`/api/admin/countries?world_id=${worldId}`)
    if (!res.ok) return []
    const body = await res.json()
    return body.countries ?? []
  }

  async function createLeague(params: {
    country_id: string
    name: string
    tier: number
    team_count: number
    promotions: number
    relegations: number
  }): Promise<League> {
    const res = await authedFetch('/api/admin/leagues', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(params),
    })
    if (!res.ok) throw new Error((await res.json().catch(() => ({})))?.error ?? 'Failed')
    return res.json()
  }

  async function listLeagues(worldId: string): Promise<League[]> {
    const res = await authedFetch(`/api/admin/leagues?world_id=${worldId}`)
    if (!res.ok) return []
    const body = await res.json()
    return body.leagues ?? []
  }

  async function updateAdjacency(leagueId: string, body: {
    promotes_to?: string | null
    relegates_to?: string | null
  }): Promise<void> {
    const res = await authedFetch(`/api/admin/leagues/${leagueId}/adjacency`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok) throw new Error((await res.json().catch(() => ({})))?.error ?? 'Failed')
  }

  async function seedCompetition(worldId: string, countryId: string, starterLeagueId: string): Promise<SeedResult> {
    const res = await authedFetch(`/api/admin/worlds/${worldId}/seed-competition`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ country_id: countryId, starter_league_id: starterLeagueId }),
    })
    if (!res.ok) throw new Error((await res.json().catch(() => ({})))?.error ?? 'Failed')
    return res.json()
  }

  // ---------- Manager reads ----------

  async function listMyCompetitions(): Promise<League[]> {
    const res = await authedFetch('/api/competitions')
    if (!res.ok) return []
    const body = await res.json()
    return body.competitions ?? []
  }

  async function getFixtures(leagueId: string, matchday?: number): Promise<Fixture[]> {
    const url = matchday
      ? `/api/competitions/${leagueId}/fixtures?matchday=${matchday}`
      : `/api/competitions/${leagueId}/fixtures`
    const res = await authedFetch(url)
    if (!res.ok) return []
    const body = await res.json()
    return body.fixtures ?? []
  }

  async function getStandings(leagueId: string): Promise<Standings | null> {
    const res = await authedFetch(`/api/competitions/${leagueId}/standings`)
    if (!res.ok) return null
    return res.json()
  }

  // ---------- Global club-name pools (admin) ----------

  async function listClubNameParts(): Promise<ClubNamePools> {
    const res = await authedFetch('/api/admin/club-name-parts')
    if (!res.ok) return { stems: [], suffixes: [] }
    return res.json()
  }

  async function addClubNamePart(kind: 'stem' | 'suffix', value: string): Promise<void> {
    const res = await authedFetch('/api/admin/club-name-parts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind, value }),
    })
    if (!res.ok) throw new Error((await res.json().catch(() => ({})))?.error ?? 'Failed')
  }

  async function removeClubNamePart(kind: 'stem' | 'suffix', value: string): Promise<void> {
    const res = await authedFetch(`/api/admin/club-name-parts/${kind}/${encodeURIComponent(value)}`, {
      method: 'DELETE',
    })
    if (!res.ok) throw new Error((await res.json().catch(() => ({})))?.error ?? 'Failed')
  }

  return {
    createCountry,
    listCountries,
    createLeague,
    listLeagues,
    updateAdjacency,
    seedCompetition,
    listMyCompetitions,
    getFixtures,
    getStandings,
    listClubNameParts,
    addClubNamePart,
    removeClubNamePart,
  }
}