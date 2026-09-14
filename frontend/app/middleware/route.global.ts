import { useAuthStore } from "../stores/auth"

export default defineNuxtRouteMiddleware((to) => {
  const authStore = useAuthStore()

  // Protect `/admin` Routes
  if (to.fullPath.startsWith("/admin")) {
    if (!authStore.user?.is_admin) {
      return navigateTo("/")
    }
  }

  // Protect `/play`
  if (to.fullPath.startsWith("/play")) {
    if (!authStore.user || Object.keys(authStore.user ?? {}).length === 0) {
      return navigateTo("/")
    }
  }

  return
})