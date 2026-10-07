<script setup lang="ts">
/**
 * /ui — living catalogue of the Touchline design-system components (app/components/ui).
 * No API calls: every fixture below mirrors a real response shape (IM38–IM46,
 * backend/internal/apidocs/openapi.yaml) and shows the mapping into component props.
 */
import type {
  DisplayDensity, FinancialHealthStage, LineupSlot, OfferTerm, PositionFit,
  SegmentOption, ShellClub, ShellFixture, TableColumn, TableSort, WhyFactor,
} from '~/types/ui/design'
import { FINANCIAL_HEALTH_STAGES } from '~/types/ui/design'

definePageMeta({ layout: false })
useHead({ title: 'UI components · Touchline' })

// ─── API fixtures (dummy data, real shapes) ──────────────────────────────────

/** GET /api/dashboard (IM38: counts, summary, explanation on board items). */
const dashboard = {
  urgent: [
    {
      id: 'board:cv', priority: 'urgent', category: 'board',
      title: 'Board confidence is critically low', description: 'Confidence at 22/100. You are close to being sacked.',
      created_at: '2026-12-14T09:00:00Z', action: { kind: 'view_board', club_id: 'cv' },
      explanation: {
        subject: 'board_confidence', score: 22,
        factors: [
          { label: 'league performance', delta: 9 }, { label: 'promise fulfillment', delta: 2 },
          { label: 'financial health', delta: 1 }, { label: 'board relationship', delta: 4 },
          { label: 'club dna alignment', delta: 3 }, { label: 'supporter sentiment', delta: 2 },
          { label: 'replacement pressure', delta: 1 },
        ],
      },
    },
    {
      id: 'bids:b1', priority: 'urgent', category: 'bids',
      title: 'Bid on Ademola Kintu for £1,250,000.00', description: 'Ostrava Union want your player Ademola Kintu (£1,250,000.00 offer, round 1).',
      created_at: '2026-12-13T18:00:00Z', action: { kind: 'respond_bid', bid_id: 'b1' },
    },
  ],
  important: [
    {
      id: 'morale:p9', priority: 'important', category: 'morale',
      title: 'Ademola Kintu has submitted a transfer request', description: 'They want out. Respond to the request or try to reassure them.',
      created_at: '2026-12-14T08:00:00Z', action: { kind: 'view_player', player_id: 'p9' },
    },
  ],
  interesting: [
    {
      id: 'standings:cv', priority: 'interesting', category: 'standings',
      title: 'Calder Vale FC sit 9th in the 2026/27', description: '18 played, 27 points.',
      created_at: '2026-12-14T08:00:00Z', action: { kind: 'view_standings', club_id: 'cv' },
    },
  ],
  counts: { urgent: 2, important: 1, interesting: 1, by_category: { board: 1, bids: 1, morale: 1, standings: 1 } },
  summary: [{
    club_id: 'cv',
    board: { confidence: 22, change: -9 },
    morale: { average: 0.62, unhappy: 3, players: 24 },
    finance: { cash: 142_000_000, weekly_wage_bill: 3_860_000, season_wage_budget: 2_500_000_000 },
    league: { position: 9, points: 27, played: 18, season_label: '2026/27' },
  }],
}

/** GET /api/clubs/:id/next-fixture (IM39 adds `club`). */
const nextFixture = {
  fixture: { id: 'f1', scheduled_at: '2026-12-16T15:00:00Z', matchday: 19, competition: { id: 'l1', name: 'Second Division North' } },
  gameweek: 19, home_or_away: 'home', derby: false, golden_goal: false,
  club: { club: { id: 'cv', name: 'Calder Vale FC', short: 'CV' }, tier: 3, reputation: 42, league_position: 9, form: { form_string: 'WWDLW', current_rating: 6.8 } },
  opponent: { club: { id: 'br', name: 'Brennock Rovers', short: 'BRE' }, tier: 3, reputation: 51, league_position: 4, form: { form_string: 'WWWDW', current_rating: 7.4 } },
}

