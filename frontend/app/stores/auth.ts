import type { ManagerOffer } from "~/types/manager"
import type { User, Club } from "../types"

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User>()
  const offer = ref<ManagerOffer>()
  const club = ref<Club>()

  const setUser = (payload: User) => user.value = payload
  const setOffer = (payload: ManagerOffer) => offer.value = payload
  const setClub = (payload: Club) => club.value = payload
  const $reset = () => {
    user.value = undefined
    offer.value = undefined
    club.value = undefined
  }

  return {
    user,
    offer,
    club,
    setUser,
    setOffer,
    setClub,
    $reset
  }
}, {
  persist: true
})