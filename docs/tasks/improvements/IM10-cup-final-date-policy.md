# IM10 — Cup final-date policy: calculated (recurring) or fixed, with per-campaign edit

**Status:** Not started
**Sprint:** Improvements (competition scheduling)
**Source:** Product decision (manual session)
**Depends on:** IM05 (the anchored cup calendar — `roundPlan.Date`, `planCupCalendar`,
`findCupRoundDay`, `cupGap`, the `materializeRound` slot preference, `publishSchedulingNews`);
IM04 (cup lifecycle — `CreateCup`, `cupPlan.ladder`, lazy round materialization);
IM06 (regions, `competitions.region_id`); IM08 (regional cups scope their calendar
to the **union of participating countries' league days** — this task generalizes that
same anchor to be admin-configurable). The default path of an unconfigured cup must
reproduce the IM05 calendar byte-for-byte.

## What to do

Give the admin real control over **when a cup's final is played**, declared at cup
creation and editable for a live campaign. Today the final date is derived
mechanically (IM05): the first allowed weekday at least **3 game-days after the
country's latest league fixture**, earlier rounds walking backward on seeded 2-3-day
gaps. IM10 replaces that with a **final-date policy** on the cup:

1. **Calculated (recurring)** — the system derives the final date each season from
   the IM05 anchor, with an **admin-tunable day offset** `K` (default 3) past the
   scope's latest league fixture. The admin may **edit the computed final date** for
   the current campaign before the final round is materialized; the remaining rounds
   re-stamp backward from the edited anchor. Each new season recalculates from the
   league calendar — the edit is a per-campaign override, not a declaration change.
2. **Fixed (one-time)** — the admin pins a single absolute date; the final is played
   on exactly that date every season, **never recalculated**. Earlier rounds walk
   backward from it on the same seeded gaps.
3. **Anchor scope** — country cups anchor to their country's league days (unchanged
   IM05); **regional cups** anchor to the union of the region's countries' league days
   (the scope a regional final must clear), per IM08's calendar semantics.

Nothing moves on its own: an unconfigured cup keeps `calculated`/`K=3` and lands
exactly where IM05 puts it; an edit only ever re-stamps rounds that are not yet
materialized; the computation stays deterministic and never reads the wall clock.

## Behaviour

### The policy on the declaration (`competition.competitions`)

- `final_date_mode TEXT NOT NULL DEFAULT 'calculated'` with
  `CHECK (final_date_mode IN ('calculated','fixed'))`.
- `final_date DATE NULL` — the absolute final date for `fixed`; NULL otherwise.
- `final_offset_days INT NOT NULL DEFAULT 3` with `CHECK (final_offset_days >= 0)` —
  the `K` in "K game-days after the latest league fixture" for `calculated`.
- `CreateCup` persistence and validation:
  - `fixed` requires `final_date` (else `422`); `calculated` ignores a provided date
    and requires `final_offset_days >= 0` (else `422`).
  - Default modes reproduce the IM05 calendar exactly (regression-guarded).

### Calculated (recurring) anchor

- At campaign start, `planCupCalendar` stays the single place that stamps the ladder:
  the final lands on the **first allowed weekday at least `final_offset_days`
  game-days after the scope's latest league fixture** (weekday set from the cup's
  `scheduling_rules`, exactly as IM05 resolves it). Earlier rounds walk backward on
  `cupGap` + `findCupRoundDay` like today.
- Scope of "latest league fixture": the **owning country** for `country_id` cups
  (today's `countryLeagueDays`); for a region cup it is the **union over the region's
  countries** — `c.country_id IN (SELECT id FROM world.countries WHERE region_id = $1)`
  — sorted and deduped. This is the generalizing rename of `countryLeagueDays`:
  `leagueDays(ctx, tx, worldID, scope)` where scope is country or region.
- If the scope has **no league fixtures** (nothing running yet) the final is not
  stamped and `materializeRound` keeps the legacy weekly formula — unchanged, except
  that a `fixed` cup (below) pins its date regardless.

### Fixed final date

- The final round's stamped date is exactly `final_date` (that day, the cup's
  seeded kickoff hour). A fixed date is **authoritative**: it is not re-snapped to an
  allowed weekday or moved off a league day — the admin picked the day. Earlier
  rounds still walk backward on `cupGap` and snap to league-free days when the scope
  has league days; with none, they fall back to the weekly formula backward from the
  fixed date.
