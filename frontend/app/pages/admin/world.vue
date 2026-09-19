<script lang="ts" setup>
import type { World } from '~/types';

const { fetchWorlds } = useAdmin()
const store = useAdminStore()

const worlds = computed<World[]>(() => store.worlds)
const worldCardType = {
  provisioning: "",
  seeding: "",
  active: "card--success",
  paused: "card--warn",
  archived: "card--danger"
}

onMounted(() => fetchWorlds())
</script>

<template>
  <section class="">
    <h1 class="page__header">
      Worlds
    </h1>

    <div class="mt-5">
      <div class="grid md:grid-cols-5 gap-5">
        <div
          class="card"
          :class="worldCardType[world.status]"
          v-for="world in worlds"
          :key="world.id"
        >
          <h4 class="card__heading card__heading--small">{{ world.status }}</h4>
          <h3 class="card__heading">{{ world.name }}</h3>
          <p class="card__text">{{ world.created_at }}</p>

          <nuxt-link :to="`/admin/world/${world.id}`" class="card__link">Edit World</nuxt-link>
        </div>
      </div>
    </div>
  </section>
  <NuxtPage />
</template>