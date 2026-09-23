import { useAuthStore } from "../stores/auth"

export default defineNuxtRouteMiddleware(async (to) => {
  const authStore = useAuthStore()

  // Protect `/play`
  if (to.fullPath.startsWith("/play")) {
    console.log(authStore.user)
    if (!authStore.user || Object.keys(authStore.user ?? {}).length === 0) {
      return navigateTo("/")
    }
  }

  // Protect `/admin` Routes
  if (to.fullPath.startsWith("/admin")) {
    if (!authStore.user?.is_admin) {
      return navigateTo("/")
    }
  }

  return
})