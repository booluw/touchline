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
</script>

<template>
  <OldUiModal size="w-full md:w-1/2 md:h-[400px]" @close="router.push('/')" hide-title>
    <div class="h-full grid gap-2 md:grid-cols-2 items-center">
      <div class="flex flex-col gap-10">
        <img src="~/assets/svgs/logomark.svg" class="w-2/5" alt="Touchline logomark" />
        <h1 class="page__header">
          Start Your Journey to <span class="text-cyan-500">Managerial Glory</span>
        </h1>
      </div>
      <div class="">
        <OldUiForm @submit="registerUser" :state :schema>
          <OldUiFormItem label="Name" prop="display_name">
            <OldUiInput v-model="state.display_name" placeholder="Jose Mourinho" />
          </OldUiFormItem>
          <OldUiFormItem label="Email" prop="email">
            <OldUiInput v-model="state.email" placeholder="jose.mourinho@example.com" />
          </OldUiFormItem>
          <OldUiFormItem label="Password" prop="password">
            <OldUiInput v-model="state.password" type="password" placeholder="jose-mourinho-4321" />
          </OldUiFormItem>
          <OldUiButton width="full" :loading>Register</OldUiButton>
        </OldUiForm>

        <div class="mt-5 heading heading--small">
          already had an account?
          <nuxt-link to="/login" class="text-cyan-500 underline">log in</nuxt-link>
        </div>
      </div>
    </div>
  </OldUiModal>
</template>