export interface Country {
  id: string
  world_id: string
  code: string
  name: string
}

export interface ClubRef {
  id: string
  name?: string
  short?: string
}

export interface CompetitionRef {
  id: string
  name?: string
}

export interface CountryRef {
  id: string
  name?: string
  code?: string
}

export interface LeagueRef {
  id: string
  name?: string
}

export interface SeasonRef {
  id: string
  label?: string
  number?: number
  status?: string
}

export interface League {
  id: string
  world_id: string
  country: CountryRef
  name: string
  tier: number
  team_count: number
  status: string
  promotions: number
  relegations: number
  promotes_to: LeagueRef | null
  relegates_to: LeagueRef | null
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
  world_id?: string
  competition: CompetitionRef
  home_club: ClubRef
  away_club: ClubRef
  matchday: number
  scheduled_at: string
  status: string
  home_score: number | null
  away_score: number | null
  /** Set once the match has completed — pass to /api/matches/{id}/events to watch it. */
  match_id?: string
}

export interface StandingRow {
  club: ClubRef
  played: number
  won: number
  drawn: number
  lost: number
  goals_for: number
  goals_against: number
  points: number
}

export interface Standings {
  season: SeasonRef
  rows: StandingRow[]
}

export interface FixtureMatchday {
  matchday: number
  scheduled_at: string
  gameweek: number
  fixtures: Fixture[]
}

export interface FixtureWeek {
  week: number
  first_day: string
  matchdays: FixtureMatchday[]
}

export interface SeasonCalendar {
  season: SeasonRef
  weeks: FixtureWeek[]
}

export interface ClubNamePools {
  stems: string[]
  suffixes: string[]
}

// ---------- Domestic cups (IM04) ----------

export interface Cup {
  id: string
  world_id: string
  country: CountryRef
  name: string
  competition_type: string
  status: string
  prize_pool: number
  format: string
  is_home_and_away: boolean
  first_tier_bye: number
  survivor_threshold: number
}

export interface CupTie {
  id: string
  home_club: ClubRef
  away_club: ClubRef
  scheduled_at: string
  status: string
  home_score: number | null
  away_score: number | null
  winner?: ClubRef
}

export interface CupRound {
  round: number
  scheduled_at?: string | null
  ties: CupTie[]
  byes: ClubRef[]
}

export interface CupCampaign {
  cup: Cup
  qualification_rules: unknown
  late_entry_round: number
  total_rounds: number
  season?: SeasonRef | null
  champion?: ClubRef | null
  rounds: CupRound[]
}

export interface RescheduleResult {
  calendar_updated: boolean
  matchdays_re_paced?: number
  fixtures_moved?: number
}

