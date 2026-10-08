import type { Club } from "./_admin"
import type { Offer } from "./_manager"
import type { User } from "./user"

export interface WorldOption {
  id: string
  name: string
  status: string
}

export interface LoginWorldPicker {
  status: 'worlds'
  worlds: WorldOption[]
}

export interface LoginResponse extends User {
  club: Club
}
export interface UserOffer extends User {
  offer: Offer
}