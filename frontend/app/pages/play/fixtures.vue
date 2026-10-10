<script setup lang="ts">
// Fixtures & results (IM67): the club's whole season grouped by month, with a
// side panel for the selected fixture. On mobile the rows are cards and the
// selected one expands in place.
import { useClubFixtures } from '~/composables/manager/fixtures'
import type { LoadingStatus } from '~/types'
import type { MatchSummary, SeasonFixture } from '~/types/manager'
import type { SegmentOption } from '~/types/ui/design'

const viewport = useViewport()
const clubstore = useClubStore()
const { getSeasonFixtures, getMatchSummary } = useClubFixtures()

const mobile = computed(() => viewport.isLessThan('tablet'))
const clubId = computed(() => clubstore.club?.id)
const leagueId = computed(() => clubstore.competitions.find(c => c.competition_type === 'league')?.league?.competition.id)

const status = ref<LoadingStatus>('loading')
const fixtures = ref<SeasonFixture[]>([])

type Comp = 'All' | 'League' | 'Cups'
type Venue = 'All' | 'Home' | 'Away'
const comp = ref<Comp>('All')
const venue = ref<Venue>('All')
const compSegs: SegmentOption<Comp>[] = [{ value: 'All', label: 'All' }, { value: 'League', label: 'League' }, { value: 'Cups', label: 'Cups' }]
const venueSegs: SegmentOption<Venue>[] = [{ value: 'All', label: 'All' }, { value: 'Home', label: 'Home' }, { value: 'Away', label: 'Away' }]

type Result = 'W' | 'D' | 'L'
type Row = {
  f: SeasonFixture, league: boolean, home: boolean, played: boolean, opp: string
  gf: number, ga: number, res: Result | null
}
const rows = computed<Row[]>(() => fixtures.value.map((f) => {
  const home = f.home_club.id === clubId.value
  const played = f.status === 'completed' && f.home_score != null && f.away_score != null
  const gf = (home ? f.home_score : f.away_score) ?? 0
  const ga = (home ? f.away_score : f.home_score) ?? 0
  return {
    f, home, played, gf, ga,
    league: f.competition.id === leagueId.value,
    opp: (home ? f.away_club : f.home_club).name,
    res: played ? (gf > ga ? 'W' : gf < ga ? 'L' : 'D') : null,
  }
}))
const nextId = computed(() => rows.value.find(r => r.f.status === 'scheduled')?.f.id)

const visible = computed(() => rows.value.filter(r =>
  (comp.value === 'All' || (comp.value === 'League') === r.league)
  && (venue.value === 'All' || (venue.value === 'Home') === r.home)))

const months = computed(() => {
  const out: { label: string, rows: Row[] }[] = []
  for (const r of visible.value) {
    const label = new Date(r.f.scheduled_at).toLocaleDateString(undefined, { month: 'long', year: 'numeric' }).toUpperCase()
    const last = out[out.length - 1]
    if (last?.label === label) last.rows.push(r)
    else out.push({ label, rows: [r] })
  }
  return out
})

// Summary tiles: league record and points; goals across all competitions.
const kpis = computed(() => {
  const played = rows.value.filter(r => r.played)
  const lg = played.filter(r => r.league)
  const count = (x: Result) => lg.filter(r => r.res === x).length
  const [w, d, l] = [count('W'), count('D'), count('L')]
  const pos = [...lg].reverse().find(r => r.f.position_after)?.f.position_after
  const left = rows.value.length - played.length
  const next = rows.value.find(r => r.f.id === nextId.value)
  return {
    w, d, l, left,
    tiles: [
      { k: 'Played', v: String(played.length), s: `of ${rows.value.length} fixtures` },
      { k: 'League record', v: `${w}-${d}-${l}`, s: 'W-D-L' },
      { k: 'Points', v: String(w * 3 + d), s: pos ? `Pos ${ordinal(pos)}` : '—' },
      { k: 'Goals', v: `${played.reduce((a, r) => a + r.gf, 0)}:${played.reduce((a, r) => a + r.ga, 0)}`, s: 'All competitions' },
      { k: 'Remaining', v: String(left), s: next ? `Next: ${next.opp}` : 'Season complete' },
    ],
  }
})

