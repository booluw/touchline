/** Signed number with a true minus (−) and explicit plus, e.g. −18 / +4 / 0. */
export function formatSigned(value: number): string {
  if (value > 0) return `+${value}`
  if (value < 0) return `−${Math.abs(value)}`
  return '0'
}
