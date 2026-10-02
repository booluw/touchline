<script setup lang="ts">
import { useToast } from '~/components/ui/Toast'

const { getTactics, saveTactics, saveLineup } = useClub()
const { notify } = useToast()

const loading = ref<"loading" | "loaded" | "error">("loading")
const saving = ref(false)

type SlotPlayer = { id?: string, player_id?: string, display_name?: string, squad_number?: number }
type Slot = { slot: number, position?: string, player: SlotPlayer }
type SquadPlayer = {
  position?: string, squad_number?: number, first_name?: string, last_name?: string, overall?: number,
  attributes: Record<'goalkeeping' | 'physical' | 'mental' | 'tactical' | 'technical', number>,
  player: { id: string, name: string }
}

const tactics = ref<{ style: string, formation: string, allowed_formations: string[] }>()
const squad = ref<SquadPlayer[]>([])
const lineup = ref<{ formation: string, slots: Slot[] }>()

const slots = ref<Slot[]>([])
const selectedSlot = ref<number|null>(null)
const slot = computed(() => lineup.value?.slots.find((s) => s.slot === selectedSlot.value))
const playersBySelectedSlotPosition = computed(() => {
  const matched = squad.value.filter((s) => s?.position === slot.value?.position)

  return [...new Map([...matched, ...squad.value].map(player => [player.player.id, player])).values()]
})

const playersInLineup = computed(() => {
  return slots.value.map((s) => s?.player?.player_id ?? s?.player?.id ?? false)
})

const state = ref({
  style: '',
  formation: ''
})

async function init() {
  try {
    const res = await getTactics()

    tactics.value = res.tactics
    lineup.value = res.lineup
    squad.value = res.squad.players

    state.value.style = res.tactics.style
    state.value.formation = res.lineup.formation
    slots.value = res.lineup.slots.map((s: Slot) => {
      const player = res.squad.players.find((p: SquadPlayer) => p?.player?.id === s.player.id)

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
  }
}

async function saveClubTactics() {
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
})

</script>

<template>
  <main class="h-full text-slate-200 font-mono space-y-5">
    <h2 class="page__header"></h2>
    <UiLoader v-if="loading === 'loading'" />
    <template v-else-if="loading === 'loaded'">
      <section class="grid gap-5 md:grid-cols-3 md:row-span-2">
        <div class="p-5">
          <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
            <h3 class="heading heading--small">Tactics</h3>
            <div class="flex gap-4 items-center">
              <UiSelect
                v-model="state.style"
                :options="['balanced', 'possession', 'gegenpress', 'low_block', 'direct']"
              />
              <UiSelect v-model="state.formation" :options="tactics?.allowed_formations ?? []" />
              <UiButton
                @click="saveClubTactics()"
                :loading="saving"
              >
                Save
              </UiButton>
            </div>
          </div>

          <PitchView variant="half" :formation="tactics?.formation ?? ''" :slots :clickable="true"
            @select="(e: number | null) => selectedSlot = e" :selected-slot />
          <div class="flex gap-5 items-center justify-end py-5">
            <UiButton
              @click="emptyLineup()"
              type="outline"
            >
              Empty
            </UiButton>
            <UiButton
              @click="saveClubLineup()"
              :loading="saving"
            >
              Save Lineup
            </UiButton>
          </div>
        </div>
        <div class="border-brutal p-5 border-void-700 bg-void-900 md:block" :class="selectedSlot !== null ? 'block fixed bottom-1 right-5 left-5' : 'hidden'">
          <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
            <h3 class="heading heading--small">Squad</h3>

            <div v-if="selectedSlot !== null" class="text-sm md:hidden">Select player for <b>{{ slot?.position }}</b></div>
          </div>
          <div class="grid grid-cols-8 gap-2 mt-5">
            <div class="col-span-2 heading heading--small">Name</div>
            <div class="heading heading--small">Position</div>
            <div class="heading heading--small text-center">OVR</div>
            <div class="col-span-4 heading heading--small">Attributes</div>
          </div>
          <div class="mt-2 h-100 overflow-auto flex gap-3 flex-col" :class="selectedSlot !== null ? 'h-[40vh] overflow-auto' : ''">
            <div class="grid grid-cols-8 gap-2 text-sm py-1 border-b border-void-500"
              :class="{ 'bg-win-500/20': playersInLineup.includes(player.player.id) }"
              @click="saveSlot({ squad_number: player.squad_number, display_name: player.player.name, player_id: player.player.id })"
              v-for="(player, key) in playersBySelectedSlotPosition" :key>
              <div class="col-span-2 heading">
                <NuxtLink v-if="selectedSlot === null" :to="`/play/players/${player.player.id}`"
                  class="hover:text-slate-100 underline-offset-2 hover:underline">
                  {{ player.first_name }} {{ player.last_name }}
                </NuxtLink>
                <span v-else class="cursor-move">{{ player.first_name }} {{ player.last_name }}</span>
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
        </div>
        <div class="overflow-auto">
        </div>
      </section>
    </template>
  </main>
</template>