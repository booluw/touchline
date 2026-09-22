<script lang="ts" setup>
defineProps<{
  src?: string,
  alt?: string
}>()
</script>

<template>
  <div class="bg-void-950 text-void-500 p-3 flex items-center justify-center w-12 h-12">
    <h3 class="font-mono uppercase font-black">{{ alt }}</h3>
  </div>
</template>