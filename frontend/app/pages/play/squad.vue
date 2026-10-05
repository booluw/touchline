<script setup lang="ts">
import { useToast } from '~/components/ui/Toast'

import IllustrationsDirectTactics from '~/components/illustrations/direct-tactics.vue';
import IllustrationsBalancedTactics from '~/components/illustrations/balanced-tactics.vue';
import IllustrationsGengenpressTactics from '~/components/illustrations/gengenpress-tactics.vue';
import IllustrationsLowblockTactics from '~/components/illustrations/lowblock-tactics.vue';
import IllustrationsPosessionTactics from '~/components/illustrations/posession-tactics.vue';

import type { SquadPlayer, Slot } from '~/types';

const { getTactics, saveTactics, saveLineup } = useClub()
const { notify } = useToast()

const loading = ref<"loading" | "loaded" | "error">("loading")
const saving = ref(false)

const tactics = ref<{ style: string, formation: string, allowed_formations: string[] }>()
const squad = ref<SquadPlayer[]>([])
const lineup = ref<{ formation: string, slots: Slot[] }>()

const slots = ref<Slot[]>([])
const selectedSlot = ref<number | null>(null)
const slot = computed(() => lineup.value?.slots.find((s) => s.slot === selectedSlot.value))
const playersBySelectedSlotPosition = computed(() => {
  const matched = squad.value.filter((s) => s?.position === slot.value?.position)

  return [...new Map([...matched, ...squad.value].map(player => [player.player.id, player])).values()]
})

const playersInLineup = computed(() => {
  return slots.value.map((s) => s?.player?.player_id ?? s?.player?.id ?? false)
})

const playerId = ref()
const state = ref({ style: '', formation: '' })
const tacticsModal = ref(false)
const formations = computed(() => TACTICS.find((tactic) => tactic.id === state.value.style)?.formations ?? [])

async function init() {
  try {
    const res = await getTactics()

    tactics.value = res.tactics
    lineup.value = res.lineup
    squad.value = res.squad.players

    state.value.style = res.tactics.style
    state.value.formation = res.lineup.formation
    slots.value = res.lineup.slots.map((s: Slot) => {
      const player = res.squad.players.find((p: SquadPlayer) => p?.player?.id === s.player?.id)

      return {
        ...s,
        player: {
          ...s.player,
          display_name: player?.player?.name,
          squad_number: player?.squad_number
        }
      }
    })

    loading.value = "loaded"
  } catch (error) {
    console.error(error)
    loading.value = "error"
  } finally {
    tacticsModal.value = false
  }
}

async function saveClubTactics() {
  if (!formations.value.includes(state.value.formation)) {
    notify({
      title: "Please select a formation for this tactics.",
      type: "warning"
    })
    return
  }
  saving.value = true

  try {
    await saveTactics(state.value)
    notify({
      title: "Success",
      description: "Tactics saved",
      type: "success"
    })
    await init()
  } catch (error) {
    console.error(error)
  } finally {
    saving.value = false
  }
}

async function saveClubLineup() {
  saving.value = true
  try {
    await saveLineup({
      slots: slots.value.map((s) => ({ slot: s.slot, player_id: s?.player?.player_id ?? s?.player?.id }))
    })

    notify({
      title: "Line up saved, your team is ready",
      type: "success"
    })
    await init()
  } finally {
    saving.value = false
  }
}

function saveSlot(player: { squad_number?: number, display_name: string, player_id: string }) {
  if (selectedSlot.value === null) {
    playerId.value = player.player_id
    return
  }

  if (playersInLineup.value.includes(player.player_id)) {
    notify({
      title: "Player Already in squad",
      type: "warning"
    })
    return
  }
  slots.value.forEach((s) => {
    if (s.slot === selectedSlot.value) {
      s.player = player
    }
  })

  selectedSlot.value = null
}

function emptyLineup() {
  slots.value = slots.value.map((s) => ({ ...s, player: {} }))
  console.log(slots.value)
}

onMounted(async () => {
  loading.value = "loading"
  await init()
  tacticsModal.value = true
})


const Illustrations = {
  direct: IllustrationsDirectTactics,
  balanced: IllustrationsBalancedTactics,
  gegenpress: IllustrationsGengenpressTactics,
  low_block: IllustrationsLowblockTactics,
  possession: IllustrationsPosessionTactics
}
</script>

