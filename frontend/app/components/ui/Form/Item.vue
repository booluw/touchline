<script lang="ts" setup>
import { Field } from '@ark-ui/vue';

defineProps<{ label: string, prop: string, as: 'input' | 'textarea' }>()
const error = inject("f_errors") as Record<string, string>
</script>

<template>
  <Field.Root :class="cn('font-geist flex flex-col gap-1.5')" :invalid="!!error[prop]">
    <div class="flex justify-between text-[12.5px]">
      <Field.Label class="capitalize font-medium text-t1">{{ label }}</Field.Label>
      <slot />
    </div>
    <component
      :is="as === 'input' ? Field.Input : Field.Textarea"
      :class="cn(
        `min-h-11 rounded-nested border border-line2 bg-s1 px-3 text-t1 placeholder:text-t3 outline-none transition-colors`,
        'focus:border-t2 data-invalid:border-neg disabled:opacity-40',
        !!error[prop] && 'border-urgent',
        $attrs.class)"
      v-bind="$attrs"
    />
    <!-- <Field.HelperText></Field.HelperText> -->
    <Field.ErrorText class="text-[11.5px] text-neg">{{ error[prop] }}</Field.ErrorText>
  </Field.Root>
</template>