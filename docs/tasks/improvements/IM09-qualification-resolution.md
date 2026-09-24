# IM09 — Qualification resolution: tier precedence, manager cup choices, and commitment news

**Status:** Not started
**Sprint:** Improvements (competition scheduling)
**Source:** Product decision (manual session)
**Depends on:** IM07 (the qualification engine and its per-cup conflict flags);
IM08 (regional cup campaigns write fields; a club can legitimately qualify for
two); IM04 (news + event plumbing); IM06 (the `manager_cup_choices` table).

## What to do

Resolve the only rule the engine cannot answer alone: a club that qualifies for
**more than one** continental cup (always the reigning champion, effectively)
must end up in exactly one of those cups. This task defines and ships the sweep
that decides, in priority order:

1. **Tier precedence** — a champion claimed by a higher-tier cup plays THAT cup;
   the other cup's +1 slot cascades to the next-best club (news published).
2. **Manager choice** — for equal- or lower-tier conflicts, the club's **manager
   chooses** which cup to play (`manager_cup_choices`), a **news story** is
   published, and the un-chosen cup's slot cascades to next-best.
3. **Deterministic default** — no prior choice + no higher tier ⇒ the champion
   defends the cup it is reigning champion of; ties resolve to the **earlier-
   created** cup.
4. **Manager surfacing** — `GET /clubs/:id/cup-qualifications` shows the club's
   projected entries, conflicts, current choices, and next-best replacements;
   `POST /clubs/:id/cup-choices` records the decision. Resolution runs at
   campaign start (IM08), consumes the choices, and never touches live tables.

## Behaviour

### The sweep (`ResolveField`, in `internal/competition/resolve.go`)

- Input: every continental/regional cup in the world with its IM07 field (the
  entitlement-maximal view before resolution), plus `manager_cup_choices`.
