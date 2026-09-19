<script lang="ts" setup>
import z from "zod"

const router = useRouter()
const { login } = useAuth()

const schema = z.object({ email: z.email(), password: z.string().min(5, "Should be greater than 5 characters") })
const state = ref({ email: '', password: '' })
const loading = ref(false)

async function logUserIn(valid: boolean, errors: Record<string, string>) {
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
  <UiModal @close="router.go(-1)" hide-title>
    <div class="grid gap-2 md:grid-cols-2 items-center">
      <div class="flex items-center justify-center">
        <img src="~/assets/svgs/logomark.svg" alt="Touchline logomark" />
      </div>
      <div class="">
        <UiForm @submit="logUserIn" :state :schema>
          <UiFormItem label="Email" prop="email">
            <UiInput v-model="state.email" placeholder="john.doe@example.com" />
          </UiFormItem>
          <UiFormItem label="Password" prop="password">
            <UiInput v-model="state.password" type="password" placeholder="jose-mourinho-4321" />
          </UiFormItem>
          <button class="button button--primary w-full">Log In</button>
        </UiForm>
      </div>
    </div>
  </UiModal>
</template>