<template>
  <UiModal v-if="tacticsModal" @close="tacticsModal = false" size="w-full md:w-2/3" title="Playing Tactics">
    <div>
      <h2 class="heading heading--small">Tactics</h2>
      <div class="grid md:grid-cols-5 gap-1 my-5">
        <button v-for="(tactics, key) in TACTICS" :key="key"
          class="flex flex-col p-3 cursor-pointer transition-all ease-brutal"
          :class="[state.style === tactics.id ? 'border-brutal border-cyan-500':'border-hairline border-void-500 hover:border-void-700']"
          @click="() => state.style = tactics.id">
          <component :is="Illustrations[tactics.id as 'direct']" class="hidden md:block w-full" />
          <div class="flex flex-col gap-1 text-left">
            <h2 class="font-semibold">{{ tactics.title }}</h2>
            <p class="text-sm">{{ tactics.about }}</p>
          </div>
        </button>
      </div>

      <h2 class="heading heading--small">formation</h2>
      <div class="max-sm:flex-col flex gap-5 md:items-center justify-between">
        <div class="mt-3 flex items-center gap-5">
          <button
            v-for="(formation, key) in formations"
            :key class="pill cursor-pointer"
            :class="{'bg-cyan-500 text-void-700': formation === state.formation }"
            @click="state.formation = formation"
          >
            {{ formation }}
          </button>
        </div>

        <UiButton @click="saveClubTactics()" width="[170px]" :loading="saving">
          Save
        </UiButton>
      </div>
    </div>
  </UiModal>

  <main class="h-full text-slate-200 font-mono space-y-5">
    <h2 class="page__header">Squad</h2>
    <UiLoader v-if="loading === 'loading'" />
    <template v-else-if="loading === 'loaded'">
      <section class="grid gap-5 md:grid-cols-3 md:row-span-2">
        <div class="p-5">
          <div class="flex items-center justify-between border-b-brutal pb-3 border-void-800">
            <h3 class="heading heading--small">Lineup</h3>

            <div class="flex gap-5 items-center justify-end">
              <UiButton @click="emptyLineup()" type="outline">
                Empty
              </UiButton>
              <UiButton @click="saveClubLineup()" :loading="saving">
                Save Lineup
              </UiButton>
            </div>
          </div>

          <PitchView variant="half" :formation="tactics?.formation ?? ''" :slots :clickable="true"
            @select="(e: number | null) => selectedSlot = e" :selected-slot />
        </div>
        <div class="border-brutal p-5 border-void-700 bg-void-900 md:block"
          :class="selectedSlot !== null ? 'block max-sm:fixed max-sm:bottom-0 max-sm:right-5 max-sm:left-5' : 'hidden'">
          <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
            <h3 class="heading heading--small">Squad</h3>

            <div v-if="selectedSlot !== null" class="text-sm">Select player for <b>{{ slot?.position }}</b>
            </div>
          </div>
          <div class="grid grid-cols-8 gap-2 mt-5">
            <div class="col-span-2 heading heading--small">Name</div>
            <div class="heading heading--small">Position</div>
            <div class="heading heading--small text-center">OVR</div>
            <div class="col-span-4 heading heading--small">Attributes</div>
          </div>
          <div class="mt-2 h-100 overflow-auto flex gap-3 flex-col"
            :class="selectedSlot !== null ? 'h-[40vh] overflow-auto' : ''">
            <div class="grid grid-cols-8 gap-2 text-sm py-1 border-b border-void-500"
              :class="{ 'bg-void-500/20': playersInLineup.includes(player.player.id) }"
              @click="saveSlot({ squad_number: player.squad_number, display_name: player.player.name, player_id: player.player.id })"
              v-for="(player, key) in playersBySelectedSlotPosition" :key>
              <div class="col-span-2 heading">
                <span class="cursor-pointer">{{ player.first_name }} {{ player.last_name }}</span>
              </div>
              <div class="heading">{{ player.position }}</div>
              <div class="heading text-center">{{ player.overall }}</div>
              <div class="heading col-span-4 flex gap-1 justify-around">
                <div v-if="player.position === 'GK'" class="flex gap-1 items-center">
                  <span class="heading heading--small">GK</span>{{ player.attributes.goalkeeping }}
                </div>
                <div class="flex gap-1 items-center">
                  <span class="heading heading--small">PHY</span>{{ player.attributes.physical }}
                </div>
                <div class="flex gap-1 items-center">
                  <span class="heading heading--small">MEN</span>{{ player.attributes.mental }}
                </div>
                <div class="flex gap-1 items-center">
                  <span class="heading heading--small">TAC</span>{{ player.attributes.tactical }}
                </div>
                <div class="flex gap-1 items-center">
                  <span class="heading heading--small">TECH</span>{{ player.attributes.technical }}
                </div>
              </div>
            </div>
          </div>
        </div>
        <div class="overflow-auto">
          <PlayerCard :player-id="playerId" v-if="playerId" />
        </div>
        <div class="overflow-auto">
        </div>
      </section>
    </template>
  </main>
</template>