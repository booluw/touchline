import { createToaster } from '@ark-ui/vue/toast'

/** Toast tone maps to the status dot: success → pos, error → urgent, info → info, warning → important. */
export type ToastTone = 'success' | 'error' | 'info' | 'warning'

export interface ToastInput {
  title: string
  tone?: ToastTone
  /** Optional link-style action, e.g. "View" or "Why?". */
  action?: { label: string, onClick: () => void }
  /** ms; design default is 6s. */
  duration?: number
}

const toaster = createToaster({ placement: 'bottom-end', gap: 8, duration: 6000 })

/** App-wide toast API backed by ark-ui. Render <UiToaster /> once in app.vue. */
export function useToaster() {
  return {
    toaster,
    notify: ({ title, tone = 'info', action, duration }: ToastInput) =>
      toaster.create({ title, type: tone, duration, action: action && { label: action.label, onClick: action.onClick } }),
    dismiss: (id?: string) => toaster.dismiss(id),
  }
}
