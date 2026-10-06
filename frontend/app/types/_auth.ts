export interface WorldOption {
  id: string
  name: string
  status: string
}

export interface LoginWorldPicker {
  status: 'worlds'
  worlds: WorldOption[]
}