const selectedId = ref<string | null>(null)
const selected = computed(() => rows.value.find(r => r.f.id === (selectedId.value ?? nextId.value)))
function select(r: Row) {
  selectedId.value = mobile.value && selectedId.value === r.f.id ? null : r.f.id
}

const summary = ref<MatchSummary | null>(null)
watch(() => selected.value?.f.match_id, async (id) => {
  summary.value = null
  if (!id) return
  const s = await getMatchSummary(id)
  if (selected.value?.f.match_id === id) summary.value = s ?? null
}, { immediate: true })

const ourGoals = computed(() => {
  const r = selected.value
  if (!r || !summary.value) return []
  const ours = r.home ? r.f.home_club.id : r.f.away_club.id
  return summary.value.events.filter(e => e.type === 'goal' && e.club?.id === ours)
    .map(e => ({ name: e.player?.name ?? 'Unknown', minute: `${e.minute}'` }))
})
// Stats in home | away order, matching the scoreline.
const stats = computed(() => {
  const s = summary.value?.stats
  if (!s) return []
  const line = (k: string, a: number, b: number, fmt = (n: number) => String(n)) =>
    ({ k, a: fmt(a), b: fmt(b), w: a + b > 0 ? (a / (a + b)) * 100 : 50 })
  return [
    ...(s.home.xg != null && s.away.xg != null ? [line('xG', s.home.xg, s.away.xg, n => n.toFixed(1))] : []),
    line('Chances', s.home.chances_created, s.away.chances_created),
    line('Cards', s.home.yellow_cards + s.home.red_cards, s.away.yellow_cards + s.away.red_cards),
  ]
})

const ordinal = (n: number) => {
  const t = n % 100, o = n % 10
  return n + (t >= 11 && t <= 13 ? 'th' : o === 1 ? 'st' : o === 2 ? 'nd' : o === 3 ? 'rd' : 'th')
}
const fmtDate = (iso: string) => new Date(iso).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
const fmtShort = (iso: string) => new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
const fmtTime = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
const compLabel = (r: Row) => r.league ? r.f.competition.name : `${r.f.competition.name} R${r.f.matchday}`
const compShort = (r: Row) => r.league ? `MD ${r.f.matchday}` : `R${r.f.matchday}`
const score = (r: Row) => r.played ? `${r.f.home_score}–${r.f.away_score}` : fmtTime(r.f.scheduled_at)
const resTone = { W: 'pos', D: 'neutral', L: 'neg' } as const
const diffTone = (level?: number) => !level ? 'text-t2' : level >= 4 ? 'text-neg' : level === 3 ? 'text-important' : 'text-pos'
const lastMeeting = (r: Row) => {
  const m = r.f.last_meeting
  return m ? `${m.home_club.name} ${m.home_score ?? 0}–${m.away_score ?? 0} ${m.away_club.name}` : 'First meeting'
}
const crowd = (n: number | null) => n == null ? '' : n.toLocaleString()

async function load() {
  status.value = 'loading'
  const list = await getSeasonFixtures()
  if (!list) { status.value = 'error'; return }
  fixtures.value = list
  status.value = 'loaded'
}
watch(clubId, id => id && load(), { immediate: true })
</script>

