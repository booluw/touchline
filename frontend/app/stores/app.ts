import type { DisplayDensity } from "~/types/ui/design"

export const useAppStore = defineStore('app', () => {
  const density = ref<DisplayDensity>("simple")
  const theme = ref<"light"|"dark">("dark")
  
  const setDensity = (payload: DisplayDensity) => density.value = payload
  const $reset = () => {
    density.value = "simple"
  }

  return {
    density,
    theme,
    setDensity,
    $reset
  }
}, {
  persist: true
})