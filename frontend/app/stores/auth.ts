import type { User, Offer, Club } from "../types"

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User>()
  const offer = ref<Offer>()
  const club = ref<Club>()

  const setUser = (payload: User) => user.value = payload
  const setOffer = (payload: Offer) => offer.value = payload
  const setClub = (payload: Club) => club.value = payload
  const $reset = () => {
    user.value = undefined
    offer.value = undefined
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