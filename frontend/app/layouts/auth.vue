<template>
  <section class="h-dvh bg-bg grid md:grid-cols-2 text-body text-t1 font-geist">
    <div class="p-3.5 md:p-5 grid grid-rows-8">
      <header class="flex">
        <nuxt-link to="/" class="text-meta font-bold">Touchline</nuxt-link>
      </header>

      <div class="row-start-2 md:row-start-3 row-end-8 flex flex-col md:justify-center md:items-center">
        <slot />
      </div>
    </div>
    <div class="hidden md:flex border-l border-line bg-s1 items-center">
      <div class="">
      </div>
    </div>
  </section>
</template>