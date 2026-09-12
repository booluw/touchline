<template>
  <div class="min-h-screen flex items-center justify-center">
    <div class="w-full max-w-sm space-y-4">
      <h1 class="text-2xl font-bold text-white">Sign in to Touchline</h1>

      <form v-if="!needsWorldSelection" class="space-y-4" @submit.prevent="submitLogin">
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
        <p v-if="errorMessage" class="text-sm text-red-400">{{ errorMessage }}</p>
        <button
          type="submit"
          class="w-full px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-500"
        >
          Sign in
        </button>
      </form>

      <form v-else class="space-y-4" @submit.prevent="confirmWorld">
        <p class="text-sm text-slate-300">You manage clubs in more than one world — pick one to sign in to.</p>
        <div class="space-y-2">
          <button
            v-for="world in worlds"
            :key="world.id"
            type="button"
            class="w-full px-4 py-3 text-left rounded-lg bg-slate-900 border border-slate-700 text-white hover:border-blue-500"
            @click="selectWorld(world.id); confirmWorld()"
          >
            {{ world.name }}
          </button>
        </div>
        <button
          type="button"
          class="text-sm text-slate-400 hover:text-white"
          @click="needsWorldSelection = false"
        >
          Back
        </button>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
const { login, selectWorld, worlds, needsWorldSelection } = useAuth()

const email = ref('')
const password = ref('')
const errorMessage = ref('')

async function submitLogin() {
  errorMessage.value = ''
  try {
    const res = await doLogin()
    if ((res as LoginWorldPicker).status === 'worlds') {
      return
    }
    navigateTo('/')
  } catch (err) {
    errorMessage.value = err instanceof Error ? err.message : 'Sign-in failed.'
  }
}

async function confirmWorld() {
  const res = await doLogin()
  // The world picker logged in with cookies on this call; land on the home
  // status panel, which opens the live socket.
  if ((res as LoginWorldPicker).status !== 'worlds') {
    navigateTo('/')
  }
}

async function doLogin() {
  const res = await login(email.value, password.value)
  // A real session now exists; open the single realtime socket. World-picker
  // responses set no cookies yet, so wait until the user has picked one.
  if ((res as LoginWorldPicker).status !== 'worlds') {
    useRealtimeStore().connect()
  }
  return res
}
</script>