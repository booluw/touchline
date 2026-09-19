<script lang="ts" setup>
import z from 'zod';

const router = useRouter()
const { createWorld } = useAdmin()

const schema = z.object({ name: z.string().min(4, "World name should be 4 or more characters") })
const state = ref({ name: '' })
const loading = ref(false)

async function createNewWorld(valid: boolean) {
  if (valid) {
    loading.value = true

    try {
      await createWorld(state.value)
      router.replace({ name: 'admin-world' })
    } finally {
      loading.value = false
    }
  }
}
</script>

<template>
  <UiModal title="Create World" description="Shoot for the galaxies!!!" @close="() => router.push({ name: 'admin-world' })">
    <UiForm @submit="createNewWorld" :state :schema>
      <UiFormItem label="World Name" prop="name">
        <UiInput v-model="state.name" placeholder="The next galaxy." />
      </UiFormItem>

      <button class="button button--primary">Create New World</button>
    </UiForm>
  </UiModal>
</template>