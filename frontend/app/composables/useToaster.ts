import { createToaster } from '@ark-ui/vue/toast'
import { isApiError } from '~/types'

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

type ToneInput = Omit<ToastInput, 'tone' | 'title'>

const toaster = createToaster({ placement: 'bottom-end', gap: 8, duration: 6000 })

const create = ({ title, tone = 'info', action, duration }: ToastInput) =>
  toaster.create({ title, type: tone, duration, action })

/** Display text for anything thrown by useApi() (or elsewhere). ApiError.message is already user-safe. */
export const errorMessage = (e: unknown, fallback = 'Something went wrong.') =>
  isApiError(e) || e instanceof Error ? e.message || fallback : fallback

/** App-wide toast API backed by ark-ui. Render <UiToaster /> once in app.vue. */
export function useToaster() {
  return {
    toaster,
    notify: create,
    success: (title: string, opts?: ToneInput) => create({ ...opts, title, tone: 'success' }),
    info: (title: string, opts?: ToneInput) => create({ ...opts, title, tone: 'info' }),
    warning: (title: string, opts?: ToneInput) => create({ ...opts, title, tone: 'warning' }),
    error: (title: string, opts?: ToneInput) => create({ ...opts, title, tone: 'error' }),
    /**
     * Toast a caught error. 401s are skipped: the api plugin already resets
     * auth and redirects, so a toast on top would be noise.
     */
    apiError: (e: unknown, opts?: ToneInput & { fallback?: string }) => {
      if (isApiError(e) && e.statusCode === 401) return
      return create({ ...opts, title: errorMessage(e, opts?.fallback), tone: 'error' })
    },
    /** Loading toast that settles to success / error with the promise. */
    promise: <T>(p: Promise<T> | (() => Promise<T>), titles: { loading: string, success: string | ((v: T) => string), error?: string }) =>
      toaster.promise(p, {
        loading: { title: titles.loading },
        success: v => ({ title: typeof titles.success === 'function' ? titles.success(v) : titles.success }),
        error: e => ({ title: titles.error ?? errorMessage(e) }),
      }),
    update: (id: string, input: Partial<ToastInput>) =>
      toaster.update(id, { title: input.title, type: input.tone, duration: input.duration, action: input.action }),
    dismiss: (id?: string) => toaster.dismiss(id),
  }
}
