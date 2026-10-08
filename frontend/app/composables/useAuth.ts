import type { LoginResponse, UserOffer } from "~/types"

export function useAuth() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const toast = useToaster()

  const store = useAuthStore()
  const clubStore = useClubStore()

  async function login(payload: { email: string, password: string }) {

    try {
      const res = await $api.post<LoginResponse>(
        `${apiBase}/api/auth/login`,
        { ...payload },
        { auth: false },
      )
      
      const { club, ...resp } = res as LoginResponse
      store.setUser(resp)
      
      if (club === null) {
        toast.success("Let's find you a club")
        navigateTo("/play/onboard")
        return
      }

      clubStore.setClub(club)
      toast.success("Welcome back")

      if (resp.is_admin) {
        navigateTo("/admin")
        return
      } else {
        navigateTo("/play")
      }
    } catch (error) {
      toast.apiError(error)
    }
  }

  async function register(payload: { email: string, password: string, display_name: string }) {
    try {
      const { offer, ...rest } = await $api.post<UserOffer>(`${apiBase}/api/auth/register`, payload)
      
      toast.success(`Welcome, ${payload.display_name}`)
      store.setUser(rest)
      navigateTo("/play/onboard")
    } catch (error) {
      toast.apiError(error)
    }
  }

  return { login, register }
}