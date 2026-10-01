// Pinia store: match.ts — the S04-03 matchday event feed. Holds the header
// (fixture + match), the ordered persisted event list, and the live clock. The
// screen loads the persisted feed over REST (catch-up / quick result), then the
// sharing useSocket session streams later minutes as match_tick envelopes. Both
// sources are the identical match_events rows; incoming ticks are deduped by
// event id so replays, redelivery, and reconnect can never corrupt the feed.
export type EventType =
  | 'kickoff' | 'goal' | 'assist' | 'chance_created'
  | 'yellow_card' | 'red_card' | 'injury' | 'substitution'
  | 'penalty_awarded' | 'penalty_scored' | 'penalty_missed'
  | 'half_time' | 'full_time'
  // IM34 positional-engine extras (source 'pitchsim'): never change a score.
  | 'shot' | 'save' | 'tackle' | 'foul' | 'corner' | 'offside'

export interface MatchEvent {
  id: string
  match: { id: string }
  sequence: number
  minute: number
  type: EventType
  club?: { id: string; name?: string }
  player?: { id: string; name?: string }
  related_player?: { id: string; name?: string }
  detail?: { commentary?: string; detail?: string }
  source?: 'matchsim' | 'pitchsim'
  // Position inside the minute in match milliseconds (absent without the 2D engine).
  offset_millis?: number
}

// IM34 simulation track: keyframes per match minute. Coordinates are 0..1000
// (x goal line to goal line, y touchline to touchline); [-1,-1] = off the pitch.
export interface TrackFrame { t: number; b: [number, number, number]; p: [number, number][] }
export interface TrackMinute {
  minute: number
  lineup: string[]
  frames: TrackFrame[]
  cues: { seq: number; t: number }[]
  passes: [number, number]
}
interface TrackResponse {
  pacing_millis: number
  last_minute: number
  players: Record<string, string>
  minutes: TrackMinute[]
}

export interface FixtureHeader {
  id: string
  world_id: string
  competition: { id: string; name?: string }
  home_club: { id: string; name?: string; short?: string }
  away_club: { id: string; name?: string; short?: string }
  matchday: number
  scheduled_at: string
  status: string
}

export interface MatchHeader {
  id: string
  status: string
  minute: number
  home_score: number
  away_score: number
  visual?: boolean
  pacing_millis?: number
}

export interface MatchTickPayload {
  match: { id: string }
  fixture: { id: string }
  minute: number
  status: string
  home_club: { id: string; name?: string }
  away_club: { id: string; name?: string }
  home_score: number
  away_score: number
  events?: MatchEvent[]
  track?: TrackMinute[]
}

// Feed order: minute, position in the minute, sequence (same as the server).
function feedOrder(a: MatchEvent, b: MatchEvent) {
  return a.minute - b.minute || (a.offset_millis ?? 0) - (b.offset_millis ?? 0) || a.sequence - b.sequence
}

