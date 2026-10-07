<script lang="ts" setup>
import { useManagerDashboard } from '~/composables/manager/dashboard';
import type { ManagerDashboard, ManagerDashboardItem, ManagerDashboardSummary } from '~/types/manager';
import type { LoadingStatus } from '~/types';
import type { AttentionTier } from '~/types/ui/design';

const emits = defineEmits<{
  (e: 'summary', summary: ManagerDashboardSummary[]): void
}>()

const { getDashboardData } = useManagerDashboard()

const status = ref<LoadingStatus>("loading")
const data = ref<ManagerDashboard>()
const tiers: AttentionTier[] = ["urgent", "important", "interesting"]

async function load() {
  status.value = "loading"
  try {
    data.value = await getDashboardData()
    emits("summary", data.value!.summary)
    status.value = "loaded"
  } catch (error) {
    console.log(error)
    status.value = "error"
  }
}

function onAction(item: ManagerDashboardItem<"urgent" | "important" | "interesting">) {
  switch (item.action.kind) {
    case "set_lineup":
      // Router user to Squad Page
      navigateTo(`/play/squad?referralId=${item.id}`)
      return;
    case "view_fixture":
      navigateTo(``)
      return
    case "view_standings":
      navigateTo(``)
      return
  }
}

onMounted(load)
</script>

<template>
  <section class="space-y-5 md:space-y-10">
    <template v-if="status === 'loading'">
      <div class="space-y-10">
        <div class="hidden md:flex flex-col gap-2">
          <UiLoader class="w-72 h-5" />
          <UiLoader class="w-42 h-3" />
        </div>

        <slot />

        <div class="space-y-5">
          <div v-for="i in 3" :key="i" class="flex flex-col gap-2">
            <div class="font-geist flex items-center gap-2">
              <h3 class="text-meta font-semibold uppercase tracking-wide-brutal text-t1">
                <UiLoader class="w-12 h-3" />
              </h3>
              <span class="num text-label text-t3">
                <UiLoader class="w-12 h-3" />
              </span>
              <span class="h-px flex-1 bg-line" aria-hidden="true" />
            </div>

            <UiLoader class="w-full h-20 rounded-card" />
          </div>
        </div>
      </div>
    </template>
    <template v-else-if="status === 'error'"></template>
    <template v-else>
      <div class="flex flex-col gap-1">
        <h1 class="text-page">What needs your attention</h1>
        <p class="text-t2">
          {{ data?.counts.urgent }} urgent, {{ data?.counts.important }} important,
          {{ data?.counts.interesting }} intresting since your last visit.
        </p>
      </div>
      <slot />
      <div v-if="data" class="flex flex-col gap-5">
        <div v-for="(tier, key) in tiers" :key class="flex flex-col gap-2">
          <UiTierHeader :tier :count="data.counts[tier as 'urgent']" />
          <UiCard v-if="data[tier as 'urgent'].length !== 0" flush>
            <UiAttentionItem
              v-for="item in data[tier as 'urgent']" :tier
              :tag="item.category" :title="item.title"
              :consequence="item.description"
              :action-label="item.action.kind.replace('_', ' ')"
              :explainable="!!item.explanation"
              @action="onAction(item)"
            />
          </UiCard>
        </div>
      </div>
    </template>
  </section>
</template>