/** GET /api/clubs/:id/players rows (IM40 adds nationality, age, contract). */
const roster = [
  { player: { id: 'p9', name: 'Ademola Kintu' }, position: 'ST', squad_role: 'key_player', morale: 0.22, overall: 75, nationality: { code: 'NGA', name: 'Nigeria' }, age: 24, date_of_birth: '2002-03-11', contract: { weekly_wage: 420_000, end_date: '2028-06-30' }, transfer_request_status: 'pending' },
  { player: { id: 'p4', name: 'Viktor Halvorsen' }, position: 'CB', squad_role: 'key_player', morale: 0.71, overall: 65, nationality: { code: 'NOR', name: 'Norway' }, age: 31, date_of_birth: '1995-01-02', contract: { weekly_wage: 510_000, end_date: '2027-06-30' } },
  { player: { id: 'p7', name: 'Lukas Brandt' }, position: 'CM', squad_role: 'rotation', morale: 0.48, overall: 66, nationality: { code: 'GER', name: 'Germany' }, age: 22, date_of_birth: '2004-05-20', contract: { weekly_wage: 280_000, end_date: '2029-06-30' } },
  { player: { id: 'p1', name: 'Kenji Morita' }, position: 'GK', squad_role: 'key_player', morale: 0.66, overall: 60, nationality: { code: 'JPN', name: 'Japan' }, age: 28, date_of_birth: '1998-08-14', contract: null },
]

/** GET /api/clubs/:id/players/:pid → dossier excerpt (IM37). */
const dossier = {
  attribute_values: { technical: { finishing: 17, passing: 10 }, mental: { composure: 14, decisions: 11, work_rate: 7 }, physical: { pace: 15, strength: 12 } },
  condition: { fitness: 0.92, sharpness: 0.64, fatigue: 0.78, morale: 0.22 },
  personality: { professionalism: 12, ambition: 18, loyalty: 6, ego: 15 },
}

/** GET /api/clubs/:id/finances (IM42 adds health, season_breakdown, cash_history). */
const finances = {
  cash: 142_000_000,
  health: { stage: 'warning', started_at: '2026-11-20T00:00:00Z' } as { stage: string, started_at: string } | null,
  season_breakdown: {
    season: 2026,
    revenue: [{ label: 'ticket_sales', amount: 61_200_000 }, { label: 'broadcasting', amount: 48_000_000 }, { label: 'sponsorship', amount: 21_000_000 }],
    expenses: [{ label: 'wages', amount: 92_600_000 }, { label: 'transfer_fee', amount: 31_000_000 }, { label: 'staff_cost', amount: 17_300_000 }],
  },
  cash_history: [
    { month: '2026-07', balance: 183_000_000 }, { month: '2026-08', balance: 171_000_000 }, { month: '2026-09', balance: 164_000_000 },
    { month: '2026-10', balance: 158_000_000 }, { month: '2026-11', balance: 149_000_000 }, { month: '2026-12', balance: 142_000_000 },
  ],
}

/** GET /api/managers/me/board (IM43 adds persona, members, confidence_history). */
const board = {
  confidence: 41, persona: 'financial_conservative',
  members: [{ name: 'Ruth Ellery', agenda: 'Keep wages inside the structure', influence: 64 }],
  confidence_history: [62, 60, 57, 55, 52, 50, 47, 44, 41].map((total_score, i) => ({ world_tick: i + 1, total_score })),
}

/** GET /api/transfers/bids → one incoming thread (IM44 adds rounds). */
const bid = {
  id: 'b2', status: 'countered', round: 2, fee: 24_000_000,
  bidding_club: { name: 'Calder Vale FC' }, selling_club: { name: 'Harlow Athletic' },
  rounds: [
    { round: 1, proposed_by: 'buying_club', created_at: '2026-12-12T10:00:00Z', terms: { fee: 18_000_000, weekly_wage: 120_000, contract_length_months: 36 } },
    { round: 2, proposed_by: 'selling_club', created_at: '2026-12-13T10:00:00Z', terms: { fee: 24_000_000, weekly_wage: 120_000, contract_length_months: 36, sell_on_percentage: 20 } },
  ],
}

/** GET /api/matches/:id/events → stats (IM45). */
const matchStats = {
  home: { goals: 2, chances_created: 7, yellow_cards: 1, red_cards: 0, substitutions: 3, penalties_awarded: 0, injuries: 1 },
  away: { goals: 1, chances_created: 4, yellow_cards: 3, red_cards: 1, substitutions: 2, penalties_awarded: 1, injuries: 0 },
}

