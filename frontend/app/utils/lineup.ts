import type { PositionFit } from '~/types/ui/design'

/**
 * Lineup maths shared by the tactics pitch. Mirrors backend
 * internal/squad/lineup.go (formationOrders, positionFit); the server stays
 * authoritative at save.
 */

export const FORMATION_ORDERS: Record<string, string[]> = {
  '4-3-3': ['GK', 'LB', 'CB', 'CB', 'RB', 'CM', 'CM', 'CM', 'RW', 'ST', 'LW'],
  '4-4-2': ['GK', 'LB', 'CB', 'CB', 'RB', 'RM', 'CM', 'CM', 'LM', 'ST', 'ST'],
  '4-2-3-1': ['GK', 'LB', 'CB', 'CB', 'RB', 'DM', 'DM', 'RM', 'AM', 'LM', 'ST'],
  '5-3-2': ['GK', 'CB', 'CB', 'CB', 'LB', 'RB', 'CM', 'CM', 'CM', 'ST', 'ST'],
  '3-2-4-1': ['GK', 'CB', 'CB', 'CB', 'DM', 'DM', 'RM', 'AM', 'AM', 'LM', 'ST'],
  '5-4-1': ['GK', 'CB', 'CB', 'CB', 'LB', 'RB', 'LM', 'CM', 'CM', 'RM', 'ST'],
  '4-5-1': ['GK', 'LB', 'CB', 'CB', 'RB', 'LM', 'CM', 'CM', 'CM', 'RM', 'ST'],
  '3-5-2': ['GK', 'CB', 'CB', 'CB', 'LB', 'RB', 'CM', 'CM', 'CM', 'ST', 'ST'],
}

const DEF = ['GK', 'CB', 'LB', 'RB']
const MID = ['DM', 'CM', 'AM', 'LM', 'RM']
const FWD = ['ST', 'LW', 'RW']

/** Engine fit multiplier: 1 natural, 0.75 same unit, 0.3 cross-unit, 0.05 keeper mismatch. */
export function positionFit(player: string, slot: string): number {
  if (player === slot) return 1
  for (const unit of [DEF, MID, FWD]) if (unit.includes(player) && unit.includes(slot)) return 0.75
  if (player === 'GK' || slot === 'GK') return 0.05
  return 0.3
}

export function fitLabel(fit: number): PositionFit {
  return fit >= 1 ? 'natural' : fit >= 0.75 ? 'capable' : fit >= 0.3 ? 'awkward' : 'out'
}

/** The number on the token: overall scaled by how well he fits the slot. */
export const effectiveRating = (overall: number, fit: number) => Math.round(overall * fit)

// Pitch rows (y: 0 = attack, 100 = own goal). Wide roles hug the touchline;
// central roles in a row spread evenly around the centre.
const ROW_Y: Record<string, number> = {
  GK: 92, LB: 71, CB: 75, RB: 71, DM: 60, LM: 46, CM: 48, RM: 46, AM: 32, LW: 22, RW: 22, ST: 13,
}

/** Percentage x/y for each slot of a formation order. */
export function slotCoordinates(order: string[]): { x: number, y: number }[] {
  const centralIdx = (pos: string) => order.map((p, i) => [p, i] as const).filter(([p]) => ROW_Y[p] === ROW_Y[pos] && !/^[LR]/.test(p)).map(([, i]) => i)
  return order.map((pos, i) => {
    const y = ROW_Y[pos] ?? 50
    if (pos.startsWith('L')) return { x: 14, y }
    if (pos.startsWith('R')) return { x: 86, y }
    const row = centralIdx(pos)
    const gap = row.length >= 3 ? 20 : 24
    return { x: 50 + (row.indexOf(i) - (row.length - 1) / 2) * gap, y }
  })
}

export interface PickablePlayer {
  id: string
  position: string
  overall: number
  available: boolean
}

/**
 * Greedy best XI: repeatedly fill the open slot/player pair with the highest
 * effective rating (ties prefer the better fit). Unavailable players are
 * never picked; slots stay null when the squad runs out.
 */
export function bestEleven(order: string[], players: PickablePlayer[]): (string | null)[] {
  const out: (string | null)[] = order.map(() => null)
  const used = new Set<string>()
  for (let n = 0; n < order.length; n++) {
    let best: { slot: number, id: string, score: number } | null = null
    order.forEach((slot, i) => {
      if (out[i]) return
      for (const p of players) {
        if (!p.available || used.has(p.id)) continue
        const fit = positionFit(p.position, slot)
        const score = effectiveRating(p.overall, fit) * 10 + fit
        if (!best || score > best.score) best = { slot: i, id: p.id, score }
      }
    })
    if (!best) break
    const pick: { slot: number, id: string } = best
    out[pick.slot] = pick.id
    used.add(pick.id)
  }
  return out
}

/** Put `playerId` into `slot`; if he already starts elsewhere, the two swap. */
export function assignToSlot(xi: (string | null)[], slot: number, playerId: string): (string | null)[] {
  const next = [...xi]
  const from = next.indexOf(playerId)
  if (from >= 0) next[from] = next[slot] ?? null
  next[slot] = playerId
  return next
}

export function swapSlots(xi: (string | null)[], a: number, b: number): (string | null)[] {
  const next = [...xi]
  ;[next[a], next[b]] = [next[b] ?? null, next[a] ?? null]
  return next
}
