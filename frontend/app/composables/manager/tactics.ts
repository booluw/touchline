import type { ManagerSquadPlayer } from "~/types/manager"
import type { ClubLineup, ClubTactics, TacticalAdvice, TacticStyle, TeamInstructions } from "~/types/manager"

export function useManagerTactics() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const toast = useToaster()

  async function getTactics(clubId: string) {
    try {
      return await $api.get<ClubTactics>(`${apiBase}/api/clubs/${clubId}/tactics`)
    } catch (error) {
      toast.apiError(error)
    }
  }

  /** Null advice = no upcoming fixture. */
  async function getAdvice(clubId: string) {
    try {
      const r = await $api.get<{ advice: TacticalAdvice | null }>(`${apiBase}/api/clubs/${clubId}/tactics/advice`)
      return r.advice
    } catch (error) {
      toast.apiError(error)
    }
  }

  async function getLineup(clubId: string) {
    try {
      return await $api.get<ClubLineup>(`${apiBase}/api/clubs/${clubId}/lineup`)
    } catch (error) {
      toast.apiError(error)
    }
  }

  async function getSquad(clubId: string) {
    try {
      const r = await $api.get<{ players: ManagerSquadPlayer[] }>(`${apiBase}/api/clubs/${clubId}/players`)
      return r.players
    } catch (error) {
      toast.apiError(error)
    }
  }

  /** Saves tactics first (formation decides slot roles), then the XI. */
  async function savePlan(clubId: string, plan: { style: TacticStyle, formation: string, instructions: TeamInstructions, xi: string[] }) {
    try {
      await $api.post(`${apiBase}/api/clubs/${clubId}/tactics`, { style: plan.style, formation: plan.formation, instructions: plan.instructions })
      await $api.put(`${apiBase}/api/clubs/${clubId}/lineup`, { slots: plan.xi.map((player_id, slot) => ({ slot, player_id })) })
      toast.success("Tactics and lineup saved")
      return true
    } catch (error) {
      toast.apiError(error, { fallback: "Could not save your plan." })
      return false
    }
  }

  async function saveTactics(clubId: string, style: TacticStyle, formation: string, instructions: TeamInstructions) {
    try {
      await $api.post(`${apiBase}/api/clubs/${clubId}/tactics`, { style, formation, instructions })
      toast.success("Tactics saved")
      return true
    } catch (error) {
      toast.apiError(error, { fallback: "Could not save tactics." })
      return false
    }
  }

  return {
    getTactics,
    getAdvice,
    getLineup,
    getSquad,
    savePlan,
    saveTactics,
  }
}
