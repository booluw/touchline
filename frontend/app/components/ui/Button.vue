<script setup lang="ts">
import type { ButtonSize, ButtonVariant } from '~/types/ui/design'

const props = withDefaults(defineProps<{
  variant?: ButtonVariant
  size?: ButtonSize
  type?: 'button' | 'submit' | 'reset'
  disabled?: boolean
  loading?: boolean
  block?: boolean
}>(), { variant: 'secondary', size: 'md', type: 'button' })

const variantClass: Record<ButtonVariant, string> = {
  primary: 'bg-btn text-btn-t! hover:opacity-90',
  secondary: 'border border-line2 text-t1 hover:bg-s2',
  ghost: 'text-t2 hover:bg-s2 hover:text-t1',
  destructive: 'bg-urgent text-white hover:opacity-90',
}
const sizeClass: Record<ButtonSize, string> = {
  sm: 'px-[9px] py-[5px] text-meta rounded-control',
  md: 'px-3 py-[7px] text-meta rounded-control',
  touch: 'min-h-11 px-4 text-body rounded-nested',
}
</script>

<template>
  <button
    :type="props.type"
    :disabled="props.disabled || props.loading"
    :aria-busy="props.loading || undefined"
    :class="cn(
      'cursor-pointer font-geist inline-flex items-center justify-center gap-1.5 font-medium transition-colors duration-150 ease-out',
      'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-t2 disabled:opacity-40 disabled:pointer-events-none',
      variantClass[props.variant], sizeClass[props.size], props.block && 'w-full',
    )"
  >
    <slot />
  </button>
</template>
