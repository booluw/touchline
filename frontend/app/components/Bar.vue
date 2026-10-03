<script lang="ts" setup>
defineProps<{
  value: number
  label: string
}>()
</script>
<template>
  <section class="">
    <span class="heading heading--small">{{ label }}</span>
    <div class="p-1 border-hairline border-void-700">
      <div class="h-1"
        :title="`${label}: ${value}%`"
        :style="`width: ${value}%`"
        :class="[
          { 'bg-win-500': value >= 80, },
          {'bg-warn-500': (value <= 79 || value >= 69) },
          { 'bg-orange-300': (value <= 68 || value == 59 ) },
          { 'bg-orange-500': (value <= 59 || value == 49) },
          {'bg-loss-500' : value < 49}
        ]"
      />
    </div>
  </section>
</template>