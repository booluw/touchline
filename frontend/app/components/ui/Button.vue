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
      'cursor-pointer font-geist inline-grid grid-cols-3 items-center justify-center gap-1.5 font-medium transition-colors duration-150 ease-out',
      'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-t2 disabled:opacity-40 disabled:pointer-events-none',
      variantClass[props.variant], sizeClass[props.size], props.block && 'w-full',
    )"
  >
    <div aria-hidden="true" class=""></div>
    <slot />
    <div class="flex items-center justify-end">
      <svg v-if="loading" class="w-5 fill-current animate-spin" viewBox="0 0 256 256">
        <path
          d="M136,32V64a8,8,0,0,1-16,0V32a8,8,0,0,1,16,0Zm37.25,58.75a8,8,0,0,0,5.66-2.35l22.63-22.62a8,8,0,0,0-11.32-11.32L167.6,77.09a8,8,0,0,0,5.65,13.66ZM224,120H192a8,8,0,0,0,0,16h32a8,8,0,0,0,0-16Zm-45.09,47.6a8,8,0,0,0-11.31,11.31l22.62,22.63a8,8,0,0,0,11.32-11.32ZM128,184a8,8,0,0,0-8,8v32a8,8,0,0,0,16,0V192A8,8,0,0,0,128,184ZM77.09,167.6,54.46,190.22a8,8,0,0,0,11.32,11.32L88.4,178.91A8,8,0,0,0,77.09,167.6ZM72,128a8,8,0,0,0-8-8H32a8,8,0,0,0,0,16H64A8,8,0,0,0,72,128ZM65.78,54.46A8,8,0,0,0,54.46,65.78L77.09,88.4A8,8,0,0,0,88.4,77.09Z">
        </path>
      </svg>
    </div>
  </button>
</template>
