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


/**
 * Formats fixture kickoff times for display. Fixtures come back from the
 * API as UTC ISO strings (e.g. "2026-06-07T19:00:00Z") — Intl.DateTimeFormat
 * converts these to the VIEWER's local time zone automatically, which is
 * what you want for a global player base watching the same kickoff.
 */

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  weekday: "short",
  month: "short",
  day: "numeric",
});

const dateFormatterCompact = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
});

const timeFormatter = new Intl.DateTimeFormat("en-US", {
  hour: "numeric",
  minute: "2-digit",
});

/**
 * Full display, for a fixture detail page or match header.
 * formatFixtureDateTime("2026-06-07T19:00:00Z") -> "Sat, Jun 7 · 7:00 PM"
 */
export function formatFixtureDateTime(iso: string): string {
  const date = new Date(iso);
  return `${dateFormatter.format(date)} · ${timeFormatter.format(date)}`;
}

/**
 * Compact display, for dense fixture lists/tables — drops the weekday.
 * formatFixtureDateTimeCompact("2026-06-07T19:00:00Z") -> "Jun 7 · 7:00 PM"
 */
export function formatFixtureDateTimeCompact(iso: string): string {
  const date = new Date(iso);
  return `${dateFormatterCompact.format(date)} · ${timeFormatter.format(date)}`;
}

/**
 * Smart relative label for "today/tomorrow" fixtures, falling back to the
 * full format otherwise — the common pattern for a schedule/fixtures screen.
 * formatFixtureDateTimeSmart(<today's kickoff>)    -> "Today · 7:00 PM"
 * formatFixtureDateTimeSmart(<tomorrow's kickoff>) -> "Tomorrow · 7:00 PM"
 * formatFixtureDateTimeSmart(<next week's kickoff>) -> "Sat, Jun 14 · 7:00 PM"
 */
export function formatFixtureDateTimeSmart(iso: string): string {
  const date = new Date(iso);
  const now = new Date();

  const isSameDay = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate();

  const tomorrow = new Date(now);
  tomorrow.setDate(now.getDate() + 1);

  if (isSameDay(date, now)) return `Today · ${timeFormatter.format(date)}`;
  if (isSameDay(date, tomorrow)) return `Tomorrow · ${timeFormatter.format(date)}`;
  return formatFixtureDateTime(iso);
}