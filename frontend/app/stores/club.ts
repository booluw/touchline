import type { CommonClub } from "~/types"
import type { ManagerBoard, ManagerNextFixture, ManagerCompetition } from "~/types/manager"

// Pinia store: club.ts — current club identity, DNA, board, facilities
export const useClubStore = defineStore('club', () => {
  const club = ref<CommonClub>()
  const competitions = ref<ManagerCompetition[]>([])
  const fixtures = ref<Fixture[]>([])
  const next_fixture = ref<ManagerNextFixture>()
  const board = ref<ManagerBoard>()

  const setClub = (payload: CommonClub) => club.value = payload
  const setCompetitions = (payload: ManagerCompetition[]) => competitions.value = payload
  const setFixtures = (payload: Fixture[]) => fixtures.value = payload
  const setNextFixture = (payload: ManagerNextFixture) => next_fixture.value = payload
  const setBoard = (payload: ManagerBoard) => board.value = payload

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