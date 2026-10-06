<script setup lang="ts">
import { Drawer } from '@ark-ui/vue/drawer'

/**
 * Mobile decisions and Why? panels. Scrim rgba(0,0,0,.5), radius 16 top, grab handle, max-height 88%.
 * Decision flow: reasons → options → consequences → confirm. Put footer buttons in #footer (1fr / 2fr).
 */
defineProps<{ title: string, eyebrow?: string }>()
const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <Drawer.Root v-model:open="open">
    <Teleport to="body">
      <Drawer.Backdrop class="fixed inset-0 z-50 bg-black/50" />
      <Drawer.Positioner class="fixed inset-0 z-50 flex items-end justify-center">
        <Drawer.Content class="font-geist flex max-h-[88%] w-full flex-col gap-2 rounded-t-sheet border-t border-line2 bg-s1 px-3.5 pb-3.5 pt-2 text-body text-t1 outline-none transition-transform duration-200 ease-out">
          <Drawer.Grabber class="flex justify-center py-1">
            <Drawer.GrabberIndicator class="h-1 w-9 rounded-sm bg-line2" />
          </Drawer.Grabber>
          <span v-if="eyebrow" class="num text-pill uppercase text-t3">{{ eyebrow }}</span>
          <Drawer.Title class="font-semibold">{{ title }}</Drawer.Title>
          <div class="min-h-0 overflow-y-auto"><slot /></div>
          <div v-if="$slots.footer" class="grid grid-cols-[1fr_2fr] gap-1.5 [&>*]:min-h-12"><slot name="footer" /></div>
        </Drawer.Content>
      </Drawer.Positioner>
    </Teleport>
  </Drawer.Root>
</template>
