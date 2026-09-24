# IM07 — Cup qualification engine: position bands, last-season standings, champion entitlement

**Status:** Not started
**Sprint:** Improvements (competition scheduling)
**Source:** Product decision (manual session)
**Depends on:** IM06 (the `cup_qualification` / `manager_cup_choices` tables and
region foundation); S04-01 (season lifecycle — completed seasons with final
standings and a `champion` entry); IM03 (tie-break ordering reused for ranks).
Feeds IM08 (regional cup campaigns) and IM09 (double-book resolution).

## What to do

Build the **pure qualification engine**: given a cup and the cup's
`cup_qualification` position-band rows, compute the exact set of clubs that
enter the cup, based strictly on the **last completed season** per league —
positions from the final table, plus the **reigning-champion entitlement** that
every cup field honours. This is the deterministic core the campaign start
(IM08) and the conflict/resolution sweep (IM09) both call; it writes nothing.

1. **Position bands** — each `cup_qualification` row names a league and a band
   `from..to` (to NULL = through last place). The field is the union over rows
   of the league's clubs ranked in its **most recent `completed` league season**,
   cut to the band.
2. **Champion +1** — the cup's **previous completed campaign's champion**
   (reigning champion) is *always* part of the field, so a field is
   **band + 1 distinct club** whenever the champion's league has a band row,
   honouring "always 5 total when a league has a champ".
3. **Determinism** — identical tables + identical qualification rows ⇒ identical
   field, replayable like league seeding; no live-matchday reads, no `now()`
   in the computation.
4. **Deferred seams** — the engine flags clubs that look double-booked
   (qualified for more than one continental cup) but leaves the *resolution*
   (tier precedence / manager choice) to IM09. It also must expose a
   `QualifyField` entry point that IM09 reuses per-cup across a world.

## Behaviour

### Inputs and interface

```go
// QualifyField computes the next field for a cup. Pure: it depends only on the
// provided providers, never on the live campaign or wall-clock time.
type QualifyField struct {
    Cup        competition.Competition      // format knockout, scope country|region
    Region     *world.Region                // nil for country cups
    Country    *world.Country               // nil for regional cups
    Bands      []QualificationBand          // cup_qualification rows (from/to)
    Tables     map[leagueID]Standings       // most recent COMPLETED season per league
    Reigning   *Club                        // champion of the cup's last completed campaign
}
```

- `Band = { League, From, To (nil = last) }` mirroring the row exactly.
- `Table = { Ranks []ClubID, SeasonNumber }` — the final ordering using
  existing tie-break order (`points DESC, GD DESC, GF DESC, name`), already
  produced by `competition.standings` for a completed season.
- Providers injected: `QualifyField` gets a `StandingsProvider`/`SeasonProvider`
  interface over the pool so unit tests run without a database; the campaign
  entry points (IM08) wire the real queries.

### Position bands

- For each Band: take `Ranks[from-1 : ≤to]` (1-based; `to == nil` → through the
  end). Clamp `to` to the table length; a band beyond the table length yields
  an **empty set** (allowed — recorded as a warning, not an error).
- Dedupe across bands is **implicit**: any club appears only via its own
  league's row, and bands are per-league; a club cannot satisfy two rows of the
  same league. Cross-league overlap is impossible by construction (a club holds
  exactly one `role='league'` membership).
- **No completed season for a banded league** → qualification is unavailable
  for that row: the engine must not guess from an in-progress season. The
  *preview* path (IM08) warns; a *campaign start* with such a row is a hard
  422 (`ErrQualificationUnavailable`) rather than proceeding on live table
  slicing.

### Reigning-champion entitlement

- `Reigning` is the `competition_entries.status = 'champion'` club of the cup's
  most recent `completed` **cup** season (not the league season). NULL when the
  cup has no completed campaign yet.
- Resolution on the champion's league (the league whose last-season table
  contains the champion, i.e. the row whose league that club belongs to):
  - **Champion inside its band** → the field is the band **plus the next-best
    club not already in the field** (highest rank outside the band / after the
    cut, skipping any club already entered) ⇒ exactly `len(band) + 1`.
  - **Champion outside its band** (e.g. the band is `1..2` and the champion
    finished 7th) → the field is the band **plus the champion** ⇒ `len(band) + 1`.
  - **Champion's league has no band row** → the champion's country may have a
    lower-tier league in the cup but the champion is in a league that isn't
    named → the champion enters **directly** (origin `champion_direct`), still
    recorded as part of the field.
