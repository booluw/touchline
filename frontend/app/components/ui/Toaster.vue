<script setup lang="ts">
import { Toast, Toaster } from '@ark-ui/vue/toast'

const { toaster } = useToaster()
const dot: Record<string, string> = { success: 'bg-pos', error: 'bg-urgent', warning: 'bg-important', info: 'bg-info', loading: 'bg-info animate-pulse' }
</script>

<template>
  <Teleport to="body">
    <Toaster v-slot="toast" :toaster="toaster">
      <Toast.Root class="font-geist flex w-80 items-center gap-2.5 rounded-nested border border-line2 bg-s3 px-3 py-2.5 text-meta text-t1 shadow-float h-(--height) opacity-(--opacity) scale-(--scale) [translate:var(--x)_var(--y)] z-(--z-index) transition-all duration-200 ease-out">
        <span :class="['size-1.5 shrink-0 rounded-full', dot[toast.type ?? 'info'] ?? 'bg-info']" aria-hidden="true" />
        <Toast.Title class="flex-1">{{ toast.title }}</Toast.Title>
        <Toast.ActionTrigger v-if="toast.action" class="text-info hover:underline">{{ toast.action.label }}</Toast.ActionTrigger>
      </Toast.Root>
    </Toaster>
  </Teleport>
</template>