/** GET /api/clubs/:id/lineup (IM46 adds player_position + fit). */
const lineup = {
  formation: '4-3-3',
  slots: [
    { slot: 0, position: 'GK', player: { name: 'Morita' }, player_position: 'GK', fit: 1 },
    { slot: 1, position: 'LB', player: { name: 'Claes' }, player_position: 'RB', fit: 0.75 },
    { slot: 2, position: 'CB', player: { name: 'Halvorsen' }, player_position: 'CB', fit: 1 },
    { slot: 3, position: 'CB', player: { name: 'Novák' }, player_position: 'CB', fit: 1 },
    { slot: 4, position: 'RB', player: { name: 'Sesay' }, player_position: 'RB', fit: 1 },
    { slot: 5, position: 'CM', player: { name: 'Takyi' }, player_position: 'DM', fit: 0.75 },
    { slot: 6, position: 'CM', player: { name: 'Brandt' }, player_position: 'CM', fit: 1 },
    { slot: 7, position: 'CM', player: { name: 'Moreau' }, player_position: 'CM', fit: 1 },
    { slot: 8, position: 'RW', player: { name: 'Ventura' }, player_position: 'RW', fit: 1 },
    { slot: 9, position: 'ST', player: { name: 'Kintu' }, player_position: 'ST', fit: 1 },
    { slot: 10, position: 'LW', player: { name: 'Reid' }, player_position: 'CB', fit: 0.3 },
  ],
}

// ─── API → component mappers (copy these into composables) ───────────────────

/** Explanation factors ({label, delta}) → WhyBreakdown factors. */
const toWhy = (exp?: { factors: { label: string, delta: number }[] }): WhyFactor[] =>
  (exp?.factors ?? []).map(f => ({ label: f.label.charAt(0).toUpperCase() + f.label.slice(1), value: f.delta }))

/** Engine fit (1 / 0.75 / 0.3 / 0.05) → ring colour. */
const toFit = (fit?: number): PositionFit =>
  fit === undefined ? 'out' : fit >= 1 ? 'natural' : fit >= 0.75 ? 'capable' : fit >= 0.3 ? 'awkward' : 'out'

/** finances.health → ladder stage (null = Healthy; stages past emergency collapse to the last rung). */
const toStage = (h: { stage: string } | null): FinancialHealthStage =>
  ({ warning: 'Warning', restriction: 'Restriction', emergency: 'Emergency' } as Record<string, FinancialHealthStage>)[h?.stage ?? ''] ??
  (h ? 'Administration risk' : 'Healthy')

/** Money is in pence on the wire. */
const money = (p: number) => {
  const v = Math.abs(p) / 100
  const s = v >= 1e6 ? `£${(v / 1e6).toFixed(2)}m` : v >= 1e3 ? `£${Math.round(v / 1e3)}k` : `£${v}`
  return p < 0 ? `−${s}` : s
}
const pct = (x: number) => Math.round(x * 100)
const tierOf = { urgent: 'urgent', important: 'important', interesting: 'info' } as const

// ─── Derived demo state ──────────────────────────────────────────────────────

const club: ShellClub = { name: nextFixture.club.club.name, short: nextFixture.club.club.short, league: nextFixture.fixture.competition.name }
const fixture: ShellFixture = { context: `Next · League · ${nextFixture.home_or_away === 'home' ? 'Home' : 'Away'}`, opponent: nextFixture.opponent.club.name, opponentShort: nextFixture.opponent.club.short, countdown: '2d 06h' }
const nav = GAME_NAV.map(i => ({
  ...i,
  count: ({ Squad: dashboard.counts.by_category.morale, Transfers: dashboard.counts.by_category.bids, Club: dashboard.counts.by_category.board } as Record<string, number | undefined>)[i.label],
  countTone: (i.label === 'Transfers' ? 'important' : 'urgent') as 'important' | 'urgent',
}))
const summary = dashboard.summary[0]!
const density = ref<DisplayDensity>('standard')
const openWhy = ref<string | null>('board:cv')
const sections = [
  { tier: 'urgent', items: dashboard.urgent },
  { tier: 'important', items: dashboard.important },
  { tier: 'interesting', items: dashboard.interesting },
] as const

