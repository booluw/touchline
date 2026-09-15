import type { User } from "../types"

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User>()

  const setUser = (payload: User) => user.value = payload
  const $reset = () => user.value = undefined

  return {
    user,
    setUser,
    $reset
  }
})