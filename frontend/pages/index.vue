<template>
  <div class="min-h-screen flex items-center justify-center">
    <main class="max-w-6xl mx-auto px-6 py-16 text-center">
      <h1 class="text-5xl font-bold text-white mb-4">Touchline</h1>
      <p class="text-lg text-slate-400 mb-8">
        The persistent multiplayer football universe.
      </p>
      <p class="text-slate-500 max-w-2xl mx-auto">
        Every club has a personality. Every player has a story. Every decision has consequences.
      </p>
      <div class="mt-10 space-x-4">
        <NuxtLink
          to="/auth/login"
          class="inline-block px-6 py-3 bg-blue-600 text-white rounded-lg hover:bg-blue-500"
        >
          Sign in
        </NuxtLink>
      </div>
    </main>
  </div>
</template>