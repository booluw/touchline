<template>
  <div>
    <div class="flex items-center gap-5">
      <button class="button button--primary">Confirm</button>
      <button class="button button--outline">Save draft</button>
      <button class="button button--danger">Terminate contract</button>
      <button class="button button--soft">Okay</button>
      <button class="button">Okay</button>
    </div>
  </div>
  <NuxtPage />
</template>

<script setup lang="ts">
const store = useAuthStore()
const user = computed(() => store.user)

onMounted(() => {
  if (user.value) {
    if (user.value.is_admin) {
      navigateTo("/admin")
      return
    }
    navigateTo("/play")
  }
})
</script>