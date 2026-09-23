<script lang="ts" setup>
import z from 'zod';


const router = useRouter()
const { register } = useAuth()

const schema = z.object({ email: z.email(), password: z.string().min(5, "Should be greater than 5 characters"), display_name: z.string() })
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
  <UiModal size="w-1/2 h-[400px]" @close="router.go(-1)" hide-title>
    <div class="h-full grid gap-2 md:grid-cols-2 items-center">
      <div class=""></div>
      <div class="">
        <UiForm @submit="registerUser" :state :schema>
          <UiFormItem label="Name" prop="display_name">
            <UiInput v-model="state.display_name" placeholder="Jose Mourinho" />
          </UiFormItem>
          <UiFormItem label="Email" prop="email">
            <UiInput v-model="state.email" placeholder="john.doe@example.com" />
          </UiFormItem>
          <UiFormItem label="Password" prop="password">
            <UiInput v-model="state.password" type="password" placeholder="jose-mourinho-4321" />
          </UiFormItem>
          <button class="button button--primary w-full">Register</button>
        </UiForm>
      </div>
    </div>
  </UiModal>
</template>