<script lang="ts" setup>
import z from 'zod';

const router = useRouter()
const route = useRoute()
const { createCountry } = useAdmin()

const loading = ref(false)
const state = ref({ code: '', name: '' })
const schema = z.object({ code: z.string().max(3), name: z.string().min(3) })

const world_id = computed(() => String(route.params.id))

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
  <OldUiModal @close="router.go(-1)" hide-title>
    <OldUiForm @submit="createACountry" :state :schema>
      <OldUiFormItem label="Country Name" prop="name">
        <OldUiInput v-model="state.name" placeholder="Try not to be fictious" />
      </OldUiFormItem>
      <OldUiFormItem label="Country Code" prop="code">
        <OldUiInput v-model="state.code" placeholder="3 Charcter long" />
      </OldUiFormItem>
      <button class="button button--primary w-full">Create Country</button>
    </OldUiForm>
  </OldUiModal>
</template>