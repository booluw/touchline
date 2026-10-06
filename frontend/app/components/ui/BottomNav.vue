<script setup lang="ts">
import { PhDotsThreeOutline } from '@phosphor-icons/vue'
import type { ShellNavItem } from '~/types/ui/design'

/** Mobile (<768px) 64px bar: primary sections + More. Secondary sections open in a sheet (3-col grid). */
const props = defineProps<{ items: ShellNavItem[] }>()
const moreOpen = ref(false)
const isActive = useShellActive()
const primary = computed(() => props.items.filter(i => i.primary))
const secondary = computed(() => props.items.filter(i => !i.primary))
const moreActive = computed(() => moreOpen.value || secondary.value.some(isActive))
const moreCount = computed(() => secondary.value.reduce((n, i) => n + (i.count ?? 0), 0))
const route = useRoute()
watch(() => route.path, () => { moreOpen.value = false })

const tab = 'flex flex-col items-center justify-center gap-1 text-label hover:no-underline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-t2'
</script>

<template>
  <nav aria-label="Main" class="font-geist grid h-16 grid-cols-5 border-t border-line bg-s1 md:hidden">
    <NuxtLink
      v-for="item in primary" :key="item.to" :to="item.to"
      :aria-current="isActive(item) ? 'page' : undefined"
      :class="[tab, isActive(item) ? 'text-t1' : 'text-t3']"
    >
      <component :is="item.icon" :size="20" :weight="isActive(item) ? 'fill' : 'regular'" aria-hidden="true" />
      {{ item.label }}
    </NuxtLink>
    <button type="button" :aria-expanded="moreOpen" :class="[tab, 'relative', moreActive ? 'text-t1' : 'text-t3']" @click="moreOpen = true">
      <PhDotsThreeOutline :size="20" :weight="moreActive ? 'fill' : 'regular'" aria-hidden="true" />
      More
      <span v-if="moreCount" class="absolute right-[30%] top-2.5 size-1.5 rounded-full bg-important" aria-hidden="true" />
    </button>
  </nav>
  <UiBottomSheet v-model:open="moreOpen" title="More">
    <div class="grid grid-cols-3 gap-2 pt-1">
      <NuxtLink
        v-for="item in secondary" :key="item.to" :to="item.to"
        :aria-current="isActive(item) ? 'page' : undefined"
        :class="['flex min-h-14 flex-col items-center justify-center gap-0.5 rounded-card border border-line text-[12.5px] text-t1 hover:no-underline',
                 isActive(item) ? 'bg-s3' : 'bg-s2']"
      >
        <component :is="item.icon" :size="18" class="text-t2" aria-hidden="true" />
        {{ item.label }}
        <span v-if="item.count" class="num text-[10px]" :class="{ urgent: 'text-urgent', important: 'text-important', info: 'text-info', neutral: 'text-t3' }[item.countTone ?? 'neutral']">{{ item.count }} new</span>
      </NuxtLink>
    </div>
  </UiBottomSheet>
</template>
