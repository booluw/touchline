<script lang="ts" setup>
import type { World } from '~/types';
import Country from '~/types';

const { fetchCountry } = useAdmin()
const store = useAdminStore()
const router = useRouter()
const route = useRoute()

const world = computed<World>(() => store.worlds.find((world: World) => world.id === route.params.id))
const countries = computed<Country[]>(() => store.countries?.filter((cou: Country) => cou.world_id === world.value.id))
onMounted(() => fetchCountry(world.value.id))
</script>

<template>
  <UiSlide
    :title="world.name"
    :description="`All countries under World: ${world.name}`"
    @close="() => router.push({ name: 'admin-world' })"
  >
    <div class="flex items-center justify-end mb-5">
      <nuxt-link :to="{ name: 'admin-world-id-create' }" class="button button--primary">
        Create Country
      </nuxt-link>
    </div>

    <div class="grid grid-cols-3 gap-5">
      <nuxt-link
        :to="{ name: 'country-page', params: { id: country.world_id, countryId: country.id }}"
        v-for="country in countries"
        :key="country.id"
        class="card card--success"
      >
        <h4 class="card__heading card__heading--small">{{ country.code }}</h4>
        <h3 class="card__heading">{{ country.name }}</h3>
      </nuxt-link>
    </div>
  </UiSlide>
  <NuxtPage />
</template>