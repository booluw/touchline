<script lang="ts" setup>
import z from 'zod';


const router = useRouter()
const { register } = useAuth()

const schema = z.object({ email: z.email(), password: z.string().min(5, "Should be atleast 5 chars"), display_name: z.string().min(5, "Should be 5 char. or more") })
const state = ref({ email: "", password: "", display_name: "" })
const loading = ref(false)

async function registerUser(valid: boolean) {
  if (valid) {
    loading.value = true

    try {
      await register(state.value)
    } finally {
      loading.value = false
    }
  }
}

definePageMeta({ layout: "auth" })
useSeoMeta({
  title: "Register",
  description: "Create your manager, and start your journey on Touchline"
})
</script>

<template>
  <div class="md:w-95">
    <UiForm class="flex flex-col gap-20 md:gap-5" @submit="registerUser" :state :schema>
      <div class="flex flex-col gap-5">
        <div class="">
          <h1 class="text-page">Create your manager</h1>
          <p class="text-t3">Next you'll choose a club. You can rename your manager later.</p>
        </div>
        <UiFormItem v-model="state.display_name" as="input" label="Manager Name" prop="display_name" placeholder="Jose Mourinho" />
        <UiFormItem v-model="state.email" as="input" label="email" prop="email" placeholder="you@example.com" />
        <UiFormItem v-model="state.password" as="input" label="password" prop="password"
          placeholder="At least 5 characters" />        
      </div>

      <UiButton size="touch" type="submit" variant="primary" :loading>Continue</UiButton>

    </UiForm>
    <div class="mt-5 text-t3 text-center">
      Already managing?
      <nuxt-link to="/login" class="text-info">Log in</nuxt-link>
    </div>
    <p class="mt-10 text-meta text-center text-t3/70">
      By creating an account you agree to the Terms and the fair-play policy. One manager per person.
    </p>
  </div>
</template>