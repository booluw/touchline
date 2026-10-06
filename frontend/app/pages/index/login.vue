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
</script>

<template>
  <OldUiModal size="w-full md:w-1/3 h-auto" @close="router.push('/')" hide-title>
    <div class="grid gap-2 md:grid-cols-2 items-center">
      <div class="flex flex-col gap-10">
        <img src="~/assets/svgs/logomark.svg" class="w-2/5" alt="Touchline logomark" />
        <h1 class="page__header">
          Continue Your Journey to <span class="text-cyan-500">Managerial Glory</span>
        </h1>
      </div>
      <div class="">
        <OldUiForm @submit="logUserIn" :state :schema>
          <OldUiFormItem label="Email" prop="email">
            <OldUiInput v-model="state.email" placeholder="jose.mourinho@example.com" />
          </OldUiFormItem>
          <OldUiFormItem label="Password" prop="password">
            <OldUiInput v-model="state.password" type="password" placeholder="jose-mourinho-4321" />
          </OldUiFormItem>
          <OldUiButton width="full" :loading>Log In</OldUiButton>
        </OldUiForm>

        <div class="mt-5 heading heading--small">
          Don't have an account?
          <nuxt-link to="/register" class="text-cyan-500 underline">Register Now</nuxt-link>
        </div>
      </div>
    </div>
  </OldUiModal>
</template>