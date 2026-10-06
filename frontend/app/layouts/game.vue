<script setup lang="ts">
import type { DisplayDensity } from '~/types/ui/design'

// Opt in per page with definePageMeta({ layout: 'game' }).
// ponytail: season/date/fixture/inbox are placeholders until a dashboard endpoint feeds the shell.
const auth = useAuthStore()
const density = ref<DisplayDensity>('standard')
const club = computed(() => ({
  name: auth.club?.name ?? 'Your club',
  short: auth.club?.short ?? '—',
}))
</script>

<template>
  <UiAppShell v-model:density="density" :club="club" :items="GAME_NAV" season="" date="">
    <slot />
  </UiAppShell>
</template>
