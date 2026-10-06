/** Shared prop types for the Touchline design-system components (app/components/ui). */

/** Attention tiers used by the Home inbox and decision cards. */
export type AttentionTier = 'urgent' | 'important' | 'info'

/** Semantic tone. Colour is only for meaning: "should I act?". */
export type SemanticTone = AttentionTier | 'pos' | 'neg' | 'neutral'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'destructive'
export type ButtonSize = 'md' | 'sm' | 'touch'

export interface SegmentOption<V extends string = string> {
  value: V
  label: string
  disabled?: boolean
}

/** One signed contributor in a Why? breakdown, e.g. { label: 'Playing time', value: -18 }. */
export interface WhyFactor {
  label: string
  value: number
}

export type PositionFit = 'natural' | 'capable' | 'awkward' | 'out'

export interface LineupSlot {
  id: string
  /** Horizontal position on the pitch, 0–100 (left → right). */
  x: number
  /** Vertical position on the pitch, 0–100 (top = attack, bottom = goal). */
  y: number
  /** Effective rating of the player in this slot. */
  rating: number
  fit: PositionFit
  label?: string
}

export type SortDirection = 'asc' | 'desc'
export interface TableSort<K extends string = string> {
  key: K
  direction: SortDirection
}
export interface TableColumn<K extends string = string> {
  key: K
  label: string
  align?: 'left' | 'right'
  sortable?: boolean
  /** CSS grid track, e.g. '2.2fr' or '80px'. Defaults to '1fr'. */
  width?: string
  /** Hidden when density is 'simple'. */
  advanced?: boolean
}

export interface OfferTerm {
  label: string
  value: string
  /** Previous value; when set and different, it is shown struck through. */
  previous?: string
}

export const FINANCIAL_HEALTH_STAGES = [
  'Healthy',
  'Warning',
  'Restriction',
  'Emergency',
  'Administration risk',
] as const
export type FinancialHealthStage = (typeof FINANCIAL_HEALTH_STAGES)[number]

/** One app-shell destination. `icon` is a Phosphor icon component (e.g. PhHouse). */
export interface ShellNavItem {
  label: string
  to: string
  icon: import('vue').Component
  /** Unread/pending count shown as a mono badge. */
  count?: number
  countTone?: AttentionTier | 'neutral'
  /** Shown in the mobile bottom bar; the rest live in the More sheet. */
  primary?: boolean
}

export interface ShellClub {
  name: string
  /** 2–3 letter crest text, e.g. "CV". */
  short: string
  league?: string
  /** Crest colour (identity only). Defaults to --color-club. */
  color?: string
}

export interface ShellFixture {
  /** "Next · League · Home" */
  context: string
  opponent: string
  /** Short opponent code for mobile, e.g. "BRE". */
  opponentShort?: string
  /** Pre-formatted mono countdown, e.g. "2d 06h". */
  countdown: string
}

export type DisplayDensity = 'simple' | 'standard' | 'advanced'