export function useCompetition() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()

  // ---------- Admin (world-scoped via body) ----------

  async function createCountry(worldId: string, code: string, name: string): Promise<Country> {
    const res = await $api.get(`${apiBase}/api/admin/countries`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ world_id: worldId, code, name }),
    })
    if (!res.ok) throw new Error((await res.json().catch(() => ({})))?.error ?? 'Failed')
    return res.json()
  }

  async function listCountries(worldId: string): Promise<Country[]> {
    const body = await $api.get(`${apiBase}/api/admin/countries?world_id=${worldId}`)
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
    return await $api.post(`${apiBase}/api/admin/leagues`, params)
  }

  async function listLeagues(worldId: string): Promise<League[]> {
    const body = await $api.get(`${apiBase}/api/admin/leagues?world_id=${worldId}`)
    return body.leagues ?? []
  }

  async function updateAdjacency(leagueId: string, body: {
    promotes_to?: string | null
    relegates_to?: string | null
  }): Promise<void> {
    await $api.patch(`${apiBase}/api/admin/leagues/${leagueId}/adjacency`, body)
  }

  async function seedCompetition(worldId: string, countryId: string, starterLeagueId: string): Promise<SeedResult> {
    return await $api.post(`${apiBase}/api/admin/worlds/${worldId}/seed-competition`,{ country_id: countryId, starter_league_id: starterLeagueId })
  }

  // ---------- Manager reads ----------

  async function listMyCompetitions(): Promise<League[]> {
    const res = await $api.get(`${apiBase}/api/competitions`)
    if (!res.ok) return []
    const body = await res.json()
    return body.competitions ?? []
  }

  async function getFixtures(leagueId: string, matchday?: number): Promise<Fixture[]> {
    const url = matchday
      ? `/api/competitions/${leagueId}/fixtures?matchday=${matchday}`
      : `/api/competitions/${leagueId}/fixtures`
    const body = await $api.get(`${apiBase}${url}`)
    return body.fixtures ?? []
  }

  async function getSeasonCalendar(leagueId: string): Promise<SeasonCalendar | null> {
    return await $api.get(`${apiBase}/api/competitions/${leagueId}/calendar`)
  }

  async function getClubFixtures(clubId: string): Promise<Fixture[]> {
    const body = await $api.get(`${apiBase}/api/clubs/${clubId}/fixtures`)
    
    return body.fixtures ?? []
  }

  async function getStandings(leagueId: string): Promise<Standings | null> {
    return await $api.get(`${apiBase}/api/competitions/${leagueId}/standings`)
  }

  // ---------- Caps (IM04) ----------

  async function createCup(params: {
    world_id: string
    country_id: string
    name: string
    first_tier_bye: number
    survivor_threshold: number
    prize_pool?: number
  }): Promise<Cup> {
   return await $api.post(`${apiBase}/api/admin/cups`, params)
  }

  async function startCupCampaign(worldId: string, countryId: string, cupId: string): Promise<{ id: string }> {
    return await $api.post(`${apiBase}/api/admin/worlds/${worldId}/countries/${countryId}/cups/${cupId}/campaign`)
  }

  async function listMyCups(): Promise<Cup[]> {
    const body = await $api.get(`${apiBase}/api/cups`)
    return body.cups ?? []
  }

  async function getCup(cupId: string): Promise<CupCampaign | null> {
    return await $api.get(`${apiBase}/api/cups/${cupId}`)
  }

  // ---------- IM05 scheduling (admin: allowed weekdays + re-pacing) ----------

  async function updateLeagueScheduling(leagueId: string, allowedWeekdays: number[]): Promise<RescheduleResult> {
    return await $api.patch(`${apiBase}/api/admin/leagues/${leagueId}/scheduling`, { allowed_weekdays: allowedWeekdays })
  }

  async function updateCupScheduling(cupId: string, allowedWeekdays: number[]): Promise<RescheduleResult> {
    return await $api.patch(`${apiBase}/api/admin/cups/${cupId}/scheduling`, { allowed_weekdays: allowedWeekdays })
  }

  async function updateCountryScheduling(worldId: string, countryId: string, allowedWeekdays: number[]): Promise<RescheduleResult> {
    return await $api.patch(`${apiBase}/api/admin/worlds/${worldId}/countries/${countryId}/scheduling`, { allowed_weekdays: allowedWeekdays })
  }

  // ---------- Club-name pools (admin: country-scoped, '' = global) ----------

  async function listClubNameParts(countryCode = ''): Promise<ClubNamePools> {
    const qs = countryCode ? `?country_code=${encodeURIComponent(countryCode)}` : ''
    const body = await $api.get(`${apiBase}/api/admin/club-name-parts${qs}`)
    return { stems: body.stems ?? [], suffixes: body.suffixes ?? [] }
  }

  async function addClubNamePart(kind: 'stem' | 'suffix', value: string, countryCode = ''): Promise<void> {
    return await $api.post(`${apiBase}/api/admin/club-name-parts`,{ kind, value, country_code: countryCode })
  }

  async function removeClubNamePart(kind: 'stem' | 'suffix', value: string, countryCode = ''): Promise<void> {
    const qs = countryCode ? `?country_code=${encodeURIComponent(countryCode)}` : ''
    return await $api.delete(`${apiBase}api/admin/club-name-parts/${kind}/${encodeURIComponent(value)}${qs}`)
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
    getSeasonCalendar,
    getClubFixtures,
    getStandings,
    listClubNameParts,
    addClubNamePart,
    removeClubNamePart,
    createCup,
    startCupCampaign,
    listMyCups,
    getCup,
    updateLeagueScheduling,
    updateCupScheduling,
    updateCountryScheduling,
  }
}