export const useMatchStore = defineStore('match', {
  state: () => ({
    fixture: null as null | FixtureHeader,
    match: null as null | MatchHeader,
    events: [] as MatchEvent[],
    stuck: false as boolean,
    error: '',
    unwatch: null as null | (() => void),
    // IM34: movement by minute, player names for the lineups, the last minute
    // held, and the no-tick polling fallback.
    track: {} as Record<number, TrackMinute>,
    trackPlayers: {} as Record<string, string>,
    trackLast: 0,
    lastTickAt: 0,
    poll: null as null | ReturnType<typeof setInterval>,
  }),

  actions: {
    // load() fetches the persistent feed: header then the full event list. It
    // is the quick-result path and the live screen's catch-up before ticks.
    async load(fixtureId: string) {
      const { authedFetch } = useAuth()
      this.error = ''

      const headerRes = await authedFetch(`/api/fixtures/${fixtureId}`)
      if (!headerRes.ok) {
        this.error = headerRes.status === 404 ? 'Fixture not found.' : 'Could not load this match right now.'
        return
      }
      const view = await headerRes.json() as { fixture: FixtureHeader; match?: MatchHeader }
      this.fixture = view.fixture
      this.match = view.match ?? null
      this.events = []
      this.track = {}
      this.trackPlayers = {}
      this.trackLast = 0

      if (!view.match) return
      await this.loadEvents()
      if (view.match.visual) await this.loadTrack()
    },

    async loadEvents() {
      if (!this.match) return
      const { authedFetch } = useAuth()
      const eventsRes = await authedFetch(`/api/matches/${this.match.id}/events`)
      if (!eventsRes.ok) return
      const body = await eventsRes.json() as { events: MatchEvent[] }
      this.events = body.events ?? []
    },

    // loadTrack() fetches every minute of movement this screen does not hold
    // yet. It is the catch-up for a late join, a reconnect, and the golden-goal
    // block (which is too large to ride on a tick).
    async loadTrack() {
      if (!this.match?.visual) return
      const { authedFetch } = useAuth()
      const res = await authedFetch(`/api/matches/${this.match.id}/track?from=${this.trackLast + 1}`)
      if (!res.ok) return
      const body = await res.json() as TrackResponse
      this.addTrack(body.minutes ?? [])
      this.trackPlayers = { ...this.trackPlayers, ...body.players }
    },

    addTrack(minutes: TrackMinute[]) {
      for (const m of minutes) {
        this.track[m.minute] = m
        if (m.minute > this.trackLast) this.trackLast = m.minute
      }
    },

    // refresh() re-reads the header, feed and missing track over REST. Live
    // ticks are published per world, so a viewer from another world (or one
    // whose socket is down) is kept current by this instead.
    async refresh(fixtureId: string) {
      const { authedFetch } = useAuth()
      const res = await authedFetch(`/api/fixtures/${fixtureId}`)
      if (!res.ok) return
      const view = await res.json() as { match?: MatchHeader }
      if (!view.match) return
      this.match = view.match
      await this.loadEvents()
      await this.loadTrack()
    },

    // connect() starts streaming match_tick envelopes for the watched match on
    // the shared socket session. Idempotent per fixture: re-watching the same
    // screen replaces the old handler instead of doubling it.
    connect(fixtureId: string) {
      const socket = useSocket()
      if (this.stuck) return
      this.disconnect()

      this.unwatch = socket.on('match_tick', (event) => {
        const tick = event.payload as MatchTickPayload
        if (!tick || tick.fixture?.id !== fixtureId) return
        if (!this.match) return

        this.lastTickAt = Date.now()
        this.applyTick(tick)
      })
      socket.connect()

      const pacing = this.match?.pacing_millis || 20000
      this.lastTickAt = Date.now()
      this.poll = setInterval(() => {
        if (this.match?.status === 'in_progress' && Date.now() - this.lastTickAt > pacing * 1.5) {
          this.refresh(fixtureId).catch(() => { })
        }
      }, pacing)
    },

    async watch(fixtureId: string) {
      await this.load(fixtureId)
      if (!this.match) return
      this.connect(fixtureId)
    },

    applyTick(tick: MatchTickPayload) {
      if (!this.match) return
      if (tick.status === 'completed') this.stuck = true

      this.match = {
        ...this.match,
        id: this.match.id,
        status: tick.status,
        minute: tick.minute,
        home_score: tick.home_score,
        away_score: tick.away_score,
      }

      const seen = new Set(this.events.map((e) => e.id))
      for (const ev of tick.events ?? []) {
        if (!seen.has(ev.id)) {
          this.events.push(ev)
          seen.add(ev.id)
        }
      }
      this.events.sort(feedOrder)

      if (!this.match.visual) return
      // A tick carries the next minute of movement. Anything else — a gap after
      // a reconnect, the multi-minute golden-goal block, the final tick — is
      // fetched.
      const next = tick.track?.[0]?.minute
      if (next === this.trackLast + 1) this.addTrack(tick.track!)
      const needed = Math.max(tick.minute, ...this.events.map((e) => e.minute))
      if (needed > this.trackLast) this.loadTrack().catch(() => { })
    },

    disconnect() {
      if (this.unwatch) {
        this.unwatch()
        this.unwatch = null
      }
      if (this.poll) {
        clearInterval(this.poll)
        this.poll = null
      }
      this.stuck = false
    },
  },
})