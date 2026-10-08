<script lang="ts" setup>
import { Field } from '@ark-ui/vue';

const props = defineProps<{ label: string, prop: string }>()
const error = inject("f_errors") as Record<string, string>
</script>
<template>
  <Field.Root
    :class="cn('font-geist flex flex-col gap-1')"
    :invalid="!!error[prop]"
  >
    <Field.Label>{{ label }}</Field.Label>
    <Field.Input class="" />
    <Field.HelperText></Field.HelperText>
    <Field.ErrorText>{{ error[prop] }}</Field.ErrorText>
  </Field.Root>
</template>