<script setup lang="ts">
import { Field } from '@ark-ui/vue/field'

defineProps<{
  label: string
  /** Positive status line under the input, e.g. "Available". */
  helper?: string
  /** Error message; marks the field invalid. */
  error?: string
  placeholder?: string
  type?: 'text' | 'email' | 'password' | 'search' | 'number'
  autocomplete?: string
  mono?: boolean
  required?: boolean
  disabled?: boolean
}>()
const model = defineModel<string>({ default: '' })
</script>

<template>
  <Field.Root :invalid="!!error" :required="required" :disabled="disabled" class="font-geist flex flex-col gap-1.5">
    <Field.Label class="text-[12.5px] font-medium text-t1">{{ label }}</Field.Label>
    <Field.Input
      v-model="model"
      :type="type ?? 'text'"
      :placeholder="placeholder"
      :autocomplete="autocomplete"
      :class="cn(
        'min-h-11 rounded-nested border border-line2 bg-s1 px-3 text-t1 placeholder:text-t3 outline-none transition-colors',
        'focus:border-t2 data-[invalid]:border-neg disabled:opacity-40',
        mono ? 'num text-[14px]' : 'text-body',
      )"
    />
    <Field.HelperText v-if="helper && !error" class="text-[11.5px] text-pos">{{ helper }}</Field.HelperText>
    <Field.ErrorText class="text-[11.5px] text-neg">{{ error }}</Field.ErrorText>
  </Field.Root>
</template>
