<script setup lang="ts">
// S04-01 admin league builder: create countries, declare leagues with their
// rules, wire promotion/relegation adjacency, then seed the competition —
// the engine schedules a deterministic round-robin once seeded.
import { ref, computed } from 'vue'

import type { Country, League, SeedResult } from '~/composables/useCompetition'
import { useCompetition } from '~/composables/useCompetition'
import { useAuth } from '~/composables/useAuth'

const { user } = useAuth()
const comp = useCompetition()

const worldId = ref('')
const error = ref('')
const notice = ref('')
const busy = ref(false)

const countries = ref<Country[]>([])
const leagues = ref<League[]>([])
const seedResult = ref<SeedResult | null>(null)

const countryForm = ref({ code: '', name: '' })
const leagueForm = ref({
  country_id: '',
  name: '',
  tier: 1,
  team_count: 8,
  promotions: 1,
  relegations: 1,
})

const countriesFor = (countryId: string) => leagues.value.filter((l) => l.country_id === countryId)
const leaguesOfCountry = computed(() => leagueForm.value.country_id ? countriesFor(leagueForm.value.country_id) : [])

async function loadAll() {
  countries.value = await comp.listCountries(worldId.value)
  leagues.value = await comp.listLeagues(worldId.value)
}
async function call(fn: () => Promise<void>) {
  error.value = ''
  notice.value = ''
  if (!worldId.value) {
    error.value = 'Enter a world id (admin operations are world-scoped).'
    return
  }
  busy.value = true
  try {
    await fn()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function refreshLists() {
  await call(loadAll)
}
async function doCreateCountry() {
  await call(async () => {
    const c = await comp.createCountry(worldId.value, countryForm.value.code, countryForm.value.name)
    notice.value = `Country "${c.name}" created.`
    countryForm.value = { code: '', name: '' }
    await loadAll()
  })
}
async function doCreateLeague() {
  await call(async () => {
    const l = await comp.createLeague(leagueForm.value)
    notice.value = `League "${l.name}" created (tier ${l.tier}, ${l.team_count} teams).`
    leagueForm.value = { ...leagueForm.value, name: '', tier: 1, team_count: 8, promotions: 1, relegations: 1 }
    await loadAll()
  })
}
async function doAdjacency(l: League, key: 'promotes_to' | 'relegates_to', value: string) {
  const patch = key === 'promotes_to'
    ? { promotes_to: value || null, relegates_to: l.relegates_to }
    : { promotes_to: l.promotes_to, relegates_to: value || null }
  await call(async () => {
    await comp.updateAdjacency(l.id, patch)
    notice.value = `Adjacency updated for "${l.name}".`
    await loadAll()
  })
}
async function doSeed(starterLeagueId: string) {
  await call(async () => {
    seedResult.value = await comp.seedCompetition(worldId.value, leagueForm.value.country_id, starterLeagueId)
    notice.value = `Country seeded — ${seedResult.value.leagues.length} leagues, fixtures scheduled.`
  })
}
</script>

<template>
  <main class="min-h-screen bg-slate-900 text-slate-200 px-6 py-10">
    <div class="max-w-5xl mx-auto space-y-8">
      <header>
        <h1 class="text-3xl font-bold text-white">League administration</h1>
        <p class="text-slate-400 mt-1">Signed in as {{ user ?? '?' }}. Countries, leagues, promotion traffic — all admin-declared per world.</p>
      </header>

      <section class="bg-slate-800 border border-slate-700 rounded-lg p-4">
        <label class="block text-sm text-slate-400 mb-1">World id</label>
        <div class="flex gap-2">
          <input v-model="worldId" type="text" placeholder="00000000-0000-0000-0000-000000000000"
                 class="flex-1 bg-slate-900 border border-slate-600 rounded px-3 py-2 text-sm" />
          <button @click="refreshLists" class="bg-indigo-600 hover:bg-indigo-500 text-white rounded px-4 text-sm">Load</button>
        </div>
        <p v-if="error" class="text-red-400 text-sm mt-2">{{ error }}</p>
        <p v-if="notice" class="text-emerald-400 text-sm mt-2">{{ notice }}</p>
      </section>

      <section class="bg-slate-800 border border-slate-700 rounded-lg p-4">
        <h2 class="text-xl font-semibold text-white mb-3">Countries</h2>
        <div class="flex flex-wrap gap-2 items-end mb-3">
          <div>
            <label class="block text-xs text-slate-400">Code</label>
            <input v-model="countryForm.code" type="text" placeholder="eng" class="bg-slate-900 border border-slate-600 rounded px-3 py-2 w-28" />
          </div>
          <div>
            <label class="block text-xs text-slate-400">Name</label>
            <input v-model="countryForm.name" type="text" placeholder="England" class="bg-slate-900 border border-slate-600 rounded px-3 py-2 w-48" />
          </div>
          <button :disabled="busy" @click="doCreateCountry" class="bg-emerald-600 hover:bg-emerald-500 text-white rounded px-4 py-2">Add country</button>
        </div>
        <ul class="flex flex-wrap gap-2">
          <li v-for="c in countries" :key="c.id" class="bg-slate-900 border border-slate-700 rounded-full px-4 py-1 text-sm">
            {{ c.name }} <span class="text-slate-500 uppercase">({{ c.code }})</span>
          </li>
          <li v-if="!countries.length" class="text-slate-500 text-sm">No countries for this world yet.</li>
        </ul>
      </section>

      <section class="bg-slate-800 border border-slate-700 rounded-lg p-4">
        <h2 class="text-xl font-semibold text-white mb-3">Leagues</h2>
        <div class="grid grid-cols-2 md:grid-cols-6 gap-2 items-end mb-3">
          <div>
            <label class="block text-xs text-slate-400">Country</label>
            <select v-model="leagueForm.country_id" class="bg-slate-900 border border-slate-600 rounded px-3 py-2 w-full">
              <option v-for="c in countries" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>
          <div>
            <label class="block text-xs text-slate-400">Name</label>
            <input v-model="leagueForm.name" type="text" placeholder="Premier" class="bg-slate-900 border border-slate-600 rounded px-3 py-2 w-full" />
          </div>
          <div>
            <label class="block text-xs text-slate-400">Tier</label>
            <input v-model.number="leagueForm.tier" type="number" min="1" class="bg-slate-900 border border-slate-600 rounded px-3 py-2 w-full" />
          </div>
          <div>
            <label class="block text-xs text-slate-400">Teams</label>
            <input v-model.number="leagueForm.team_count" type="number" min="4" step="2" class="bg-slate-900 border border-slate-600 rounded px-3 py-2 w-full" />
          </div>
          <div>
            <label class="block text-xs text-slate-400">Promote</label>
            <input v-model.number="leagueForm.promotions" type="number" min="0" class="bg-slate-900 border border-slate-600 rounded px-3 py-2 w-full" />
          </div>
          <div>
            <label class="block text-xs text-slate-400">Relegate</label>
            <input v-model.number="leagueForm.relegations" type="number" min="0" class="bg-slate-900 border border-slate-600 rounded px-3 py-2 w-full" />
          </div>
        </div>
        <button :disabled="busy" @click="doCreateLeague" class="bg-indigo-600 hover:bg-indigo-500 text-white rounded px-4 py-2">Add league</button>

        <div v-if="leaguesOfCountry.length" class="mt-4">
          <h3 class="text-sm text-slate-400 mb-2">Promotion traffic (1 up must equal 1 down across the border)</h3>
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead class="text-slate-500 text-left">
                <tr><th class="py-1 pr-3">Tier</th><th class="py-1 pr-3">League</th><th class="py-1 pr-3">Teams</th><th class="py-1 pr-3">Promotes to</th><th class="py-1 pr-3">Relegates to</th></tr>
              </thead>
              <tbody>
                <tr v-for="l in leaguesOfCountry" :key="l.id" class="border-t border-slate-700">
                  <td class="py-2 pr-3">{{ l.tier }}</td>
                  <td class="py-2 pr-3 font-medium text-white">{{ l.name }}</td>
                  <td class="py-2 pr-3">{{ l.team_count }}</td>
                  <td class="py-2 pr-3">
                    <select :value="l.promotes_to ?? ''" @change="doAdjacency(l, 'promotes_to', (($event.target as HTMLSelectElement).value))"
                            class="bg-slate-900 border border-slate-600 rounded px-2 py-1 w-44">
                      <option value="">— none —</option>
                      <option v-for="t in leaguesOfCountry" :key="t.id" :value="t.id" :disabled="t.id === l.id">{{ t.name }}</option>
                    </select>
                  </td>
                  <td class="py-2 pr-3">
                    <select :value="l.relegates_to ?? ''" @change="doAdjacency(l, 'relegates_to', (($event.target as HTMLSelectElement).value))"
                            class="bg-slate-900 border border-slate-600 rounded px-2 py-1 w-44">
                      <option value="">— none —</option>
                      <option v-for="t in leaguesOfCountry" :key="t.id" :value="t.id" :disabled="t.id === l.id">{{ t.name }}</option>
                    </select>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <section v-if="leaguesOfCountry.length" class="bg-slate-800 border border-slate-700 rounded-lg p-4">
        <h2 class="text-xl font-semibold text-white mb-1">Seed competition</h2>
        <p class="text-slate-400 text-sm mb-3">The world's starter club will be placed in the league you pick; every league then fills to its team count with generated AI clubs and a full double round-robin is scheduled.</p>
        <div class="flex flex-wrap gap-2">
          <button v-for="l in leaguesOfCountry" :key="l.id" :disabled="busy" @click="doSeed(l.id)"
                  class="bg-emerald-600 hover:bg-emerald-500 text-white rounded px-4 py-2">
            Seed with starter in {{ l.name }}
          </button>
        </div>

        <div v-if="seedResult" class="mt-4 grid md:grid-cols-2 gap-3">
          <div v-for="ls in seedResult.leagues" :key="ls.league_id" class="bg-slate-900 border border-slate-700 rounded-lg p-3">
            <h3 class="font-medium text-white">{{ ls.name }} <span class="text-slate-500 text-sm">— {{ ls.season_label }}</span></h3>
            <p class="text-sm text-slate-400">{{ ls.team_count }} teams · {{ ls.matchdays }} matchdays · {{ ls.fixture_count }} fixtures</p>
          </div>
        </div>
      </section>
    </div>
  </main>
</template>