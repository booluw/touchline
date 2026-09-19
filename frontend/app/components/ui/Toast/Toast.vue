<script setup lang="ts">
import { ToastAction, ToastDescription, ToastProvider, ToastRoot, ToastTitle, ToastViewport } from 'reka-ui'
import { useToast } from '.';

const { toasts, remove } = useToast()

const _class = {
  success: "bg-win-500 text-white border-win-500",
  danger: "bg-loss-500 text-white border-loss-500",
  warning: "bg-warn-500 text-white border-warn-500"
}
</script>

<template>
  <template v-for="({ type, title, description, btnText, btnClick, id: key }) in toasts" :key>
    <ToastProvider>
      <ToastRoot
        :open="true"
        @update:open="(open: boolean) => !open && remove(key)"
        class="border-brutal p-3 font-mono" :class="_class[type]"
      >
        <ToastTitle class="mb-1.25 text-sm font-bold">
          {{ title }}
        </ToastTitle>
        <ToastDescription as-child>
          <p class=" m-0 text-xs leading-[1.3]">
            {{ description }}
        </p>
        </ToastDescription>
        <ToastAction v-if="btnClick" class="" as-child :alt-text="btnText">
          <button
            @click="() => btnClick"
            class="button button--outline"
          >
            {{ btnText ?? "Click" }}
          </button>
        </ToastAction>
      </ToastRoot>
      <ToastViewport class="fixed top-4 left-1/2 -translate-x-1/2 w-1/5" />
    </ToastProvider>
  </template>
</template>