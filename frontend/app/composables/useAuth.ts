// Auth session flow against the Gin API (S02-01).
//
// The API authenticates with httpOnly cookies (access_token + refresh_token),
// so every request uses credentials: 'include'. A jobless account that spans
// multiple worlds triggers a world-picker round-trip: login returns
// { status: 'worlds', worlds: [...] } without cookies, the user picks a world,

import type { User, Offer, Club } from "~/types"
import { useToast } from '../components/old-ui/Toast';

// and login is re-posted with world_id.
export interface WorldOption {
  id: string
  name: string
  status: string
}

export interface LoginSuccess {
  display_name: string
}

export interface LoginWorldPicker {
  status: 'worlds'
  worlds: WorldOption[]
}

export interface LoginResponse extends User {
  club: Club
}
export interface UserOffer extends User {
  offer: Offer
}

export function useAuth() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const { notify } = useToast()

  const store = useAuthStore()
  const clubStore = useClubStore()

  const user = ref<string | null>(null)
  const worlds = ref<WorldOption[]>([])
  const needsWorldSelection = ref(false)
  const pendingWorldId = ref<string | null>(null)
  // let pendingCredentials: { email: string, password: string } | null = null

  async function login(payload: { email: string, password: string }) {
    worlds.value = []
    needsWorldSelection.value = false

    try {
      const res = await $api.post<LoginResponse>(
        `${apiBase}/api/auth/login`,
        { ...payload, world_id: pendingWorldId.value ?? undefined },
        { auth: false },
      )
      // if ('status' in res && res.status === 'worlds') {
      //   pendingCredentials = payload
      //   worlds.value = res.worlds
      //   needsWorldSelection.value = true
      //   return
      // }
      // pendingWorldId.value = null
      // pendingCredentials = null
      const { club, ...resp } = res as LoginResponse

      store.setUser(resp)
      if (club === null) {
        console.log("Came Here")
        navigateTo("/play/offers")
        return
      }
      clubStore.setClub(club)

      notify({
        title: 'Welcome back',
        description: 'Your team awaits',
        type: 'success'
      })

      if (resp.is_admin) {
        navigateTo("/admin")
        return
      } else {
        navigateTo("/play")
      }
    } catch (error) {
      const { data } = error as { data: Record<string, string>}
      notify({
        title: "Error",
        description: data.error,
        type: "danger"
      })
    }
  }

  async function register(payload: { email: string, password: string, display_name: string }) {
    try {
      const { offer, ...rest } = await $api.post<UserOffer>(`${apiBase}/api/auth/register`, payload)

      notify({
        title: 'Account created',
        description: offer ? 'You have a job offer' : 'Welcome onboard',
        type: 'success'
      })
      
      store.setUser(rest)
      navigateTo("/play/offers")
    } catch (error) {
      console.error(error)
      
    }
  }

  // Registers a world choice; the next login() call re-posts with world_id.
  function selectWorld(id: string) {
    pendingWorldId.value = id
  }

  // Re-posts the credentials from the world-picker round-trip with the chosen world.
  // async function confirmWorld() {
  //   if (pendingCredentials && pendingWorldId.value) await login(pendingCredentials)
  // }

  // Rotates the session. Returns false when the refresh cookie is gone/revoked.
  async function refresh(): Promise<boolean> {
    const res = await fetch(`${apiBase}/api/auth/refresh`, {
      method: 'POST',
      credentials: 'include',
    })

    console.log(await res.json())

    return res.status === 200
  }

  // fetch wrapper that transparently refreshes a stale session once.
  async function authedFetch(path: string, init?: RequestInit): Promise<Response> {
    const attempt = () => fetch(`${apiBase}${path}`, { ...init, credentials: 'include' })

    let res = await attempt()
    if (res.status === 401 && (await refresh())) {
      res = await attempt()
    }
    return res
  }

  return { user, worlds, needsWorldSelection, login, selectWorld, refresh, authedFetch, register }
}