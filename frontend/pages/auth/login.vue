<template>
  <div class="min-h-screen flex items-center justify-center">
    <form class="w-full max-w-sm space-y-4" @submit.prevent="login">
      <h1 class="text-2xl font-bold text-white">Sign in to Touchline</h1>
      <div>
        <label class="block text-sm text-slate-400 mb-1" for="email">Email</label>
        <input
          id="email"
          v-model="email"
          type="email"
          required
          class="w-full px-4 py-2 rounded-lg bg-slate-900 border border-slate-700 text-white"
        />
      </div>
      <div>
        <label class="block text-sm text-slate-400 mb-1" for="password">Password</label>
        <input
          id="password"
          v-model="password"
          type="password"
          required
          class="w-full px-4 py-2 rounded-lg bg-slate-900 border border-slate-700 text-white"
        />
      </div>
      <button
        type="submit"
        class="w-full px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-500"
      >
        Sign in
      </button>
    </form>
  </div>
</template>

<script setup lang="ts">
const email = ref('')
const password = ref('')

async function login() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { data, error } = await useFetch(`${apiBase}/api/auth/login`, {
    method: 'POST',
    body: { email: email.value, password: password.value },
  })

  if (error.value) {
    alert('Sign-in failed. Check your credentials.')
    return
  }

  // On success, server sets httpOnly cookies (access + refresh)
  // Then route to home dashboard
  navigateTo('/dashboard')
}
</script>