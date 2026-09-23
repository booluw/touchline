<script lang="ts" setup>
const store = useAuthStore()

const ROUTES = computed(() => store.user?.is_admin ? ADMIN_ROUTES : MANAGER_ROUTES)
</script>

<template>
  <section class="min-h-screen px-5">
    <header class="border-b-brutal border-void-800 flex items-center justify-between">
      <nuxt-link to="/" class="font-mono flex gap-3 font-bold text-xl py-5">
        <img src="~/assets/svgs/logomark.svg" class="h-6" />
        TouchLine
      </nuxt-link>

      <nav class="">
        <nuxt-link
          :to="link.to" v-for="(link, key) in ROUTES.links"
          :key
          class="uppercase text-void-500 text-sm px-5 font-semibold font-mono hover:text-void-400 ease-brutal duration-80"
          router-link-exact-active="!text-cyan-500"
        >
          {{ link.text }}
        </nuxt-link>
      </nav>
      <nav class="">
        <nuxt-link v-if="!store.user" to="/login" class="button button--primary uppercase">
          start your career
        </nuxt-link>
        <nuxt-link :to="ROUTES.cta.to" v-else class="button button--primary uppercase">
          {{ ROUTES.cta.text }}
        </nuxt-link>
      </nav>
    </header>
    <section class="py-5">
      <slot />
    </section>
  </section>
</template>