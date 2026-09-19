<script lang="ts" setup>
import type { Country, League, World } from '~/types';

definePageMeta({
  name: 'country-page',
  path: '/admin/world/:id/countries/:countryId'
})

const { fetchWorldLeagues } = useAdmin()
const route = useRoute()
const store = useAdminStore()

const worldId = computed(() => route.params.id)
const countryId = computed(() => route.params.countryId)

const world = computed(() => store.worlds?.find((w: World) => w.id === worldId.value ))
const country = computed(() => store.countries?.find((c: Country) => c.id === countryId.value))
const leagues = computed(() => store.leagues?.filter((l: League) => l.country_id === countryId.value) ?? [])

onMounted(() => fetchWorldLeagues(worldId.value))
</script>
<template>
  <section>
    <div class="flex justify-between items-center">
      <div class="flex flex-col gap-3 items-start mb-5">
        <span class="pill pill--success">{{ world.status }}</span>
        <h2 class="page__header">{{ country.name }} <span class="uppercase">[{{ country.code }}]</span>, {{ world.name }}</h2>
      </div>

      <nuxt-link
        :to="{ name: 'admin-CountryLeagues-create', params: { id: worldId, countryId }}"
        class="button button--outline"
      >
        Create New League
      </nuxt-link>
    </div>
  </section>
  <NuxtPage />
  {{ leagues }}
</template>