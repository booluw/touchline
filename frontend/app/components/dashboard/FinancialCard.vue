<script lang="ts" setup>
import { useManagerFinance } from '~/composables/manager/finance';
import type { LoadingStatus } from '~/types';
import type { ManagerDashboardSummary } from '~/types/manager';

const { getFinancialSummary } = useManagerFinance()
const finstore = useFinanceStore()

const status = ref<LoadingStatus>("loaded")
const summary = computed(() => finstore.summary) 

onMounted(async () => {
  if (!summary.value) status.value = "loading"
  await getFinancialSummary()
  status.value = "loaded"
})
</script>

<template>
  <UiCard class="flex flex-col gap-1.5">
    <div class="flex items-center justify-between">
      <span class="label-caps">finances</span>
      <span class="text-label text-t3">This season</span>
    </div>
    <div class="flex items-center justify-between">
      <span class="text-t2">Balance</span>
      <UiLoader class="w-20 h-3" v-if="status === 'loading'" />
      <div v-else class="flex items-center gap-3">
        <span>{{ formatMoneyCompact(summary!.cash) }}</span>
        <!-- <span class="text-urgent text-label">hello</span> -->
      </div>
    </div>

    <div class="flex items-center justify-between">
      <span class="text-t2">Wage bill / wk</span>
      <UiLoader class="w-20 h-3" v-if="status === 'loading'" />
      <div v-else class="flex items-center gap-2">
        <span>{{ formatMoneyCompact(summary!.wage_budget.allocated) }}</span>
        <span v-if="summary?.wage_commitments.weekly_wage" class="text-urgent text-label">- {{ formatMoneyCompact(summary?.wage_commitments.weekly_wage) }}</span>
      </div>
    </div>

    <div class="flex items-center justify-between">
      <span class="text-t2">Transfer budget</span>
      <UiLoader class="w-20 h-3" v-if="status === 'loading'" />
      <div v-else class="flex items-center gap-2">
        <span>{{ formatMoneyCompact(summary!.transfer_budget.allocated) }}</span>
        <span v-if="summary?.transfer_budget.committed" class="text-urgent text-label">- {{ formatMoneyCompact(summary?.transfer_budget.committed) }}</span>
      </div>
    </div>
  </UiCard>
</template>