<script lang="ts" setup>
definePageMeta({ layout: "minimal" })
</script>

<template>
  <section class="flex flex-col gap-5 mt-20">
    <div class="">
      <h2 class="text-page">Choose your club</h2>
      <p class="text-t3 w-1/3">You start in the lower divisions. You can always resign and apply for new roles.</p>
    </div>

    <div class="grid grid-cols-6 gap-10">
      <div class="col-span-4 grid grid-cols-2 gap-5">
        <UiCard>
          <div class="">
            <UiLoader class="w-12 h-12" />
          </div>
        </UiCard>
        <UiCard>
          <div class="">
            <UiLoader class="w-12 h-12" />
          </div>
        </UiCard>
      </div>
      <UiCard class="col-span-2"></UiCard>
    </div>

  </section>
</template>