type RosterKey = 'name' | 'position' | 'age' | 'overall' | 'morale' | 'contract' | 'wage'
const columns: TableColumn<RosterKey>[] = [
  { key: 'name', label: 'Player', width: '2.4fr', sortable: true },
  { key: 'position', label: 'Pos', width: '60px' },
  { key: 'age', label: 'Age', align: 'right', width: '60px', sortable: true },
  { key: 'overall', label: 'Rating', align: 'right', width: '70px', sortable: true },
  { key: 'morale', label: 'Morale', align: 'right', width: '80px', sortable: true },
  { key: 'contract', label: 'Contract', align: 'right', width: '100px', advanced: true },
  { key: 'wage', label: 'Wage/wk', align: 'right', width: '90px', sortable: true },
]
const sort = ref<TableSort<RosterKey>>({ key: 'overall', direction: 'desc' })
const rows = computed(() => {
  const r = roster.map(p => ({
    id: p.player.id, name: p.player.name, position: p.position, age: p.age, overall: p.overall,
    morale: pct(p.morale), contract: p.contract?.end_date.slice(0, 7) ?? '—', wage: p.contract?.weekly_wage ?? 0,
    nat: p.nationality.code, flag: p.transfer_request_status ? 'Wants out' : p.contract ? '' : 'No contract',
  }))
  const { key, direction } = sort.value
  return r.sort((a, b) => {
    const x = a[key], y = b[key]
    const c = typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y))
    return direction === 'asc' ? c : -c
  })
})
const selectedRow = ref<string>()

const pitchSlots: LineupSlot[] = (() => {
  // 4-3-3 coordinates; y: 0 = attack, 100 = own goal.
  const xy = [[50, 90], [15, 70], [38, 74], [62, 74], [85, 70], [30, 48], [50, 52], [70, 48], [82, 22], [50, 14], [18, 22]]
  return lineup.slots.map((s, i) => ({ id: String(s.slot), x: xy[i]![0]!, y: xy[i]![1]!, rating: 12 + Math.round((s.fit ?? 0) * 4), fit: toFit(s.fit), label: s.player?.name }))
})()
const selectedSlot = ref<string>()

const offerTerms = (n: number): OfferTerm[] => {
  const cur = bid.rounds[n]!.terms, prev = bid.rounds[n - 1]?.terms as typeof cur | undefined
  return [
    { label: 'Fee', value: money(cur.fee), previous: prev ? money(prev.fee) : undefined },
    { label: 'Wage / wk', value: money(cur.weekly_wage) },
    { label: 'Length', value: `${cur.contract_length_months / 12} yrs` },
    ...('sell_on_percentage' in cur ? [{ label: 'Sell-on', value: `${cur.sell_on_percentage}%`, previous: 'None' }] : []),
  ]
}

const stage = ref<FinancialHealthStage>(toStage(finances.health))
const stageOptions: SegmentOption<FinancialHealthStage>[] = FINANCIAL_HEALTH_STAGES.map(s => ({ value: s, label: s }))
const mode = ref<'simple' | 'standard' | 'advanced'>('standard')
const fee = ref(240)
const sellOn = ref(true)
const loanChip = ref(false)
const u21Chip = ref(true)
const handle = ref('millgate_manager')
const email = ref('not-an-email')
const sheetOpen = ref(false)
const toast = useToaster()

const tones = ['urgent', 'important', 'info', 'pos', 'neg', 'neutral', 'muted', 'outline'] as const
</script>

