<script lang="ts" setup>
import type { Player } from '~/types/player';

const props = defineProps<{
  playerId: string
}>()

const { getPlayer } = usePlayer()

const player = ref<Player>()
const loading = ref<'loading'|'loaded'|'error'>("loading")

async function fetchPlayerData() {
  loading.value = "loading"
  try {
    player.value = await getPlayer(props.playerId)
    loading.value = "loaded"
  } catch {
   loading.value = "error" 
  }
}

watch(() => props.playerId, () => fetchPlayerData())

onMounted(async () => {
  await fetchPlayerData()
})
</script>

<template>
  <section class="">
    <OldUiLoader v-if="loading === 'loading'" />
    <template v-else-if="loading === 'loaded' && player">
      <div class="flex gap-5 justify-between">
        <div class="flex gap-5">
          <EmptyPlayer class="w-24" />
          <div class="">
            <div class="flex items-center gap-2">
              <h2 class="text-xl font-semibold">
                {{ player?.first_name }} {{ player?.last_name }}
              </h2>
              <!-- <img :src="`https://flagsapi.com/${player?.nationality.toUpperCase()}/flag/24.png`" class="bg-transparent" /> -->
            </div>
            <div class="flex items-center">
              <nuxt-link :to="`/clubs/${player?.club.id}`" class="underline text-cyan-500">{{ player?.club.name }}</nuxt-link>

              <svg class="w-10 fill-void-500" viewBox="0 0 256 256">
                <path d="M128,96a32,32,0,1,0,32,32A32,32,0,0,0,128,96Zm0,48a16,16,0,1,1,16-16A16,16,0,0,1,128,144Z">
                </path>
              </svg>

              <div class="">
                {{ player.squad_number }} {{ player?.position }}
              </div>
            </div>
            <div class="heading heading--small mt-5 flex gap-3">
              <div>
                Played <span class="text-white">{{ player?.career.appearances }}</span>
              </div>
              <div>
                Goals <span class="text-white">{{ player?.career.goals }}</span>
              </div>
              <div>
                Assists <span class="text-white">{{ player?.career.assists }}</span>
              </div>
            </div>
          </div>
        </div>
        <div class="text-right">
          <h4 class="heading heading--small">average</h4>
          <div class="text-5xl text-void-500 font-bold">
            {{ player.overall }}
          </div>
        </div>
      </div>

      <div class="mt-3 py-2 grid grid-cols-4">
        <div class="">
          <h3 class="heading heading--small">value</h3>
          <h2 class="heading text-lg">{{ formatMoneyCompact(player.dossier.bio.market_value) }}</h2>
        </div>

        <div class="" v-if="player.dossier.private.contracts.length !== 0">
          <h3 class="heading heading--small">wsalary</h3>
          <h2 class="heading text-lg">{{ formatMoneyCompact(player.dossier.private.contracts[0]?.weekly_wage as number) }}</h2>
        </div>

        <div class="" v-if="player.dossier.private.contracts.length !== 0">
          <h3 class="heading heading--small">release fee</h3>
          <h2 class="heading text-lg">{{ formatMoneyCompact(player.dossier.private.contracts[0]?.release_clause as number) }}</h2>
        </div>

        <div class="text-center" v-if="player.dossier.private.contracts.length !== 0">
          <h3 class="heading heading--small">contract ends</h3>
          <h2 class="heading text-lg">{{ player.dossier.private.contracts[0]?.end_date.split("T")[0] }}</h2>
        </div>
      </div>

      <div class="mt-5">
        <AccordionRoot
          class="border border-void-500"
          default-value="stats"
          type="single"
          :collapsible="true"
        >
          <AccordionItem value="stats">
            <AccordionHeader class="border-b p-3 heading heading--small bg-void-800">
              <AccordionTrigger class="heading heading--small text-cyan-600 cursor-pointer">
                Stats
              </AccordionTrigger>
            </AccordionHeader>
            <AccordionContent class="p-3 border-b border-void-600 h-[25vh] overflow-auto">
              <div class="grid gap-3">
                <div class="mb-3">
                  <h3 class="capitalize">Match Readiness</h3>
                  <div class="grid grid-cols-3 gap-x-5 gap-y-2">
                    <Bar :value="100 - (player.dossier.condition.fatigue * 100)" label="strength" />
                    <Bar :value="100 - (player.dossier.condition.sharpness * 100)" label="sharpness" inverse />
                    <Bar :value="100 - (player.dossier.condition.injury_risk * 100)" label="injury risk" inverse />
                  </div>
                </div>
                
                <div v-for="(attr, key) in Object.keys(player.dossier.attribute_values).reverse()" :key>
                  <h3 class="capitalize">{{ attr }}</h3>

                  <div class="grid grid-cols-3 gap-x-5 gap-y-2">
                    <template v-for="(value, label, i) in player.dossier.attribute_values[attr as 'mental']" :key="i">
                      <Bar :value :label />
                    </template>
                  </div>
                </div>

                <div class="grid grid-cols-3 gap-x-5 gap-y-2">
                  <Bar :value="player.dossier.personality.leadership" label="leadership" />
                  <Bar :value="player.dossier.personality.ambition" label="ambition" />
                  <Bar :value="player.dossier.personality.ego" label="ego" inverse />
                </div>
              </div>
            </AccordionContent>
          </AccordionItem>

          <AccordionItem value="history">
            <AccordionHeader class="border-b p-3 heading heading--small bg-void-800">
              <AccordionTrigger class="heading heading--small text-cyan-600 cursor-pointer">
                History
              </AccordionTrigger>
            </AccordionHeader>
            <AccordionContent class="p-3 border-b border-void-600 h-[25vh] overflow-auto">
              WIP
            </AccordionContent>
          </AccordionItem>
        </AccordionRoot>
      </div>
    </template>
  </section>
</template>