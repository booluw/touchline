<template>
  <section class="h-dvh bg-bg font-geist text-t1">
    <div class="flex flex-col justify-center items-center">
      <div class="w-7xl my-0 mx-auto p-3.5 md:p-5">
        <header class="flex items-center">
          <nuxt-link to="/">Touchline</nuxt-link>
        </header>

        <slot />
      </div>
    </div>
  </section>
</template>