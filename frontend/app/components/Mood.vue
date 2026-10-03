<script lang="ts" setup>
const props = defineProps<{
  mood: number | string,
  attr: {
    [key: number | string]: 'blanked' | 'dot' | 'happy' | 'neutral' | 'sad',
  }
}>()
</script>

<template>

</template>