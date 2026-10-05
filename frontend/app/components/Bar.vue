<script lang="ts" setup>
const props = defineProps<{
  value: number
  label: string
  inverse?: boolean
}>()

const classes = computed(() => {
  if (props.inverse) {
    return [
      { 'bg-win-500': props.value <= 80, },
      { 'bg-warn-500': (props.value >= 79 || props.value <= 69) },
      { 'bg-orange-300': (props.value >= 68 || props.value == 59) },
      { 'bg-orange-500': (props.value >= 59 || props.value == 49) },
      { 'bg-loss-500': props.value > 49 }
    ].reverse()
  }
  return [
    { 'bg-win-500': props.value >= 80, },
    { 'bg-warn-500': (props.value <= 79 || props.value >= 69) },
    { 'bg-orange-300': (props.value <= 68 || props.value == 59) },
    { 'bg-orange-500': (props.value <= 59 || props.value == 49) },
    { 'bg-loss-500': props.value < 49 }
  ]
})
</script>
<template>
  <section class="">
    <div class="flex justify-between">
      <span class="heading heading--small">{{ label.replaceAll("_", " ") }}</span>
      <span class="text-xs">{{ value }}</span>
    </div>
    <div class="p-1 border-hairline border-void-700">
      <div class="h-1"
        :title="`${label}: ${value}%`"
        :style="`width: ${value}%`"
        :class="classes"
      />
    </div>
  </section>
</template>