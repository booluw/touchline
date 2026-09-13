<template>
  <div>
    <nav class="border-b border-slate-800 bg-slate-950 px-5 py-3 text-sm">
      <div class="mx-auto flex max-w-5xl gap-5 text-slate-300">
        <NuxtLink to="/" class="font-bold text-white">Touchline</NuxtLink>
        <NuxtLink to="/squad" class="hover:text-white">Squad</NuxtLink>
        <NuxtLink to="/tactics" class="hover:text-white">Tactics</NuxtLink>
        <NuxtLink to="/training" class="hover:text-white">Training</NuxtLink>
        <NuxtLink to="/competitions" class="hover:text-white">Competitions</NuxtLink>
      </div>
    </nav>
    <NuxtLayout>
      <NuxtPage />
    </NuxtLayout>
  </div>
</template>
