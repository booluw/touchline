<script lang="ts" setup>
import z from "zod"

const router = useRouter()
const { login } = useAuth()

const schema = z.object({ email: z.email(), password: z.string().min(5, "Should be greater than 5 characters") })
const state = ref({ email: '', password: '' })
const loading = ref(false)

async function logUserIn(valid: boolean) {
  if (valid) {
    loading.value = true

    try {
      await login(state.value)
    } finally {
      loading.value = false
    }
  }
}

definePageMeta({ layout: "auth" })
useSeoMeta({
  title: "Log in",
  description: "Login and continue your journey on Touchline, Always good to have you back."
})
</script>

<template>
  <div class="md:w-95">
    <UiForm class="flex flex-col gap-10 md:gap-5" @submit="logUserIn" :state :schema>
      <div class="flex flex-col gap-5">
        <div class="">
          <h1 class="text-page">Welcome back</h1>
          <p class="text-t3">Always good to have you back.</p>
        </div>
        <UiFormItem v-model="state.email" as="input" label="email" prop="email" placeholder="you@example.com" />
        <UiFormItem v-model="state.password" as="input" label="password" prop="password" placeholder="At least 5 characters">
          <nuxt-link to="" class="text-info text-meta">Forgot password?</nuxt-link>
        </UiFormItem>
      </div>
      <UiButton size="touch" type="submit" variant="primary" :loading>Continue</UiButton>
    </UiForm>

    <div class="mt-5 text-t3 text-center">
      New to Touchline?
      <nuxt-link to="/register" class="text-info">Create a manager</nuxt-link>
    </div>
  </div>
</template>