import { useApi } from "~/utils/api"

export default defineNuxtPlugin(() => {
  const api = useApi()
  const router = useRouter()
  const authStore = useAuthStore()

  // Request interceptor - add timestamp
  api.addRequestInterceptor((url, options) => {
    if (import.meta.dev) {
      console.log(`[API] ${options.method || 'GET'} ${url}`)
    }
    return { url, options }
  })

  // Response interceptor - log responses
  api.addResponseInterceptor((response) => {
    if (import.meta.dev) {
      console.log('[API] Response:', response)
    }
    return response
  })

  // Error interceptor - handle auth errors
  api.addErrorInterceptor(async (error) => {
    if (error.statusCode === 401) {
      // Clear auth token
      authStore.$reset()

      // Redirect to login
      if (import.meta.client) {
        await router.push('/')
      }
    }

    throw error
  })

  return {
    provide: {
      api
    }
  }
})