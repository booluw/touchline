<script setup lang="ts">
import { useToast } from '~/components/ui/Toast'

const { getTactics, saveTactics, saveLineup } = useClub()
const { notify } = useToast()

const loading = ref<"loading" | "loaded" | "error">("loading")
const saving = ref(false)

const tactics = ref()
const squad = ref()
const lineup = ref()

const slots = ref([])
const selectedSlot = ref()
const playersBySelectedSlotPosition = computed(() => {
  const slot = lineup.value.slots.find((s) => s.slot === selectedSlot.value)
  const matched = squad.value.filter((s) => s?.position === slot?.position)

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
    slots.value = res.lineup.slots.map((s) => {
      const player = res.squad.players.find((p) => p?.player?.id === s.player.id)

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
  try {
    await saveLineup({ slots: slots.value.map((s) => ({ slot: s.slot, player_id: s?.player?.player_id ?? s?.player?.id })) })
    await init()
  } finally {
  }
}

function saveSlot(player: { squad_number: number, display_name: string, player_id: string }) {
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
      <section class="grid gap-5 grid-cols-3 row-span-2">
        <div class="p-5">
          <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
            <h3 class="heading heading--small">Tactics</h3>
            <div class="flex gap-4 items-center">
              <UiSelect
                v-model="state.style"
                :options="['balanced', 'possession', 'gegenpress', 'low_block', 'direct']"
              />
              <UiSelect v-model="state.formation" :options="tactics.allowed_formations" />
              <button @click="saveClubTactics()" class="button button--primary" :disabled="saving">Save</button>
            </div>
          </div>

          <PitchView variant="half" :formation="tactics.formation" :slots :clickable="true"
            @select="(e) => selectedSlot = e" :selected-slot />
          <div class="flex gap-5 items-center justify-end py-5">
            <button @click="emptyLineup()" class="button button--outline">Empty</button>
            <button @click="saveClubLineup()" class="button button--primary">Save Lineup</button>
          </div>
        </div>
        <div class="border-brutal p-5 border-void-700">
          <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
            <h3 class="heading heading--small">Squad</h3>
          </div>
          <div class="grid grid-cols-8 gap-2 mt-5">
            <div class="col-span-2 heading heading--small">Name</div>
            <div class="heading heading--small">Position</div>
            <div class="heading heading--small text-center">OVR</div>
            <div class="col-span-4 heading heading--small">Attributes</div>
          </div>
          <div class="mt-2 h-100 overflow-auto flex gap-3 flex-col">
            <div class="grid grid-cols-8 gap-2 text-sm py-1 border-b border-void-500"
              :class="{ 'bg-win-500/20': playersInLineup.includes(player.player.id) }"
              @click="saveSlot({ squad_number: player.squad_number, display_name: player.player.name, player_id: player.player.id })"
              v-for="(player, key) in playersBySelectedSlotPosition" :key>
              <div class="col-span-2 heading">
                <NuxtLink v-if="!selectedSlot" :to="`/play/players/${player.player.id}`"
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
          {{ selectedSlot }}
          <!-- {{ playersInLineup }}
          {{ slots }} -->
          <!-- {{ squad[0] }} -->
          <!-- {{ playersInLineup }} -->
        </div>
        <div class="overflow-auto">
          <!-- {{ slots }} -->
        </div>
        <div class="">
          <!-- {{ tactics.allowed_formations }} -->
        </div>
        <div>Kollow</div>
        <div class="">Challow</div>
      </section>
    </template>
  </main>
</template>