import type { Toast } from '~/types/ui/toast'

const toasts = ref<Toast[]>([])
const show = ref(false)

export function useToast() {
  const notify = (notification: Toast) => {
    show.value = true
    const id = crypto.randomUUID()

    toasts.value.push({
      ...notification,
      id,
    })

    console.log("Hello Booluw")
    return id
  }

  const remove = (id: string) => {
    toasts.value = toasts.value.filter((toast: Toast) => toast.id !== id)
    show.value = false
  }

  const clear = () => {
    toasts.value = []
    show.value = false
  }

  return {
    toasts: readonly(toasts),
    notify,
    remove,
    clear,
    show,
  }
}