- `final_date` applies every season; there is no recalculation.

### Per-campaign edit (`PATCH /api/admin/cups/:id/final-date`)

- Body `{final_date: "2026-05-16" | null, final_offset_days?: n}`.
  - `calculated` cup: `final_date` sets a **campaign override** — a new stamp on the
    final `roundPlan.Date` of the persisted ladder (`competition_rules.
    qualification_rules->'campaign'`); `null` clears the override and restores the
    computed anchor. Editing `final_offset_days` re-derives the anchor for **future**
    seasons (and clears any override).
  - `fixed` cup: `final_date` updates the declaration (and the live ladder stamp);
    `final_offset_days` is rejected (`422`), since fixed ignores offsets.
- **Window**: an edit is allowed only while the **final round is not materialized**.
  Rounds at or below the last materialized round are **frozen** — their ties and
  fixtures stay; only rounds after the frontier are re-stamped backwards from the
  (possibly edited) anchor. Editing the final date earlier than the latest frozen
  round's date is rejected (`422`) — history never moves.
- Re-stamping reuses the exact IM05 back-walk (`cupGap` + `findCupRoundDay`), so the
  result is deterministic given world, cup, and the edited anchor.
- A successful edit that moves dates publishes a **`scheduling` news story** via the
  existing `publishSchedulingNews` plumbing (same transaction), matching IM05's
  contract that feeds never describe a calendar the engine didn't keep.
- Reads: `GET /api/cups/:id` campaign view and the `Cup` shape expose
  `final_date_mode`, `final_date` (the override/stamp for the current campaign, the
  declaration date for fixed), and `final_offset_days`, so the admin can see both the
  policy and what the engine computed for this season.

## Changes

### Migration `0053`

- `competition.competitions`: `final_date_mode TEXT NOT NULL DEFAULT 'calculated'
  CHECK (final_date_mode IN ('calculated','fixed'))`, `final_date DATE NULL`,
  `final_offset_days INT NOT NULL DEFAULT 3 CHECK (final_offset_days >= 0)`.
- `.down.sql` drops the three columns. `migrations/README.md` gets a `0053` row.

### internal/competition

- `cup.go`: `CupParams`/`Cup` gain `final_date_mode`, `final_date`,
  `final_offset_days`; `CreateCup` validation + persistence; `planCupCalendar` selects
  the anchor (fixed date / campaign override / computed league-end + offset) and the
  scope-aware league-day union; `countryLeagueDays` is renamed/generalized to
  `scopeLeagueDays` (country vs region) with the old country call sites updated;
  `cupGap`/`findCupRoundDay`/`materializeRound` reused untouched.
- `scheduling.go` (or `cup.go`): `SetCupFinalDate` — validates mode + edit window,
  finds the materialization frontier, re-stamps unfrozen ladder rounds, persists the
  plan JSONB, publishes scheduling news.
- `service.go`: sentinels `ErrCupFinalDateInvalid` (fixed without date, offset < 0,
  offset set on fixed) and `ErrCupFinalDateLocked` (edit after the final round is
  materialized, or an anchor earlier than the frozen frontier).
- IM05 default reproducibility: `planCupCalendar` with `calculated`/`K=3`/no override
  produces the current ladder dates for every existing cup — no fixture moves.

### internal/httpapi + openapi

- `cup_handlers.go`: `handleCreateCup` accepts the new fields (both scopes);
  new `handleSetCupFinalDate` → `PATCH /api/admin/cups/:id/final-date`
  (`requireAuth` + `requireAdmin`; body `{final_date, final_offset_days?}`).
