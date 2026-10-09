<script lang="ts" setup>
import { useToast } from '~/components/old-ui/Toast';
import { useManagerOffer } from '~/composables/manager/offer';
import type { ManagerOffer } from '~/types/manager';

const store = useAuthStore()
const router = useRouter()
const { getOffers } = useManagerOffer()
const { notify } = useToast()

const offers = ref<ManagerOffer[]>([])
const takenClubs = ref<{ id: string, name: string }[]>([])
const offerToView = ref<ManagerOffer>()
const loading = ref("loading")

async function getManagerOffers() {
  if (offerToView.value) offerToView.value = undefined
  try {
    loading.value = "loading"
    const { offers: response, taken_clubs } = await getOffers()
    offers.value = response ?? []
    takenClubs.value = taken_clubs ?? []
    loading.value = "loaded"
  } catch {
    loading.value = "error"
  }
}

function offerAccepted() {
  notify({
    title: "Offer Accepted",
    type: "success"
  })

  store.$reset()
  router.replace("/")
}

onMounted(() => getManagerOffers())
</script>

<template>
  <section class="space-y-5">
    <h1 class="page__header">Offers</h1>

    <OldUiLoader v-if="loading === 'loading'" />
    <template v-else-if="loading === 'loaded'">
      <div v-if="takenClubs.length" role="status" class="bg-void-800 p-3 text-xs uppercase text-void-300">
        No longer available — another manager has taken
        <span class="font-semibold text-void-100">{{ takenClubs.map(c => c.name).join(", ") }}</span>.
        You can't be offered {{ takenClubs.length === 1 ? "this club" : "these clubs" }} again.
      </div>
      <div v-if="offers.length === 0" class="p-10 uppercase text-xs text-void-400 text-center">
        No offers for you at this time.
      </div>
      <div v-else class="grid grid-cols-6">
        <div v-for="(offer, key) in offers" :key class="bg-void-800 p-3">
          <div class="font-mono">
            <div class="flex items-center justify-between">
              <h3 class="uppercase text-lg font-bold">{{ offer.club.name }}</h3>
              <button class="heading heading--small cursor-pointer text-cyan-500 underline" @click="() => offerToView = offer">View</button>
            </div>
            <div class="mt-3">
              <h4 class="heading heading--small">financials</h4>
              <div class="flex justify-between">
                <h4 class="">Transfer Budget:</h4>
                <p class="font-semibold">{{ formatMoneyCompact(offer.finance.transfer_budget.available) }}</p>
              </div>
              <div class="flex justify-between">
                <h4 class="">Wage Budget:</h4>
                <p class="font-semibold">{{ formatMoneyCompact(offer.finance.wage_budget.available) }}</p>
              </div>
            </div>
            <div class="mt-3 pt-3 border-t border-void-600">
              <h4 class="heading heading--small">board</h4>
              <div class="flex justify-between">
                <h4 class="">Persona:</h4>
                <p class="font-semibold capitalize">{{ offer.board.persona.split("_").join(" ") }}</p>
              </div>
            </div>
            <div class="mt-3 pt-3 border-t border-void-600">
              <h4 class="heading heading--small">competition</h4>
              <div class="flex justify-between">
                <nuxt-link :to="`/play/league/${offer.league.id}`" class="text-cyan-700">{{ offer.league.name
                }}:</nuxt-link>
                <p class="font-semibold capitalize">{{ offer.league.position ?? "-" }}</p>
              </div>
              <div class="flex justify-between">
                <h4 class="">Form:</h4>
                <p class="font-semibold capitalize">
                  P{{ offer.league.played }}
                  W{{ offer.league.won }}
                  D{{ offer.league.drawn }}
                  L{{ offer.league.lost }}
                </p>
              </div>
            </div>
          </div>
        </div>

        <OfferView v-if="offerToView" :offer="offerToView" @close="offerToView = undefined"
          @done="(e: boolean) => e ? offerAccepted() : getManagerOffers()" />
      </div>
    </template>
    <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
      <div class="text-loss-500">
        An Error Occurred
      </div>
      <button class="uppercase text-cyan-300" @click="getManagerOffers()">Retry</button>
    </div>
  </section>
</template>