<template>
  <div class="font-geist min-h-dvh bg-bg text-body text-t1">
    <div class="mx-auto flex max-w-[1240px] flex-col gap-10 px-4 py-8 md:px-6">
      <header class="flex flex-col gap-1">
        <span class="num text-label uppercase tracking-[.08em] text-t3">Touchline · design system</span>
        <h1 class="text-page">UI components</h1>
        <p class="max-w-[720px] text-t2">
          Every component in <code class="num">app/components/ui</code>, fed with dummy data shaped exactly like the API
          responses (IM38–IM46). Each section shows the response it reads and the mapping into props. No network calls.
        </p>
      </header>

      <!-- App shell -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">App shell</h2>
        <p class="text-t2">
          <code class="num">UiAppShell</code> = <code class="num">UiNavSidebar</code> + <code class="num">UiTopBar</code> + content,
          and <code class="num">UiBottomNav</code> under 768px. Nav counts come from <code class="num">dashboard.counts.by_category</code>;
          the club and countdown from <code class="num">next-fixture</code>.
        </p>
        <div class="overflow-hidden rounded-card border border-line [&>div]:h-[420px]">
          <UiAppShell v-model:density="density" :club :items="nav" :fixture :inbox-count="dashboard.counts.urgent"
            season="Season 2026/27 · Matchday 19" date="Sat 14 Dec 2026" @open-inbox="toast.info('Inbox opened')">
            <UiEmptyState title="Page content goes here" description="Pages render in the default slot." />
          </UiAppShell>
        </div>
        <pre class="ui-code">&lt;UiAppShell v-model:density="density" :club :items="nav" :fixture
  :inbox-count="dashboard.counts.urgent" season="…" date="…"&gt;…&lt;/UiAppShell&gt;</pre>
      </section>

      <!-- Home inbox -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Home inbox · TierHeader, AttentionItem, WhyBreakdown</h2>
        <p class="text-t2">
          One <code class="num">UiTierHeader</code> per dashboard section; each item becomes a <code class="num">UiAttentionItem</code>.
          Items with an <code class="num">explanation</code> are explainable and open a <code class="num">UiWhyBreakdown</code> via <code class="num">toWhy()</code>.
        </p>
        <div class="flex flex-col gap-5">
          <div v-for="sec in sections" :key="sec.tier" class="flex flex-col gap-2">
            <UiTierHeader :tier="tierOf[sec.tier]" :count="dashboard.counts[sec.tier]" />
            <UiCard flush>
              <UiAttentionItem v-for="it in sec.items" :key="it.id" :tier="tierOf[sec.tier]" :tag="it.category"
                :title="it.title" :consequence="it.description" action-label="Open" :explainable="'explanation' in it"
                @action="toast.info(`Route on action.kind = ${it.action.kind}`)" @why="openWhy = openWhy === it.id ? null : it.id">
                <UiWhyBreakdown v-if="'explanation' in it && openWhy === it.id" subject="Board confidence"
                  :value="it.explanation?.score" :factors="toWhy(it.explanation)" />
              </UiAttentionItem>
            </UiCard>
          </div>
        </div>
        <pre class="ui-code">&lt;UiAttentionItem :tier="tierOf[section]" :tag="item.category" :title="item.title"
  :consequence="item.description" :explainable="!!item.explanation" @why="…"&gt;
  &lt;UiWhyBreakdown subject="Board confidence" :value="item.explanation.score" :factors="toWhy(item.explanation)" /&gt;
