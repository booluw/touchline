<script lang="ts" setup>
import { Field } from '@ark-ui/vue';

defineProps<{ label: string, prop: string, as: 'input' | 'textarea' }>()
const error = inject("f_errors") as Record<string, string>
</script>

<template>
  <Field.Root :class="cn('font-geist flex flex-col gap-1 w-full')" :invalid="!!error[prop]">
    <div class="flex justify-between">
      <Field.Label class="capitalize text-body">{{ label }}</Field.Label>
      <slot />
    </div>
    <component
      :is="as === 'input' ? Field.Input : Field.Textarea"
      :class="cn(
        `bg-s1 rounded-nested h-11 border border-line text-t1 ring-0 outline-0 p-3.5 focus-within:border-t1 ease-in-out duration-300`,
        !!error[prop] && 'border-urgent',
        $attrs.class)"
      v-bind="$attrs"
    />
    <!-- <Field.HelperText></Field.HelperText> -->
    <Field.ErrorText class="text-urgent text-meta">{{ error[prop] }}</Field.ErrorText>
  </Field.Root>
</template>