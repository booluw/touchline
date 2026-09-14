// Auth session flow against the Gin API (S02-01).
//
// The API authenticates with httpOnly cookies (access_token + refresh_token),
// so every request uses credentials: 'include'. A jobless account that spans
// multiple worlds triggers a world-picker round-trip: login returns
// { status: 'worlds', worlds: [...] } without cookies, the user picks a world,
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

export type LoginResponse = LoginSuccess | LoginWorldPicker

export function useAuth() {
  const { public: { apiBase } } = useRuntimeConfig()

  const user = ref<string | null>(null)
  const worlds = ref<WorldOption[]>([])
  const needsWorldSelection = ref(false)
  const pendingWorldId = ref<string | null>(null)

  async function login(email: string, password: string): Promise<LoginResponse> {
    worlds.value = []
    needsWorldSelection.value = false

    const res = await fetch(`${apiBase}/api/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({
        email,
        password,
        world_id: pendingWorldId.value ?? undefined,
      }),
    })
    const body: LoginResponse = await res.json().catch(() => ({}))

    if (res.ok && (body as LoginWorldPicker).status === 'worlds') {
      worlds.value = (body as LoginWorldPicker).worlds ?? []
      needsWorldSelection.value = true
      return body as LoginWorldPicker
    }
    if (res.ok) {
      user.value = (body as LoginSuccess).display_name ?? null
      pendingWorldId.value = null

      console.log(body)
      return body as LoginSuccess
    }
    throw new Error((body as { error?: string }).error ?? 'Sign-in failed.')
  }

  // Registers a world choice; the next login() call re-posts with world_id.
  function selectWorld(id: string) {
    pendingWorldId.value = id
  }

  // Rotates the session. Returns false when the refresh cookie is gone/revoked.
  async function refresh(): Promise<boolean> {
    const res = await fetch(`${apiBase}/api/auth/refresh`, {
      method: 'POST',
      credentials: 'include',
    })
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

  return { user, worlds, needsWorldSelection, login, selectWorld, refresh, authedFetch }
}