/**
 * Formats an integer amount of cents as a USD currency string.
 * Cents, not dollars, because that's how money should be stored/passed
 * around (avoids floating-point rounding errors) — convert at the edge.
 *
 * formatMoney(4825000)   -> "$48,250.00"
 * formatMoney(-150000)   -> "-$1,500.00"
 * formatMoney(0)         -> "$0.00"
 */
const usdFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

export function formatMoney(amountInCents: number): string {
  return usdFormatter.format(amountInCents / 100);
}

/**
 * Same thing, but for values that are already whole dollars
 * (e.g. a UI input field before you convert it to cents for storage).
 *
 * formatMoneyFromDollars(48250) -> "$48,250.00"
 */
export function formatMoneyFromDollars(amountInDollars: number): string {
  return usdFormatter.format(amountInDollars);
}

/**
 * Compact variant for tight UI space (dashboards, chips) — rounds to
 * the nearest thousand/million rather than showing exact cents.
 *
 * formatMoneyCompact(4825000000) -> "$48.3M"
 * formatMoneyCompact(150000)     -> "$1.5K"
 */
const usdCompactFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  notation: "compact",
  maximumFractionDigits: 1,
});

export function formatMoneyCompact(amountInCents: number): string {
  return usdCompactFormatter.format(amountInCents / 100);
}