- For each champion `C` of cup `A` (its `champion_*` entry) that also appears
  in cup `B`'s field:
  - `B.tier > A.tier` → `C` is **claimed by B**. `A` loses `C`, `A`'s +1 slot
    cascades to `B`→`A`'s league's next-best club (not already in `A`'s field),
    and a news story is published naming `C`, `A`, `B`, and the replacement.
  - `A.tier > B.tier` → symmetric: `C` stays in `A`, `B` cascades.
  - **Equal tiers (or both empty)**: if a `manager_cup_choices` row for `C`
    opts into `A` → `C` stays in `A`; opts into `B` → `C` moves to `B`; a row
    for the *other* cup is treated as forfeiture of this one. With no row for
    either, the default: `C` keeps the cup it is **reigning champion of**; tie
    → the **earlier-created** cup wins. The loser cascades.
- Cascades beget cascades: a club promoted from next-best is itself considered
  for conflicts with its own cup entries in the same sweep pass; the sweep
  loops to fixpoint, always bounded by the finite membership graph, then checks
  invariants (every champion in exactly one cup, every cup ≥2, no club in two
  cups) — a residual violation is a hard error at campaign start, never a
  silent drop.
- Resolution output is a **per-cup field assignment** handed to IM08's
  membership/entry writes. It is deterministic: same fields + same choices ⇒
  same assignments.

### Commitment news

- One `world.news_stories` row per cascade: subject = the club losing its slot,
  text naming both cups, the affected cup, and the replacement club; emitted
  inside the campaign transaction along with the final memberships so reads and
  news never disagree. Follow the existing news publisher plumbing (IM06-adjacent
  groundwork / S04) — no new feed columns.
- A manager choice resulting in a move also emits a short confirmation story;
  a no-op re-commit of the same choice does not.

### Manager surface

- `GET /clubs/:id/cup-qualifications`: the club's continental/regional cup
  outlook — per cup: qualification row/origin, tier, projected entry, conflicts
  (list of other cups + tier), and the club's current recorded choice; plus the
  next-best replacements that would take its place if it forfeits. Read-only;
  world-scoped to the caller's club.
- `POST /clubs/:id/cup-choices` `{"cup_id": …}`: records an opt-in; upsert on
  `UNIQUE(club_id, cup_id)`; `422` when the club is not projected for that cup
  (a choice for a cup you are not in is rejected), `403` no world context.
- Choices may be recorded **any time between the previous completed campaign
  and the cup's next campaign start**; the sweep consumes them at start and the
  view shows them live so a manager can see the outlook converge as they choose.

### Cap exemption, formalized

- Auto-qualified entrants (champion, `champion_*`, and next-best cascade
  replacements) are exempt from the 3-cup membership cap — the entitlement can
  never be denied by a cap error (`ErrCupLimit`). Positional (band) entrants
  remain capped and the sweep never assigns a positional club beyond the cap;
  such an assignment is the only case the resolver may refuse (422 at start).

## Changes

### internal/competition

- `resolve.go` (new): `ResolveField` — consumes `[]Field` (IM07 output) +
  `manager_cup_choices`; tier precedence, equal-tier choice, default
  (defend + earlier-created), fixpoint cascades, invariant check
  (`ErrResolutionImpossible`), deterministic by construction.
- `campaign.go`/`cup.go`: IM08's campaign start now calls `ResolveField`
  before writing memberships/entries; news writes on cascades; second start
  `409` unchanged.
- `service.go`: `ErrResolutionImpossible`, choice-validation errors,
  cup-qualifications read surface.
- `news.go` (or the existing news seam): `PublishCupForfeit` / `PublishChoice`.

### internal/httpapi + openapi

- `club_handlers.go`/`cup_handlers.go`: `GET /clubs/:id/cup-qualifications`,
  `POST /clubs/:id/cup-choices` (`requireAuth` + caller's club only).
- `openapi.yaml`: `CupQualificationView`, `CupChoice`, the two routes; the
  `Cup` admin read gains the tier/region fields; `TestDocsCoverRouter` updated.

### Frontend

- Manager: a "Cup outlook" tile on the club page listing projected cups,
  conflicts highlighted, an in-line choice picker per conflicted cup, and the
  replacement info. News feed already renders the cascade stories via existing
  news reads.

## Tests

- Unit (DB-free): the **choice matrix** — champion in A(tier1)+B(tier1): no
  choice → defend; choice for B → B; choice for A → A; equal-created tie →
  earlier cup; cross-tier (tier2 vs tier1) → higher tier irrespective of choice;
  both-empty tiers equal; cascade chain (A→next-best x qualifies for C → x
  forfeits A for C, A cascades again) reaches fixpoint; invariant violations
  raise `ErrResolutionImpossible`; determinism on same inputs.
- Unit: choice validation (`422` for a cup the club is not projected for);
  duplicate choice upsert no-op.
- Integration (CI-only): two regional cups in one region — one champion in both;
  campaign start → exactly one membership for the champion, replacement entered
  in the loser cup, news rows emitted naming clubs/cups; manager records a
  choice then re-start path flips the outcome; cap exemption (champion +
  cascade exceeding 3 cup memberships does not error); positional cap still
  enforced; outlook endpoint returns converging state after each choice.
- Regression: IM08 country-cup + league paths untouched and green.

## Docs

- `docs/tasks/improvements/IM09-qualification-resolution.md` (this file).
- `docs/how-to/cup-competitions.md`: "Double-qualified champions" section —
  tier precedence, manager choice, the defend-default, and news.
- `docs/how-to/glossary.md`: `reigning champion`, `tier precedence`,
  `next-best`, `cup choice`.
- `docs/product_manager.md`: recorded decision — a champion always plays one
  cup; automatic precedence/minimum-tier rules + explicit manager opt-in, with
  deterministic defaults, never a silent drop.

## Recorded decisions

- **First tier takes precedence** (product decision): a higher-tier cup always
  claims a double-qualified champion; the forfeited cup cascades its +1 slot to
  the next-best club. No choice can override a strict tier difference.
- **Managers choose among equal/lower tiers**: for any remaining conflict the
  club's manager opts in via `POST /clubs/:id/cup-choices`; a news story
  documents the move and the freed slot goes to the next-best club.
- **Deterministic default**: silence = the champion defends the cup it is
  reigning champion of; otherwise the earlier-created cup wins. Everything must
  be seed/choice-derived — no wall-clock tie-breaks.
- **Cascades are resolved to a fixpoint**, then invariants hold or the campaign
  start fails loudly (`ErrResolutionImpossible`) — a club is never silently
  dropped from every cup.
- **News is transactional with the campaign**: every forfeit/replacement is
  published inside the same transaction as the final memberships, so the news
  feed and the schedule never diverge.
- **Auto-qualified entrants are cap-exempt** (their entitlement can't be denied
  by the 3-cup cap); positional entrants keep `ErrCupLimit` semantics.