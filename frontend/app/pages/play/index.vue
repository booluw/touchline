<script lang="ts" setup>
import type { ManagerDashboardSummary } from '~/types/manager';

const summary = ref<ManagerDashboardSummary[]>()
</script>

<template>
  <section class="grid gap-5 md:grid-cols-3">
    <div class="md:col-span-2">
      <DashboardRightPanel @summary="(e) => summary = e" />
    </div>
    <div class="hidden md:block">
      {{ summary }}
    </div>
  </section>
</template>