&lt;/UiAttentionItem&gt;</pre>
      </section>

      <!-- Summary rail -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Summary rail · StatCard</h2>
        <p class="text-t2">From <code class="num">dashboard.summary[0]</code>. Deltas colour by sign unless <code class="num">deltaTone</code> is set.</p>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <UiStatCard label="BOARD CONFIDENCE" :value="summary.board.confidence" :delta="summary.board.change"
            :progress="summary.board.confidence" progress-tone="important" caption="Review below 35" />
          <UiStatCard label="SQUAD MORALE" :value="pct(summary.morale.average)" :progress="pct(summary.morale.average)"
            progress-tone="pos" :caption="`${summary.morale.unhappy} unhappy of ${summary.morale.players}`" />
          <UiStatCard label="CASH" :value="money(summary.finance.cash)" :caption="`Wages ${money(summary.finance.weekly_wage_bill)}/wk`" />
          <UiStatCard label="LEAGUE" :value="`${summary.league.position}th`" :caption="`${summary.league.points} pts · ${summary.league.played} played`" />
        </div>
        <pre class="ui-code">&lt;UiStatCard label="BOARD CONFIDENCE" :value="s.board.confidence" :delta="s.board.change" :progress="s.board.confidence" /&gt;</pre>
      </section>

      <!-- Next match -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Next match · ClubCrest</h2>
        <p class="text-t2">Both sides from <code class="num">next-fixture.club</code> and <code class="num">.opponent</code> (IM39). Crest is the only place club colour appears.</p>
        <UiCard class="max-w-[420px]">
          <div class="grid grid-cols-[1fr_auto_1fr] items-center gap-2 text-center">
            <div v-for="(side, i) in [nextFixture.club, nextFixture.opponent]" :key="side.club.id" :class="['flex flex-col items-center gap-1.5', i ? 'order-3' : '']">
              <UiClubCrest :short="side.club.short" size="lg" :color="i ? 'var(--color-opp)' : undefined" />
              <span class="font-medium">{{ side.club.name }}</span>
              <span class="num text-label text-t3">{{ side.league_position }}th · {{ side.form.form_string }}</span>
            </div>
            <span class="num order-2 text-meta text-t3">15:00</span>
          </div>
        </UiCard>
        <div class="flex items-center gap-3">
          <UiClubCrest short="CV" size="sm" /><UiClubCrest short="CV" /><UiClubCrest short="CV" size="lg" />
        </div>
      </section>

      <!-- Squad table -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Squad · DataTable, StatusBadge</h2>
        <p class="text-t2">
          Roster rows (IM40). Sorting is controlled: the page owns <code class="num">v-model:sort</code> and orders <code class="num">rows</code>.
          Simple density hides <code class="num">advanced</code> columns (Contract). Override cells with <code class="num">#cell-&lt;key&gt;</code>.
        </p>
        <UiSegmentedControl v-model="mode" label="Table density" class="self-start"
          :options="[{ value: 'simple', label: 'Simple' }, { value: 'standard', label: 'Standard' }, { value: 'advanced', label: 'Advanced' }]" />
        <UiCard flush class="overflow-x-auto">
          <UiDataTable v-model:sort="sort" :columns :rows :row-key="r => r.id" :density="mode === 'simple' ? 'simple' : 'standard'"
            :selected-key="selectedRow" caption="Squad" @row-click="r => selectedRow = r.id">
            <template #cell-name="{ row }">
              <span class="flex items-center gap-2">
                <span class="num w-8 text-label text-t3">{{ row.nat }}</span>
                <span class="font-medium">{{ row.name }}</span>
                <UiStatusBadge v-if="row.flag" :tone="row.flag === 'Wants out' ? 'urgent' : 'important'">{{ row.flag }}</UiStatusBadge>
              </span>
            </template>
            <template #cell-morale="{ row }">
              <span :class="row.morale <= 35 ? 'text-neg' : 'text-t2'">{{ row.morale }}</span>
            </template>
            <template #cell-wage="{ row }"><span class="num">{{ row.wage ? money(row.wage) : '—' }}</span></template>
            <template #empty><UiEmptyState title="No players match these filters" /></template>
          </UiDataTable>
        </UiCard>
        <div class="flex flex-wrap gap-1.5">
          <UiStatusBadge v-for="t in tones" :key="t" :tone="t">{{ t }}</UiStatusBadge>
          <UiStatusBadge tone="urgent" count>7</UiStatusBadge>
        </div>
      </section>

      <!-- Player -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Player · AttributeRating, StatBar, TraitMeter</h2>
        <p class="text-t2">From the player <code class="num">dossier</code>: attributes are 1–20, condition is 0–1 (×100 for bars), personality 0–20.</p>
        <div class="grid gap-3 md:grid-cols-3">
          <UiCard class="flex flex-col gap-1">
            <span class="text-label text-t3">ATTRIBUTES · 1–20</span>
            <template v-for="(attrs, cat) in dossier.attribute_values" :key="cat">
              <UiAttributeRating v-for="(v, k) in attrs" :key="k" :label="String(k).replace('_', ' ')" :value="v" />
            </template>
          </UiCard>
          <UiCard class="flex flex-col gap-2">
            <span class="text-label text-t3">CONDITION</span>
            <UiStatBar label="Fitness" :value="pct(dossier.condition.fitness)" tone="pos" />
            <UiStatBar label="Sharpness" :value="pct(dossier.condition.sharpness)" />
            <UiStatBar label="Fatigue" :value="pct(dossier.condition.fatigue)" tone="important" />
            <UiStatBar label="Morale" :value="pct(dossier.condition.morale)" tone="neg" />
          </UiCard>
          <UiCard class="flex flex-col gap-2">
            <span class="text-label text-t3">PERSONALITY</span>
            <UiTraitMeter v-for="(v, k) in dossier.personality" :key="k" :label="String(k)" :value="v" />
          </UiCard>
        </div>
      </section>

      <!-- Tactics -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Tactics · Pitch, LineupToken</h2>
        <p class="text-t2">
          Lineup slots (IM46): <code class="num">fit</code> 1 / 0.75 / 0.3 / 0.05 → natural / capable / awkward / out via <code class="num">toFit()</code>.
          The rating shown here is a placeholder; the API does not expose an effective slot rating yet.
        </p>
        <div class="grid gap-4 md:grid-cols-[minmax(0,420px)_1fr]">
          <UiPitch :slots="pitchSlots" :selected-id="selectedSlot" show-legend @select="s => selectedSlot = s.id" />
          <div class="flex flex-col gap-2">
            <div class="flex flex-wrap items-center gap-4">
              <UiLineupToken :rating="16" fit="natural" label="Natural" />
              <UiLineupToken :rating="14" fit="capable" label="Capable" />
              <UiLineupToken :rating="11" fit="awkward" label="Awkward" />
              <UiLineupToken :rating="6" fit="out" label="Out" selected />
            </div>
            <pre class="ui-code">slots.map(s =&gt; ({ id: String(s.slot), x, y,
  rating, fit: toFit(s.fit), label: s.player?.name }))</pre>
          </div>
        </div>
      </section>

      <!-- Transfers -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Transfers · OfferCard, SliderField, CheckRow, FilterChip</h2>
        <p class="text-t2">
          One <code class="num">UiOfferCard</code> per <code class="num">bid.rounds[]</code> entry (IM44). <code class="num">proposed_by</code> picks the side;
          the previous round's terms fill <code class="num">previous</code> so changes show struck through.
        </p>
        <div class="grid gap-4 md:grid-cols-2">
          <div class="flex flex-col gap-2">
            <UiOfferCard v-for="(r, i) in bid.rounds" :key="r.round" :side="r.proposed_by === 'buying_club' ? 'mine' : 'theirs'"
              :meta="`${r.proposed_by === 'buying_club' ? bid.bidding_club.name : bid.selling_club.name} · round ${r.round}`"
              :status="i === bid.rounds.length - 1 ? 'Counter' : 'Offer'" :status-tone="i === bid.rounds.length - 1 ? 'important' : 'neutral'"
              :terms="offerTerms(i)" :emphasis="i === bid.rounds.length - 1" />
          </div>
          <UiCard class="flex flex-col gap-3">
            <div class="flex flex-wrap gap-1.5">
              <UiFilterChip v-model:selected="u21Chip" label="U21" :count="4" />
              <UiFilterChip v-model:selected="loanChip" label="Include loans" />
              <UiFilterChip label="Disabled" disabled />
            </div>
            <UiSliderField v-model="fee" label="Transfer fee" :min="100" :max="400" :step="5" :format="v => `£${v}k`" />
            <UiCheckRow v-model:checked="sellOn" label="Sell-on clause" meta="20% of any future fee" />
            <UiDecisionCard tier="important" eyebrow="BID RECEIVED · EXPIRES 16 DEC" title="Ostrava Union bid £1.25m for Ademola Kintu">
              <p class="text-t2">From <code class="num">dashboard.urgent</code> item <code class="num">bids:*</code>.</p>
              <template #actions>
                <UiButton variant="primary" size="sm" @click="toast.success('Bid accepted')">Accept</UiButton>
                <UiButton size="sm" @click="sheetOpen = true">Counter…</UiButton>
                <UiButton variant="ghost" size="sm" @click="toast.error('Bid rejected')">Reject</UiButton>
              </template>
            </UiDecisionCard>
          </UiCard>
        </div>
      </section>

      <!-- Finances & board -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Finances &amp; board · HealthLadder, SegmentedControl</h2>
        <p class="text-t2">
          <code class="num">finances.health</code> (IM42) → <code class="num">toStage()</code>. Breakdown bars from <code class="num">season_breakdown</code>,
          the column chart from <code class="num">cash_history</code>, board timeline from <code class="num">confidence_history</code> (IM43).
        </p>
        <UiSegmentedControl v-model="stage" :options="stageOptions" label="Preview health stage" class="self-start" />
        <div class="grid gap-3 md:grid-cols-2">
          <UiCard class="flex flex-col gap-3">
            <UiHealthLadder :stage />
            <div class="grid gap-4 sm:grid-cols-2">
              <div v-for="(group, name) in { Revenue: finances.season_breakdown.revenue, Expenses: finances.season_breakdown.expenses }" :key="name" class="flex flex-col gap-1.5">
                <span class="text-label text-t3">{{ String(name).toUpperCase() }}</span>
                <UiStatBar v-for="f in group" :key="f.label" :label="`${f.label.replace('_', ' ')} · ${money(f.amount)}`"
                  :value="f.amount / 926_000 " :tone="name === 'Revenue' ? 'pos' : 'neutral'" />
              </div>
            </div>
          </UiCard>
          <UiCard class="flex flex-col gap-2">
            <span class="text-label text-t3">CASH BALANCE · MONTH END</span>
            <div class="flex h-32 items-end gap-1 border-b border-line" role="img" :aria-label="`Cash fell from ${money(finances.cash_history[0]!.balance)} to ${money(finances.cash)}`">
              <div v-for="c in finances.cash_history" :key="c.month" class="flex-1 rounded-t-[3px] bg-t3" :style="{ height: `${c.balance / 1_900_000}%` }" :title="`${c.month}: ${money(c.balance)}`" />
            </div>
            <span class="text-label text-t3">BOARD CONFIDENCE · {{ board.persona.replace('_', ' ') }} · {{ board.members[0]!.name }}</span>
            <div class="flex h-16 items-end gap-1">
              <div v-for="p in board.confidence_history" :key="p.world_tick" class="flex-1 rounded-t-[2px]"
                :class="p.total_score < 45 ? 'bg-important' : 'bg-t3'" :style="{ height: `${p.total_score}%` }" :title="`Tick ${p.world_tick}: ${p.total_score}`" />
            </div>
          </UiCard>
        </div>
      </section>

      <!-- Matchday -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Matchday stats</h2>
        <p class="text-t2"><code class="num">events.stats.home / .away</code> (IM45), drawn as paired StatBars.</p>
        <UiCard class="flex max-w-[520px] flex-col gap-2">
          <div v-for="(v, k) in matchStats.home" :key="k" class="grid grid-cols-[32px_1fr_32px] items-center gap-3">
            <span class="num text-right">{{ v }}</span>
            <UiStatBar :label="String(k).replace('_', ' ')" :value="(v / Math.max(1, v + matchStats.away[k])) * 100" tone="pos" />
            <span class="num">{{ matchStats.away[k] }}</span>
          </div>
        </UiCard>
      </section>

      <!-- Primitives -->
      <section class="flex flex-col gap-3">
        <h2 class="text-section">Primitives · Button, TextField, EmptyState, Loader, BottomSheet, Toaster, NavCount</h2>
        <div class="grid gap-3 md:grid-cols-2">
          <UiCard class="flex flex-col gap-3">
            <div class="flex flex-wrap gap-2">
              <UiButton variant="primary">Primary</UiButton>
              <UiButton>Secondary</UiButton>
              <UiButton variant="ghost">Ghost</UiButton>
              <UiButton variant="destructive">Destructive</UiButton>
              <UiButton loading>Loading</UiButton>
              <UiButton disabled>Disabled</UiButton>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <UiButton size="sm">Small</UiButton><UiButton size="touch">Touch 44px</UiButton>
              <span class="flex items-center gap-1 text-t2">Squad <UiNavCount :count="3" tone="urgent" /></span>
              <span class="flex items-center gap-1 text-t2">Social <UiNavCount :count="4" /></span>
            </div>
            <UiButton variant="primary" block @click="sheetOpen = true">Open bottom sheet</UiButton>
            <div class="flex flex-wrap gap-2">
              <UiButton size="sm" @click="toast.success('Bid of £340k accepted by Ostrava Union')">Toast success</UiButton>
              <UiButton size="sm" @click="toast.error('Kintu has gone public with his request')">Toast error</UiButton>
              <UiButton size="sm" @click="toast.info('Report ready: 3 left-backs')">Toast info</UiButton>
            </div>
          </UiCard>
          <UiCard class="flex flex-col gap-3">
            <UiTextField v-model="handle" label="Manager name" mono helper="Available. Shown to other managers as @millgate_manager" />
            <UiTextField v-model="email" label="Email" type="email" error="Enter a valid email address." />
            <UiEmptyState title="No scouting assignments" description="Scouts are idle. Give them a region or a role.">
              <UiButton variant="primary" size="sm">Assign scout</UiButton>
            </UiEmptyState>
            <div class="flex items-center gap-2 text-t2"><UiLoader /> Loading…</div>
          </UiCard>
        </div>
      </section>
    </div>

    <UiBottomSheet v-model:open="sheetOpen" eyebrow="NEGOTIATE MANDATE" title="Finish top 6 → top 8">
      <p class="text-t2">Preview from <code class="num">POST …/negotiate/preview</code> (IM43): accepted, delta 2, tolerance 2.</p>
      <UiWhyBreakdown subject="Board agrees?" :factors="[{ label: 'Within persona tolerance', value: 2 }, { label: 'Easier target', value: -2 }]" />
      <template #footer>
        <UiButton size="touch" @click="sheetOpen = false">Cancel</UiButton>
        <UiButton variant="primary" size="touch" @click="sheetOpen = false; toast.success('Put to the board')">Put to the board</UiButton>
      </template>
    </UiBottomSheet>
  </div>
</template>

<style>
.ui-code {
  font-family: var(--font-geist-mono);
  font-size: 12px;
  white-space: pre-wrap;
  overflow-x: auto;
  border: 1px solid var(--color-line);
  border-radius: 8px;
  background: var(--color-s1);
  color: var(--color-t2);
  padding: 10px 12px;
}
</style>
