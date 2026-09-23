<script setup lang="ts">
import { DrawerContent, DrawerDescription, DrawerOverlay, DrawerPortal, DrawerRoot, DrawerTitle, DrawerTrigger } from 'reka-ui';

const emit = defineEmits(["close"])
const props = withDefaults(defineProps<{
  title?: string
  description?: string
  hideTitle?: boolean
  padded?: boolean
  size?: string
}>(), {
  hideTitle: false,
  padded: true,
  size: 'w-2/6 h-[300px]'
})
</script>

<template>
  <DrawerRoot :defaultOpen="true" @update:open="(e: boolean) => emit('close', e)">
    <DrawerPortal>
      <DrawerOverlay class="fixed top-0 inset-0 z-30 bg-black/95 flex items-center justify-center">
        <DrawerContent
          class="bg-void-950 border-brutal border-void-800"
          :class="[size, padded ? 'p-5' : undefined]"
        >
          <div v-if="!props.hideTitle" class="flex justify-between pb-3 mb-5 border-b border-void-600">
            <div class="font-mono">
              <DrawerTitle class="font-bold">{{ title }}</DrawerTitle>
              <DrawerDescription class="text-xs">{{ description }}</DrawerDescription>
            </div>  
            <DrawerClose class="text-xs p-3 text-loss-500 font-mono cursor-pointer">
              Close
            </DrawerClose>
          </div>
          <slot />
        </DrawerContent>
      </DrawerOverlay>
    </DrawerPortal>
  </DrawerRoot>
</template>