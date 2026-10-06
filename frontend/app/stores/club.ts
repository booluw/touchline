import type { Board, Club, ManagerCompetition } from "~/types"
import type { ManagerNextFixture } from "~/types/manager"

// Pinia store: club.ts — current club identity, DNA, board, facilities
export const useClubStore = defineStore('club', () => {
  const club = ref<Club>()
  const competitions = ref<ManagerCompetition[]>([])
  const fixtures = ref<Fixture[]>([])
  const next_fixture = ref<ManagerNextFixture>()
  const board = ref<Board>()

  const setClub = (payload: Club) => club.value = payload
  const setCompetitions = (payload: ManagerCompetition[]) => competitions.value = payload
  const setFixtures = (payload: Fixture[]) => fixtures.value = payload
  const setNextFixture = (payload: ManagerNextFixture) => next_fixture.value = payload
  const setBoard = (payload: Board) => board.value = payload

  return {
    club,
    competitions,
    fixtures,
    next_fixture,
    board,
    setClub,
    setCompetitions,
    setFixtures,
    setNextFixture,
    setBoard
  }
}, {
  persist: true
})