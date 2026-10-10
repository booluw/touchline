import type { ManagerSquadPlayer, PlayerMoraleDetail, PricePreset, RequestPreview, SquadDynamics } from "~/types/manager"

export type RequestAction = 'approve' | 'reassure' | 'deny'

export function useManagerSquad() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const toast = useToaster()

  async function getSquad(clubId: string) {
    try {
      const r = await $api.get<{ players: ManagerSquadPlayer[] }>(`${apiBase}/api/clubs/${clubId}/players`)
      return r.players
    } catch (error) {
      toast.apiError(error)
    }
  }

  async function getPlayer(clubId: string, playerId: string) {
    try {
      return await $api.get<PlayerMoraleDetail>(`${apiBase}/api/clubs/${clubId}/players/${playerId}`)
    } catch (error) {
      toast.apiError(error)
    }
  }

  /** Factions are optional context; a failure only hides that line. */
  async function getDynamics(clubId: string) {
    try {
      return await $api.get<SquadDynamics>(`${apiBase}/api/clubs/${clubId}/dynamics`)
    } catch {
      return null
    }
  }

  const paths: Record<RequestAction, string> = {
    approve: 'transfer-request/approve',
    deny: 'transfer-request/deny',
    reassure: 'transfer-request/reassure',
  }
  const done: Record<RequestAction, string> = {
    approve: 'Player transfer-listed',
    deny: 'Transfer request denied',
    reassure: 'Promise made, request paused',
  }

  /** IM65: what each answer to the open request does. */
  async function getPreview(clubId: string, playerId: string) {
    try {
      return await $api.get<RequestPreview>(`${apiBase}/api/clubs/${clubId}/players/${playerId}/transfer-request/preview`)
    } catch (error) {
      toast.apiError(error)
    }
  }

  async function respond(clubId: string, playerId: string, action: RequestAction, preset?: PricePreset) {
    try {
      const body = action === 'approve' && preset ? { price_preset: preset } : {}
      await $api.post(`${apiBase}/api/clubs/${clubId}/players/${playerId}/${paths[action]}`, body)
      toast.success(done[action])
      return true
    } catch (error) {
      toast.apiError(error, { fallback: 'Could not respond to the request.' })
      return false
    }
  }

  return { getSquad, getPlayer, getDynamics, getPreview, respond }
}
