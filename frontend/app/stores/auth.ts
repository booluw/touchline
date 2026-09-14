import type { User } from "~/app/types"

export const authStore = defineStore('auth', () => {
  const user = ref<User>()

  const setUser = (payload: User) => user.value = payload

  return {
    user,
    setUser
  }
})