- Every entrant carries an **origin** (`position` for a band cut,
  `champion_next_best`, `champion_out_of_band`, `champion_direct`) so the
  preview, the news text (IM09), and tests can show *why* a club is in.

### Double-book flags (no resolution here)

- The engine marks each entrant with `conflicts []{otherCupID, tier}` when it
  also appears in another continental cup's field in the world. IM09 reads
  these flags and performs the sweep; IM07 only exposes the per-cup field and
  the flag list. Nothing is written in this task.
- The single-cup view is always the entitlement-maximal view: every champ is
  in, and only resolver logic (IM09) *removes* a champ from a cup.

### Errors

- `ErrQualificationUnavailable` — campaign start with a banded league lacking a
  completed season.
- `ErrQualificationField` (422) — final field `len < 2` (a knockout needs two
  clubs); the preview surfaces this before the admin commits.
- There is **no 404 from the engine** — identity/scope errors happen in the
  HTTP layer (IM08); the engine only computes.

## Changes

### internal/competition

- `qualify.go` (new): `QualifyField` struct + `ComputeField(ctx, cup, providers)`
  entry, band expansion, champion cases, origin tagging, conflict flags, and
  the `StandingsProvider`/`SeasonProvider` interfaces (with the pool-backed
  implementation returning the last completed standings via the S04-01
  completed-season query, and a `hist/`-style previous-campaign champion query).
- `service.go`: `ErrQualificationUnavailable`; validation glue for band rows
  (`from >= 1`, `to == nil || to >= from`) — mirrored in IM06's test matrix.

### Tests (this task is engine-first; most coverage lands here, not in the HTTP layer)

- Unit (DB-free via injected providers): band slicing incl. `to` clamping and
  empty bands; ranking-order cut honouring the tie-break order; champ-in-band
  adds next-best; champ-out-of-band adds champ; champ-direct when its league
  has no row; "always 5 total when a league has a champ" invariants (band sizes
  1,2,3,4 with champs); dedupe / next-best skipping already-entered clubs;
  empty-field (`len < 2`) error; determinism (two identical inputs ⇒ equal
  fields); conflict flags populated when a champion is shared across two cups'
  fields.
- Provider unit: a fake `SeasonProvider` answering "completed season N-1" vs
  "no completed season" drives both the happy band path and the
  `ErrQualificationUnavailable` path without a DB.
- Integration (CI-only): a small finished league through the real pool, then
  `ComputeField` twice — identical results (replay determinism), correct origin
  labels, field counts, and conflict flags against a second regional cup.

## Docs

- `docs/tasks/improvements/IM07-cup-qualification-engine.md` (this file).
- `docs/tasks/improvements/IM08-regional-cup-campaigns.md` / `IM09-…` list this
  engine as their dependency.
- `docs/how-to/glossary.md`: `position band`, `reigning champion`, `next-best`.

## Recorded decisions

- **Qualification is historical, never live**: every band and the champion
  entitlement read the **last completed season** ("qualification is based on
  the last season's performance for each club"). There is deliberately no fall
  back to an in-progress table — a banded league without a completed season
  cannot qualify clubs, and campaign start rejects that configuration (the
  preview warns first).
- **The champion rule is band + 1, not champion-only**: the band always ships
  its full quota and the champion adds exactly one extra distinct club —
  "always 5 total when a league has a champ". A champion inside its band makes
  the +1 the next-best club; outside the band it is the champion itself.
- **Position is a resolution / mismatch**: a champion whose country cups are
  built from a different league than the one it finished in enters directly;
  the entry is still explicit in the field, not invented.
- **Single-cup entitlement-maximal, resolution deferred**: this engine never
  drops a champion; dropping/ceding is IM09's tier-precedence + manager-choice
  sweep. Keeping the computation pure here makes the IM09 resolver testable in
  isolation.