- `openapi.yaml`: `CupFinalDatePolicy` ref (mode, offset, date), the baked
  fields on `Cup`/create body, the PATCH document; route-coverage test
  `TestDocsCoverRouter` green.
- Frontend `admin/competitions.vue`: final-date section in the cup form —
  mode toggle (Calculated / Fixed), day-offset number for calculated, date
  picker for fixed, and for a live campaign a "last materialized round" hint
  next to the edit control; no manager-facing change here.

## Tests

- Unit (DB-free, pure `planCupCalendar` fed canned league days): anchor
  selection — `calculated` + offset (0, 3, n) lands the first allowed weekday
  ≥ K after the latest scope league day; `fixed` pins the exact date with no
  weekday snapping; a campaign override beats the computed anchor and `null`
  restores it; the backward walk never moves a round at/below the frozen
  frontier; `finalAnchor` determinism (same world+cup+date ⇒ same ladder);
  validation — fixed without date, negative offset, offset on fixed, edit after
  the final round is materialized, anchor earlier than the frontier.
- Integration (CI-only): country world → **calculated** cup with default
  reproduces `TestCupCalendarAnchoredToLeagueEnd` dates exactly (regression);
  a **fixed** cup campaign lands the final on `final_date` and back-walks the
  earlier rounds; a **region** cup anchors ≥ K after the *latest* league end
  across two countries sharing no calendar overlap; `PATCH` moves the final and
  re-stamps only unfrozen rounds, publishes a `scheduling` story, and a second
  edit after the final round materializes returns `422`/`ErrCupFinalDateLocked`.
- Regression: `TestCupCalendarAnchoredToLeagueEnd`, `TestFixturePacing`,
  `TestWeekdayPacingAndRePace`, all IM04 cup/ladder tests (unconfigured cups
  keep `calculated`/`K=3`), `TestDocsCoverRouter`.
- Frontend: `pnpm typecheck` + `pnpm lint` on touched files.

## Docs

- `docs/tasks/improvements/IM10-cup-final-date-policy.md` (this file).
- `docs/how-to/cup-competitions.md`: final-date policy section — calculated vs
  fixed, the `K` offset, the per-campaign edit window + frozen rounds.
- `docs/how-to/glossary.md`: `final date policy` (calculated/fixed), "campaign
  final-date override".
- `docs/product_manager.md`: recorded decision — the admin owns the cup final's
  timing via a per-declaration policy; recurring dates derive deterministically
  from the league calendar and are editable per campaign until the final round
  materializes.
- `backend/migrations/README.md`: `0053` row; `docs/tasks/README.md`
  improvements list gains IM10.

## Recorded decisions

- **Two modes, default calculated**: `calculated` (recurring — deterministic
  derivation each season, tunable `final_offset_days`, per-campaign editable)
  and `fixed` (one absolute date, authoritative and never recalculated). The
  default reproduces IM05 byte-for-byte, so existing cups never move.
- **Recurring dates derive from the league calendar, never the wall clock**:
  the anchor is the scope's latest league fixture (+ K, weekday-snapped);
  regional cups use the union of the region's countries' league days (IM08).
  No `now()` in the computation — replay determinism holds.
- **A fixed final date is pinned exactly** — no weekday re-snap (the admin
  chose the day) — while earlier rounds still avoid league days where any exist.
- **Edits freeze history**: a final-date edit only re-stamps rounds after the
  last materialized round; an anchor earlier than the frontier is rejected, and
  a live cup's final can no longer be moved once that round is materialized
  (`ErrCupFinalDateLocked`). Date-moving edits publish `scheduling` news in the
  same transaction, matching IM05's feed contract.
- **Per-campaign override stored on the ladder**: the edited date rides the
  persisted `roundPlan.Date` of `cupPlan.ladder` (the same stamp
  `materializeRound` already honors) — no new calendar state table.