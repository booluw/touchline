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
}

export const useMatchStore = defineStore('match', {
  state: () => ({
    fixture: null as null | FixtureHeader,
    match: null as null | MatchHeader,
    events: [] as MatchEvent[],
    stuck: false as boolean,
    error: '',
    unwatch: null as null | (() => void),
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

      if (!view.match) return
      const eventsRes = await authedFetch(`/api/matches/${view.match.id}/events`)
      if (!eventsRes.ok) return
      const body = await eventsRes.json() as { events: MatchEvent[] }
      this.events = body.events ?? []
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

        this.applyTick(tick)
      })
      socket.connect()
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
        id: this.match.id,
        status: tick.status,
        minute: tick.minute,
        home_score: tick.home_score,
        away_score: tick.away_score,
      }

      if (!tick.events?.length) return
      const seen = new Set(this.events.map((e) => e.id))
      for (const ev of tick.events) {
        if (!seen.has(ev.id)) {
          this.events.push(ev)
          seen.add(ev.id)
        }
      }
      this.events.sort((a, b) => a.sequence - b.sequence)
    },

    disconnect() {
      if (this.unwatch) {
        this.unwatch()
        this.unwatch = null
      }
      this.stuck = false
    },
  },
})