<template>
  <!-- Mobile: fixed header over a scrolling card list; the selected card expands. -->
  <main v-if="mobile" class="-mx-3.5 flex h-full flex-col">
    <header class="flex flex-col gap-2.5 border-b border-line bg-s1 px-4 pb-2.5 pt-3.5">
      <div class="flex flex-col gap-0.5">
        <h2 class="text-[17px] font-semibold">Fixtures</h2>
        <span class="num text-[11.5px] text-t3">{{ kpis.w }}W {{ kpis.d }}D {{ kpis.l }}L · {{ kpis.left }} to play</span>
      </div>
      <UiSegmentedControl v-model="comp" label="Competition" :options="compSegs" fill />
    </header>

    <section class="flex min-h-0 flex-1 flex-col gap-1.5 overflow-y-auto px-3 py-2.5">
      <UiEmptyState v-if="status === 'error'" title="Fixtures unavailable" />
      <template v-else-if="status === 'loading'">
        <div v-for="i in 8" :key="i" class="flex min-h-[56px] items-center gap-3 rounded-card border border-line bg-s1 px-3 py-2.5" aria-hidden="true">
          <div class="flex w-11 flex-col gap-1.5"><UiLoader class="h-3 w-9" /><UiLoader class="h-2.5 w-8" /></div>
          <div class="flex flex-1 flex-col gap-1.5"><UiLoader class="h-3.5 w-32" /><UiLoader class="h-3 w-24" /></div>
          <UiLoader class="h-4 w-8" />
        </div>
      </template>
      <UiEmptyState v-else-if="!fixtures.length" title="No fixtures this season" />
      <template v-else>
        <template v-for="m in months" :key="m.label">
          <span class="num px-0.5 pb-0.5 pt-2 text-[10.5px] text-t3">{{ m.label }}</span>
          <div v-for="r in m.rows" :key="r.f.id"
            :class="['flex flex-col gap-2 rounded-card border bg-s1 px-3 py-2.5',
                     selected?.f.id === r.f.id && selectedId ? 'border-line2' : r.f.id === nextId ? 'border-important' : 'border-line']">
            <button type="button" class="flex min-h-9 items-center gap-2.5 text-left" :aria-expanded="selectedId === r.f.id" @click="select(r)">
              <span class="flex w-11 flex-none flex-col">
                <span class="num text-[11px] text-t2">{{ fmtShort(r.f.scheduled_at) }}</span>
                <span :class="['text-[10.5px]', r.league ? 'text-t2' : 'text-info']">{{ compShort(r) }}</span>
              </span>
              <span class="flex min-w-0 flex-1 flex-col">
                <span class="truncate font-medium">{{ r.opp }}</span>
                <span class="text-[11px] text-t3">{{ r.home ? 'Home' : 'Away' }}<template v-if="r.f.attendance != null"> · {{ crowd(r.f.attendance) }} att.</template><template v-else-if="r.f.difficulty"> · {{ r.f.difficulty.label }}</template></span>
              </span>
              <span :class="['num text-[14px] font-semibold', r.played ? 'text-t1' : 'text-t3']">{{ score(r) }}</span>
              <UiStatusBadge v-if="r.res" :tone="resTone[r.res]" class="w-[22px] justify-center">{{ r.res }}</UiStatusBadge>
              <UiStatusBadge v-else-if="r.f.id === nextId" tone="important">NEXT</UiStatusBadge>
            </button>
            <div v-if="selectedId === r.f.id" class="flex flex-col gap-1.5 border-t border-line pt-2 text-[12px]">
              <template v-if="r.played">
                <div v-for="(g, i) in ourGoals" :key="i" class="flex justify-between text-t2"><span>{{ g.name }}</span><span class="num text-t3">{{ g.minute }}</span></div>
                <div v-for="s in stats" :key="s.k" class="num flex justify-between"><span>{{ s.a }}</span><span class="font-geist text-t3">{{ s.k }}</span><span>{{ s.b }}</span></div>
                <NuxtLink v-if="r.f.match_id" :to="`/play/matches/${r.f.id}`" class="text-info">Match report</NuxtLink>
              </template>
              <template v-else>
                <div class="flex justify-between"><span class="text-t3">Kick-off · Difficulty</span><span>{{ fmtTime(r.f.scheduled_at) }} · <span :class="diffTone(r.f.difficulty?.level)">{{ r.f.difficulty?.label ?? '—' }}</span></span></div>
                <div class="flex justify-between"><span class="text-t3">Last meeting</span><span class="num">{{ lastMeeting(r) }}</span></div>
              </template>
            </div>
          </div>
        </template>
      </template>
    </section>
  </main>

  <!-- Desktop: filters, summary tiles, month-grouped table + sticky detail panel. -->
  <main v-else class="flex flex-col gap-4 py-5">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <div class="flex flex-col gap-0.5">
        <span class="num text-[11px] uppercase text-t3">{{ clubstore.club?.name }}</span>
        <h2 class="text-page">Fixtures &amp; results</h2>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <UiSegmentedControl v-model="comp" label="Competition" :options="compSegs" />
        <UiSegmentedControl v-model="venue" label="Venue" :options="venueSegs" />
      </div>
    </div>

    <UiEmptyState v-if="status === 'error'" title="Fixtures unavailable" />
    <template v-else>
      <div class="grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-2">
        <UiCard v-for="t in kpis.tiles" :key="t.k" class="flex flex-col gap-0.5 p-3">
          <span class="num text-[10.5px] uppercase text-t3">{{ t.k }}</span>
          <UiLoader v-if="status === 'loading'" class="my-1 h-5 w-14" />
          <span v-else class="num text-[20px] font-semibold">{{ t.v }}</span>
          <span class="text-[11.5px] text-t3">{{ status === 'loading' ? '' : t.s }}</span>
        </UiCard>
      </div>

      <div class="flex flex-wrap items-start gap-4">
        <UiCard flush class="min-w-0 flex-[2_1_640px] overflow-auto">
          <div class="min-w-[680px]" role="table" aria-label="Fixtures and results">
            <div role="row" class="fx-grid num border-b border-line px-3.5 py-2 text-[10.5px] text-t3">
              <span role="columnheader">MD</span><span role="columnheader">DATE</span><span role="columnheader">COMP</span>
              <span role="columnheader">OPPONENT</span><span role="columnheader">H/A</span><span role="columnheader" class="text-center">SCORE</span>
              <span role="columnheader">RES</span><span role="columnheader" class="text-right">POS</span><span role="columnheader" class="text-right">ATT</span>
            </div>
            <template v-if="status === 'loading'">
              <div v-for="i in 10" :key="i" class="fx-grid min-h-11 items-center border-b border-line px-3.5" aria-hidden="true">
                <UiLoader class="h-3 w-5" /><UiLoader class="h-3 w-16" /><UiLoader class="h-3 w-14" /><UiLoader class="h-3.5 w-32" />
                <UiLoader class="h-3 w-3" /><UiLoader class="mx-auto h-3.5 w-9" /><UiLoader class="h-4 w-6" /><UiLoader class="ml-auto h-3 w-6" /><UiLoader class="ml-auto h-3 w-10" />
              </div>
            </template>
            <div v-else-if="!visible.length" class="p-4 text-meta text-t2">No fixtures match these filters</div>
            <template v-for="m in months" v-else :key="m.label">
              <div class="num border-b border-line bg-s2 px-3.5 py-2 text-[10.5px] text-t3">{{ m.label }}</div>
              <div v-for="r in m.rows" :key="r.f.id" role="row" tabindex="0"
                :class="['fx-grid min-h-11 cursor-pointer items-center border-b border-line px-3.5 hover:bg-s2 focus-visible:bg-s2 focus-visible:outline-none',
                         selected?.f.id === r.f.id && 'bg-s3', r.f.id === nextId && 'shadow-[inset_3px_0_0_var(--color-important)]']"
                :aria-selected="selected?.f.id === r.f.id" @click="select(r)" @keydown.enter="select(r)">
                <span class="num text-[12px] text-t3">{{ r.league ? r.f.matchday : '—' }}</span>
                <span class="num text-[12px] text-t2">{{ fmtDate(r.f.scheduled_at) }}</span>
                <span :class="['truncate text-[11.5px]', r.league ? 'text-t2' : 'text-info']">{{ compLabel(r) }}</span>
                <span class="truncate font-medium">{{ r.opp }}</span>
                <span class="num text-[11.5px] text-t3">{{ r.home ? 'H' : 'A' }}</span>
                <span :class="['num text-center text-[13px] font-semibold', r.played ? 'text-t1' : 'text-t3']">{{ score(r) }}</span>
                <span>
                  <UiStatusBadge v-if="r.res" :tone="resTone[r.res]">{{ r.res }}</UiStatusBadge>
                  <UiStatusBadge v-else-if="r.f.id === nextId" tone="important">NEXT</UiStatusBadge>
                </span>
                <span class="num text-right text-[12px] text-t2">{{ r.f.position_after ? ordinal(r.f.position_after) : '' }}</span>
                <span class="num text-right text-[12px] text-t3">{{ crowd(r.f.attendance) }}</span>
              </div>
            </template>
          </div>
        </UiCard>

        <UiCard v-if="selected" as="aside" class="sticky top-4 flex min-w-0 flex-[1_1_300px] flex-col gap-3.5 p-4">
          <div class="num flex justify-between text-[10.5px] uppercase text-t3">
            <span>{{ selected.league ? `${selected.f.competition.name} · MD ${selected.f.matchday}` : compLabel(selected) }}</span>
            <span>{{ fmtDate(selected.f.scheduled_at) }}</span>
          </div>
          <div class="grid grid-cols-[1fr_auto_1fr] items-center gap-2.5 text-center">
            <span class="font-semibold">{{ selected.f.home_club.name }}</span>
            <span class="num text-[28px] font-semibold">{{ selected.played ? `${selected.f.home_score}–${selected.f.away_score}` : 'vs' }}</span>
            <span class="font-semibold">{{ selected.f.away_club.name }}</span>
          </div>

          <template v-if="selected.played">
            <div class="flex flex-col gap-1">
              <div v-for="(g, i) in ourGoals" :key="i" class="flex justify-between text-[12px] text-t2"><span>{{ g.name }}</span><span class="num text-t3">{{ g.minute }}</span></div>
              <span v-if="summary && !ourGoals.length" class="text-[12px] text-t3">No goals for us</span>
            </div>
            <div v-if="stats.length" class="flex flex-col gap-2.5 border-t border-line pt-3">
              <div v-for="s in stats" :key="s.k" class="flex flex-col gap-1">
                <div class="num flex justify-between text-[12px] font-medium"><span>{{ s.a }}</span><span class="font-geist text-t3">{{ s.k }}</span><span>{{ s.b }}</span></div>
                <div class="h-1 overflow-hidden rounded-sm bg-s3"><div class="h-full bg-club" :style="{ width: `${s.w}%` }" /></div>
              </div>
            </div>
            <div v-else-if="!summary" class="flex flex-col gap-2 border-t border-line pt-3" aria-hidden="true">
              <UiLoader v-for="i in 3" :key="i" class="h-4 w-full" />
            </div>
            <div v-if="selected.f.attendance != null" class="flex justify-between border-t border-line pt-3 text-[12px]">
              <span class="text-t3">Attendance</span><span class="num">{{ crowd(selected.f.attendance) }}</span>
            </div>
            <UiButton v-if="selected.f.match_id" variant="secondary" @click="navigateTo(`/play/matches/${selected.f.id}`)">Match report</UiButton>
          </template>

          <template v-else>
            <div class="flex flex-col gap-2 text-[12px]">
              <div v-if="selected.f.attendance != null" class="flex justify-between"><span class="text-t3">Attendance</span><span class="num">{{ crowd(selected.f.attendance) }}</span></div>
              <div class="flex justify-between"><span class="text-t3">Kick-off</span><span>{{ fmtDate(selected.f.scheduled_at) }} {{ fmtTime(selected.f.scheduled_at) }}</span></div>
              <div class="flex justify-between"><span class="text-t3">Difficulty</span><span :class="diffTone(selected.f.difficulty?.level)">{{ selected.f.difficulty?.label ?? '—' }}</span></div>
              <div class="flex justify-between gap-3"><span class="text-t3">Last meeting</span><span class="num truncate">{{ lastMeeting(selected) }}</span></div>
            </div>
            <UiButton variant="primary" @click="navigateTo(`/play/matches/${selected.f.id}`)">Open match preview</UiButton>
          </template>
        </UiCard>
      </div>
    </template>
  </main>
</template>

<style scoped>
.fx-grid {
  display: grid;
  grid-template-columns: 40px 92px 120px 1fr 34px 70px 52px 50px 72px;
  gap: 10px;
}
</style>
