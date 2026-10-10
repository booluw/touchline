<script lang="ts" setup>
import type { ManagerDashboardSummary } from '~/types/manager';
const viewport = useViewport()

const summary = ref<ManagerDashboardSummary[]>()
</script>

<template>
  <section class="grid gap-10 md:grid-cols-3">
    <div class="md:col-span-2">
      <DashboardAttentionPanel @summary="(e) => summary = e">
        <DashboardCardCluster v-if="viewport.isLessThan('tablet')" :summary />
      </DashboardAttentionPanel>
    </div>
    <div v-if="!viewport.isLessThan('tablet')" class="flex flex-col gap-5">
      <DashboardNextFixture />
      <DashboardCardCluster :summary />
      <DashboardFinancialCard />
    </div>
  </section>
</template>