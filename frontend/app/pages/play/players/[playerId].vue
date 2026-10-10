<script setup lang="ts">
// Player detail (IM20): one player of the caller's world, reached from a name
// in the match feed or the squad list. Everything on screen is the server's
// PlayerDetail read — the ability block is the same two numbers the squad roster
// shows, and the weekly wage is present only when this is the manager's own club.
// IM63: an own-club player renders the squad's tabbed PlayerPanel instead.
import { playerAttributeCategories, type PlayerDetail } from '~/composables/usePlayer'
import { useManagerSquad, type RequestAction } from '~/composables/manager/squad'
import type { PricePreset } from '~/types/manager'
import type { ManagerSquadPlayer, PlayerMoraleDetail, SquadDynamics } from '~/types/manager'

const route = useRoute()
const { getPlayer } = usePlayer()
const squad = useManagerSquad()
const clubstore = useClubStore()
const own = ref<{ detail: PlayerMoraleDetail, row?: ManagerSquadPlayer, dynamics: SquadDynamics | null } | null>(null)
const busy = ref(false)

const playerId = computed(() => String(route.params.playerId))
const loading = ref<"loading" | "loaded" | "error">("loading")
const player = ref<PlayerDetail | null>(null)

const age = computed(() => {
  const dob = player.value?.date_of_birth
  if (!dob) return null
  const born = new Date(dob)
  if (Number.isNaN(born.getTime())) return null
  const now = new Date()
  let years = now.getFullYear() - born.getFullYear()
  const monthDelta = now.getMonth() - born.getMonth()
  if (monthDelta < 0 || (monthDelta === 0 && now.getDate() < born.getDate())) years--
  return years
})

const attributes = computed(() => {
  const attrs = player.value?.attributes
  if (!attrs) return []
  return playerAttributeCategories.map(({ key, label }) => ({ label, value: attrs[key] }))
})

async function init() {
  loading.value = "loading"
  try {
    player.value = await getPlayer(playerId.value)
    await loadOwn()
    loading.value = "loaded"
  } catch {
    player.value = null
    loading.value = "error"
  }
}

async function loadOwn() {
  const clubId = clubstore.club?.id
  if (!clubId || player.value?.club?.id !== clubId) { own.value = null; return }
  const [detail, rows, dynamics] = await Promise.all([
    squad.getPlayer(clubId, playerId.value), squad.getSquad(clubId), squad.getDynamics(clubId),
  ])
  own.value = detail ? { detail, row: rows?.find(r => r.player.id === playerId.value), dynamics } : null
}

async function onRespond(action: RequestAction, preset?: PricePreset) {
  const clubId = clubstore.club?.id
  if (!clubId) return
  busy.value = true
  if (await squad.respond(clubId, playerId.value, action, preset)) await loadOwn()
  busy.value = false
}

onMounted(init)
</script>

<template>
  <main v-if="own" class="space-y-4 pt-5">
    <NuxtLink to="/play/squad" class="text-meta text-t2 hover:text-t1">&lsaquo; Squad</NuxtLink>
    <SquadPlayerPanel :detail="own.detail" :row="own.row" :dynamics="own.dynamics" :busy="busy" tabbed @respond="onRespond" />
  </main>
  <main v-else class="h-full text-slate-200 font-mono space-y-5">
    <div class="flex items-center gap-4">
      <NuxtLink to="/play" class="heading heading--small text-slate-400 hover:text-slate-200">&larr; Back</NuxtLink>
      <h2 class="page__header">Player</h2>
    </div>

    <OldUiLoader v-if="loading === 'loading'" />

    <p v-else-if="loading === 'error'" class="text-red-400">That player could not be loaded. They may not play in your world.</p>

    <template v-else-if="player">
      <section class="grid gap-5 grid-cols-3">
        <div class="p-5 border-brutal border-void-700">
          <div class="flex items-start justify-between border-b-brutal pb-5 border-void-800">
            <div>
              <h3 class="heading">{{ player.display_name }}</h3>
              <p class="heading heading--small text-slate-400">
                {{ player.position }}<span v-if="player.squad_number"> &middot; #{{ player.squad_number }}</span>
                <span v-if="age !== null"> &middot; {{ age }} yrs</span>
                <span v-if="player.nationality"> &middot; {{ player.nationality.toUpperCase() }}</span>
              </p>
              <p v-if="player.club" class="heading heading--small text-slate-400">{{ player.club.name }}</p>
              <p v-else class="heading heading--small text-slate-500">Free agent</p>
            </div>
            <div class="text-center">
              <p class="heading heading--small text-slate-400">OVR</p>
              <p class="heading text-2xl">{{ player.overall }}</p>
            </div>
          </div>
          <p class="heading heading--small text-slate-400 mt-5" v-if="player.weekly_wage">
            Weekly wage: {{ player.weekly_wage.toLocaleString() }}
          </p>
          <p class="heading heading--small text-slate-500 mt-5" v-else>
            Weekly wage is the owning club's business.
          </p>
        </div>

        <div class="p-5 border-brutal border-void-700">
          <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
            <h3 class="heading heading--small">Career</h3>
          </div>
          <div class="grid grid-cols-4 gap-2 mt-5">
            <div class="heading heading--small">Apps</div>
            <div class="heading heading--small">Goals</div>
            <div class="heading heading--small">Assists</div>
            <div class="heading heading--small">Rating</div>
            <div class="heading">{{ player.career.appearances }}</div>
            <div class="heading">{{ player.career.goals }}</div>
            <div class="heading">{{ player.career.assists }}</div>
            <div class="heading">{{ player.career.average_rating || '—' }}</div>
          </div>
        </div>

        <div class="p-5 border-brutal border-void-700">
          <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
            <h3 class="heading heading--small">Attributes</h3>
          </div>
          <div class="grid grid-cols-3 gap-2 mt-5">
            <template v-for="attr in attributes" :key="attr.label">
              <div class="heading heading--small">{{ attr.label }}</div>
              <div class="heading">{{ attr.value }}</div>
            </template>
          </div>
        </div>
      </section>
    </template>
  </main>
</template>
