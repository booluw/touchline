import type { Board, Club, Competition } from "~/types"

// Pinia store: club.ts — current club identity, DNA, board, facilities
export const useClubStore = defineStore('club', () => {
  const club = ref<Club>()
  const competitions = ref<Competition[]>()
  const fixtures = ref<Fixture[]>()
  const board = ref<Board>()

  const setClub = (payload: Club) => club.value = payload
  const setCompetitions = (payload: Competition[]) => competitions.value = payload
  const setFixtures = (payload: Fixture[]) => fixtures.value = payload
  const setBoard = (payload: Board) => board.value = payload

  return {
    club,
    competitions,
    fixtures,
    board,
    setClub,
    setCompetitions,
    setFixtures,
    setBoard
  }
}, {
  persist: true
})