import type { User, Offer } from "../types"

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User>()
  const offer = ref<Offer>()

  const setUser = (payload: User) => user.value = payload
  const setOffer = (payload: Offer) => offer.value = payload
  const $reset = () => {
    user.value = undefined
    offer.value = undefined
  }

  return {
    user,
    offer,
    setUser,
    setOffer,
    $reset
  }
}, {
  persist: true
})