export interface Toast {
  title: string
  description?: string
  btnText?: string
  btnClick?: () => void
  type?: 'success' | 'danger' | 'warning'
  id?: string
}