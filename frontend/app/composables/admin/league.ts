import { useToast } from "~/components/ui/Toast"

interface MatchDayMovedResponse {
  calendar_updated: boolean,
  matchdays_re_paced: number,
  fixtures_moved: number
}

interface StartLeagueResponse {
  id: string
  competition: {
    id: string
    name: string
  }
  season_label: string
  season_number: 1
  status: string
}

export function useAdminLeague({ leagueId }: { leagueId: string }) {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const { notify } = useToast()

  async function setLeagueStartDate({ kickoff_date, worldId }: { kickoff_date: string, worldId: string }) {
    try {
      const res: StartLeagueResponse = $api.post(`${apiBase}/api/admin/worlds/${worldId}/leagues/${leagueId}/season`, { kickoff_date })

      notify({
        title: "Success",
        description: `Kickoff date for the ${res.season_label} season of ${res.competition.name} has been updated`,
        type: "success"
      })
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "An Error Occurred When Setting League Start Date",
        type: "danger"
      })
      throw error
    }
  }
  async function setLeagueMatchDays() {
    try {
      const { calendar_updated, matchdays_re_paced, fixtures_moved }: MatchDayMovedResponse = await $api.post<MatchDayMovedResponse>(`${apiBase}/api/admin/leagues/${leagueId}/scheduling`)
      if (calendar_updated) {
        notify({
          type: "success",
          description: `Affected fixtures ${fixtures_moved}, league pace is now ${matchdays_re_paced} days.`,
          title: "MatchDay set"
        })

        return
      }

      notify({
        title: "MatchDay set",
        description: "Calendar stays the same",
        type: "success"
      })
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "Error Setting League MatchDay",
        type: "danger"
      })
      throw error
    }
  }

  return {
    setLeagueMatchDays,
    setLeagueStartDate
  }
}