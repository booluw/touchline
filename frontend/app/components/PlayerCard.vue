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
  } catch (error) {
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
    <UiLoader v-if="loading === 'loading'" />
    <template v-else-if="loading === 'loaded'">
      <div class="flex gap-5">
        <EmptyPlayer class="w-24" />
        <div class="">
          <div class="flex items-center gap-2">
            <h2 class="text-xl font-semibold">
              {{ player?.first_name }} {{ player?.last_name }}
            </h2>
            <!-- <img :src="`https://flagsapi.com/${player?.nationality.toUpperCase()}/flag/24.png`" class="bg-transparent" /> -->
          </div>
          <div class="">
            {{ player?.position }},
            <nuxt-link :to="`/clubs/${player?.club.id}`" class="underline text-cyan-500">{{ player?.club.name }}</nuxt-link>
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

      <div class="">
        <Bar :value="70" label="Shooting" class="w-6" />
      </div>
    </template>
    <!-- {{ playerId }} -->
    <!-- {{ player }} -->
  </section>
</template>