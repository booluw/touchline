<template>
  <input :class="inputClass" class="border-brutal" v-bind="$attrs" v-model="model" />
</template>

<script setup lang="ts">
import { cn } from '~/utils/cn';

const props = withDefaults(defineProps<{ class?: string }>(), { class: "" });
const errors = inject("f_errors") as Ref<Record<string, string>>
const f_prop = inject("f_prop")

const hasErrors = computed(() => errors ? Boolean(errors.value[f_prop as keyof typeof errors.value]) : false)

const inputClass = computed(() =>
  cn(
    "flex w-full mt-1 p-2 text-white font-mono border-brutal border-void-700 bg-void-800 placeholder:text-void-600 ring-transparent focus-visible:ring focus-visible:border-cyan-500 ease-brutal duration-80",
    props.class,
    hasErrors.value ? '!border-loss-500 focus-visible:ring-loss-500 !ring-transparent placeholder:!text-loss-500/50' : ''
  )
);

const model = defineModel()
</script>