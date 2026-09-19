<script lang="ts" setup>
import z from 'zod';

const emit = defineEmits(["submit"])
const props = defineProps<{
  schema: any,
  state: Record<string, any>,
  validate?: 'submit' | 'blur'
}>()

const formError = ref<typeof props.state>({})
provide('f_errors', formError)

async function validateForm(submit: boolean) {
  let valid = false
  formError.value = {}
  try {
    props.schema.parse(props.state)
    valid = true
  } catch (error) {
    valid = false
    if (error instanceof z.ZodError) {
      error.issues.forEach((err) => {
        formError.value[err.path[0] as keyof typeof props.state] = err.message
      })
    }
  } finally {
    if (submit) emit("submit", valid, formError.value)
  }
}

watch(props.state, () => {
  if (props.validate === 'blur') {
    validateForm(false)
  }
}, { deep: true })
</script>

<template>
  <form @submit.prevent="validateForm(true)">
    <slot />
  </form>
</template>