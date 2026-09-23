<script lang="ts" setup>
import type { Offer } from '~/types';
import { formatMoneyCompact } from '../../utils/helpers';
import { useManagerOffer } from '~/composables/manager/offer';
import { useToast } from '../ui/Toast';

const props = defineProps<{ offer: Offer }>()
const emit = defineEmits(["close", "done"])

const { acceptOffer, rejectOffer } = useManagerOffer()
const { notify } = useToast()

const loading = ref(false)

async function acceptJobOffer() {
  if (loading.value) return

  try {
    loading.value = true
    await acceptOffer(props.offer.id)
    notify({
      title: "Offer Accepted",
      description: `You're now the new manager of ${props.offer.club.name}`,
      type: "success"
    })

    emit('done', true)
  } catch {
    emit("close")    
  } finally {
    loading.value = false
  }
}
async function rejectJobOffer() {
  if (loading.value) return

  try {
    loading.value = true
    await rejectOffer(props.offer.id)    
    emit("done", false)
  } catch {
    emit("close")
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <UiModal size="w-2/4 h-[500px]" :title="`${offer.club.name} want you to be their next manager`" @close="emit('close')">
    <div class="h-80 overflow-auto space-y-10">
      <div class="space-y-1">
        <h3 class="heading heading--small">finance</h3>
        <div class="grid gap-5 grid-cols-2 lg:grid-cols-3">
          <div class="flex justify-between">
            <h4 class="">Transfer Available:</h4>
            <p class="font-semibold">
              {{ formatMoneyCompact(offer.finance.transfer_budget.available) }}
            </p>
          </div>

          <div class="flex justify-between">
            <h4 class="">Wage Available:</h4>
            <p class="font-semibold">
              {{ formatMoneyCompact(offer.finance.wage_budget.available) }}
            </p>
          </div>

          <div class="flex justify-between">
            <h4 class="">Wage Commitment:</h4>
            <p class="font-semibold">
              {{ formatMoneyCompact(offer.finance.wage_commitments.annual_wage) }}
            </p>
          </div>

          <div class="flex justify-between">
            <h4 class="">Projected Revenue:</h4>
            <p class="font-semibold">
              {{ formatMoneyCompact(offer.finance.projected_revenue) }}
            </p>
          </div>

          <div class="flex justify-between">
            <h4 class="">Projected Year End Bal.:</h4>
            <p class="font-semibold">
              {{ formatMoneyCompact(offer.finance.projected_year_end_balance) }}
            </p>
          </div>

          <div class="flex justify-between">
            <h4 class="">Debt:</h4>
            <p class="font-semibold">
              {{ formatMoneyCompact(offer.finance.debt) }}
            </p>
          </div>
        </div>
      </div>
      
      <div class="">
        <h3 class="heading heading--small">board</h3>
        <div class="grid gap-y-1 gap-x-5 grid-cols-2 lg:grid-cols-3">
          <div class="flex justify-between">
            <h4 class="">Persona:</h4>
            <p class="font-semibold capitalize">
              {{ offer.board.persona.split("_").join(" ") }}
            </p>
          </div>

          <div class="flex justify-between items-start" :class="{ 'col-span-2' : mandate.target_value === '0'}" v-for="(mandate, key) in offer.board.mandates" :key>
            <h4 class="capitalize">{{ mandate.target_type.split("_").join(" ") }}</h4>
            <p class="font-semibold">
              {{ mandate.target_value === "0" ? mandate.description : mandate.target_value }}
            </p>
          </div>
        </div>
      </div>

      <div class="">
        <h3 class="heading heading--small">competition</h3>
        <div class="grid gap-y-1 gap-x-5 grid-cols-2 lg:grid-cols-3">
          <div class="flex justify-between">
            <h4 class="">League:</h4>
            <p class="font-semibold capitalize">
              {{ offer.league.name }}
            </p>
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

      <div class="">
        <h3 class="heading heading--small">squad</h3>
        <div class="grid gap-y-1 gap-x-5 grid-cols-2 lg:grid-cols-3">
          <div class="flex justify-between">
            <h4 class="">Size:</h4>
            <p class="font-semibold capitalize">
              {{ offer.squad.size }}
            </p>
          </div>
          <div class="flex justify-between">
            <h4 class="">Top Player:</h4>
            <p class="font-semibold capitalize">
              {{ offer.squad.top_player.name }} ({{ offer.squad.top_player.position }})
            </p>
          </div>
        </div>
      </div>
    </div>

    <div class="flex justify-end gap-5 py-5">
      <button class="button button--danger" @click.once="rejectJobOffer()">Decline</button>
      <button class="button button--primary" @click.once="acceptJobOffer()">Accept</button>
    </div>
  </UiModal>
</template>