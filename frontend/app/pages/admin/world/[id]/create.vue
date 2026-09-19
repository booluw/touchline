<script lang="ts" setup>
import z from 'zod';

const store = useAdminStore()
const router = useRouter()
const route = useRoute()
const { createCountry } = useAdmin()

const loading = ref(false)
const state = ref({ code: '', name: '' })
const schema = z.object({ code: z.string().max(3), name: z.string().min(3) })

const world_id = computed(() => route.params.id)

async function createACountry(valid: boolean) {
  if (valid) {
    loading.value = true
    try {
      await createCountry({
        ...state.value,
        world_id: world_id.value
      })
      router.push({ name: 'admin-world-id', params: { id: world_id.value } })
    } finally {
      loading.value = false
    }
  }
}
</script>
<template>
  <UiModal @close="router.go(-1)" hide-title>
    <UiForm @submit="createACountry" :state :schema>
      <UiFormItem label="Country Name" prop="name">
        <UiInput v-model="state.name" placeholder="Try not to be fictious" />
      </UiFormItem>
      <UiFormItem label="Country Code" prop="code">
        <UiInput v-model="state.code" placeholder="3 Charcter long" />
      </UiFormItem>
      <button class="button button--primary w-full">Create Country</button>
    </UiForm>
  </UiModal>
</template>