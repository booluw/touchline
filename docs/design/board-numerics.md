# Board Numerics (S06-02)

Source of truth for the implemented numbers behind board confidence, structured
mandates, and management sackings. The product task
(`docs/tasks/S06-02-board-confidence-mandates-and-sackings.md`, PRD
job-security sections, technical plan §16) publishes intent; this document
fixes the implemented values and binds them to the existing `club.*` +
`manager.job_security_snapshots` schema (shipped unused since migrations
0005/0007). All numbers here are **proposal** data until PM tuning sign-off —
every block is config expressed as constants in `internal/board/numerics.go`
(never freeform in engine code), like `squad.DefaultPositionWeights`.

Money is **pence** (`int64`) at the service boundary and `NUMERIC(14,2)`
pounds in `finance.*` columns.

## 1. Why the engine reviews a club

On every **month-boundary** world tick (`handleWorldTick` in `internal/app`,
day `current_day % calendar.days_per_month == 0` — default day 30, IM02),
`board.Service.Review` scores every club that has an
active manager in the world. The same scoring also runs lazily when a manager
opens `GET /api/manager/me/board`, so the board page is never stale; §9 records
the consequence for the sentiment figure.

1. **Scores** the manager against the club's open mandates (see §3).
2. **Snapshots** the seven factor scores + their weighted total into
   `manager.job_security_snapshots` (one row per manager per tick — the write
   is idempotent per `(manager_id, world_tick)`, so redelivered ticks replay
   without doubles).
3. **Emits** `BOARD_REVIEWED` (explanation `board_confidence` whose factors
   sum exactly to the weighted total — OPD-12 contract, opt-in `Validate()`)
   plus `BOARD_MANDATE_MET` / `BOARD_MANDATE_BROKEN` for each mandate resolved
   this tick.
4. **Sacking guard**: if the weighted total ≤ `SackThresholdTotal = 25` **and
   the club is human-managed** (`manager.managers.is_policy_bot = FALSE`), the
   board sacks the manager — `manager.Service.Sack(managerID, exp)` hands the
   club back to AI control (`is_ai_controlled → TRUE`, policy-bot manager
   stands in, `MANAGER_SACKED` event carries the `board_confidence`
   explanation). AI-managed clubs are scored identically but **never sacked**;
   a human going broke can only be replaced via the OPD-16/OPD-05 hiring flow.

Reasons per-lens are persisted in the snapshot rows and event explanations;
consumers render stored explanations and never recalculate.

## 2. Board persona and DNA

Each club gets one board persona (`club.boards.personality_type`) seeded at
bootstrap from the club's DNA archetype (`internal/bootstrap/profile.go`,
weighted draw of six archetypes). DNA lives in `club.club_dna`
(`competitive_ambition`, `patience`, …).

**Personas** (`board.Persona`):

| Persona | Weight row (see §4) | Negotiation tolerance (§5) |
| --- | --- | --- |
| `patient_owner` | {0.18, 0.12, 0.18, 0.18, 0.14, 0.10, 0.10} | 3 |
| `demanding_owner` | {0.30, 0.25, 0.12, 0.10, 0.10, 0.08, 0.05} | 0 |
| `financial_conservative` | {0.18, 0.12, 0.30, 0.12, 0.10, 0.08, 0.10} | 2 |
| `academy_owner` | {0.12, 0.10, 0.12, 0.12, 0.32, 0.10, 0.12} | 2 |
| `prestige_owner` | {0.28, 0.22, 0.10, 0.10, 0.12, 0.12, 0.06} | 0 |
| `political_board` | {0.18, 0.18, 0.14, 0.22, 0.08, 0.12, 0.08} | 1 |

Unknown stored personas fall back to `political_board` rather than panic.

## 3. Mandate set (four categories)

The engine grades exactly four structured mandates, one per category, created
lazily on first board view for a new `(club, manager, season)` (insert
`ON CONFLICT DO NOTHING`, unique per category per season enforced by migration
0039 so only one open mandate per category exists — earlier seasons' open rows
are ignored by the season-scoped reads). Targets:

`expectedFinish → finish = clamp(round((110 − ambition) / 8 [+ 1.5 if
patience ≥ 70]), 1, 24)`; `expectedPoints → points = clamp(round(88 − 3.5 ×
finish), 10, 95)`.

| Category | `target_type` | Seed target | Evaluated against |
| --- | --- | --- | --- |
| `primary` | `league_finish` | `finish` | league position |
| `secondary` | `points_target` | `points` | league points |
| `strategic` | `operating_balance` | `0` (break-even) | operating profit |
| `financial` | `wage_structure` | `0` (wage budget) | committed wages vs budget |

Mandate states: `pending → agreed` (negotiation) `→ met | broken`; broken or
met closes the row (`resolved_at`), and only open (`pending`/`agreed`) rows are
scored.

### Grading rules (per open mandate, every month-boundary review)

Progress = played / total fixtures (clamped 0–1); season-complete = all
league fixtures played.

- **league_finish**: slack shrinks with the season —
  `finishSlack = 3 (<40%), 2 (<80%), 1 (≥80%), 0 (complete)`. Met when
  `position ≤ finish + slack`; broken when `position > finish + 6`
  (`FinishBrokenMargin`) mid-season — season-complete is strict
  (`position > finish`). Between slack and the broken margin the mandate stays
  open. No league standings ⇒ neutral.
- **points_target**: scaled target = `round(target × progress)` mid-season.
  Met when `points ≥ scaled`; broken when `points ≤ scaled − 10`
  (`PointsTargetWindow`) or the season is complete with any shortfall.
- **wage_structure**: allowance = `budget × (1 + targetPP/100 + 5%)`
  (`WageTolerancePct`); committed-annual wages within ⇒ met, else broken.
  No budget ⇒ ungraded.
- **operating_balance**: `operatingProfit ≥ 0` ⇒ met; a loss beyond
  `¼ × wageBudget` (`OperatingLossWageShare`) ⇒ broken; small front-loaded
  losses stay open. No budget with a loss ⇒ broken.

## 4. The seven factor scores and the weighted total

Each lens is a 0–100 score:

| Factor | Formula | Notes |
| --- | --- | --- |
| `performance_score` | mean of the manager's last 8 per-match ratings at this club (§4a); with none on record, `clamp(50 + 8·(expectedFinish − position), 0, 100)` | neutral 50 with no ratings and no league standings |
| `expectations_score` | `clamp(50 + 10·met − 15·broken, 0, 100)` | resolved mandates this/previous reviews this season (month boundaries) |
| `financial_score` | `50 + round(40·(1 − wageRatio)) ± 10` profit signal | wageRatio = committedAnnual/budget capped at 1; `+10` operating profit / `−10` loss; `60` (or `50` with committed wages) when no budget |
| `board_relationship_score` | `clamp(50 + (patience − 50)/5 + 25·(met − broken)/resolved, 0, 100)` | patience softens; kept-mandate ratio pressures |
| `club_dna_alignment_score` | `clamp(50 + 15·met_strategic + 15·met_financial − 25·broken_strategic − 25·broken_financial, 0, 100)` | deeper DNA adherence deferred to S10-03 |
| `supporter_sentiment_score` | stored `club.supporter_groups.current_sentiment` | read-only in the review; it moves after every match (§4a), clamped [15, 95] |
| `alternatives_score` | `clamp(50 + 5·worldReputation, 10, 90)` | manager career-log reputation; replacement-pressure proxy |

**Weighted total** = Σ `round(wᵢ × factorScoreᵢ)` with the persona's seven
weights (§2), clamped to [0, 100]. Because each explanation factor delta is the
same rounded contribution, the factor deltas **sum exactly to the total**
(a tested guarantee).

## 4a. Per-match rating and supporter reaction (IM33)

Every completed match (quick-play and live) rates both managers inside the
match-completion transaction (`board.RecordCompletedMatch`). Clubs without a
manager are skipped; bot-managed clubs are rated like human ones. **Board
confidence is not recalculated here** — it stays on the monthly review, which
reads the stored ratings.

| Step | Formula |
|---|---|
| expected points share | `clamp(0.5 + 0.004·(ownReputation − opponentReputation) ± 0.08 (home +, away −), 0.10, 0.90)` |
| actual | win 1, draw 0.5, loss 0 |
| match rating | `clamp(round(50 + 60·(actual − expected)) + 5·clamp(goalDifference, −3, 3), 0, 100)` |
| supporter sentiment | `sentiment += α·(rating − sentiment)`, `α = 0.08`, doubled (0.16) in a rivalry game, clamped [15, 95] |

A **rivalry game** is one where the two clubs' `rivalry` edge in
`social.relationships` has strength ≥ 50. Every meeting grows that edge (a first
meeting reaches at most 40), so the threshold — not the edge's existence — is
what marks a real rivalry.

Each rating is one row in `manager.match_ratings` keyed `(fixture_id, club_id)`
(rating, sentiment before/after); the key makes a replayed completion a no-op.

**Fan-reaction news.** For a **human-managed** club the same hook publishes one
`fan_reaction` story linked to the fixture's `MATCH_PLAYED` event: a headline
for the result and two or three named fans quoted on the manager. Quotes are
templates picked by a seed derived from the fixture and club (no LLM, same
fixture = same story). The mood band is `(sentiment + rating) / 2`: below 30
angry, below 50 worried, below 70 content, otherwise delighted; about one voice
in four sits a band away from the crowd. The story is filed under the club's
country when `club.clubs.country` matches a `world.countries` code or name,
otherwise it is world-wide.

## 5. Mandate negotiation (manager-proposed targets)

`POST /api/managers/me/board/mandates/:id/negotiate` with `{target_value}`.

- **Bound by window:** `league_finish` proposals must stay within
  `|proposal − current| ≤ 3` places (`NegotiationFinishPlaces`) and
  `1..24`; `points_target` within `|Δ| ≤ 8` points
  (`NegotiationPointsPoints`) and `10..95`. Outside the window ⇒ `400`
  `ErrMandateValueInvalid`.
- **Bounded by persona**: a proposal that *worsens* the target (higher finish
  position / lower points) is accepted only within the persona's tolerance (§2);
  beyond it ⇒ `409` `ErrNegotiationRejected`. Same-or-better proposals always
  accepted.
- Only `league_finish` / `points_target` are negotiable; strategic/financial
  mandates ⇒ `400`. Unknown mandate ⇒ `404`; another manager's ⇒ `403`.
- Acceptance sets the new `target_value`, flips the mandate to `agreed`, and
  emits `BOARD_MANDATE_NEGOTIATED` (actor `manager`).

## 6. Events

Actor types: `system` (board review) / `manager` (negotiation).

| Event | Actor | Notes |
| --- | --- | --- |
| `BOARD_REVIEWED` | system | explanation `board_confidence` (factors sum to total) |
| `BOARD_MANDATE_MET` / `BOARD_MANDATE_BROKEN` | system | one per resolved mandate |
| `BOARD_MANDATE_NEGOTIATED` | manager | accepted proposal |
| `MANAGER_SACKED` | (manager pkg) | sacking guard, actor = board via engine; explanation `board_confidence` |

## 7. HTTP surface

- `GET /api/managers/me/board` → `BoardView`: {`confidence`, `snapshot`,
  `explanation`, `mandates`}. The current-tick snapshot is refreshed
  idempotently on read if missing.
- `POST /api/managers/me/board/mandates/:id/negotiate` → `NegotiationResult`.

Error mapping in `boardStatus`: not employed `404`; not your club / wrong world
`403` (world-mismatch `409` via the router's conflict mapping where used);
unknown mandate / target type `404`; non-negotiable `400`; out-of-window `400`;
beyond tolerance `409`.

## 8. Migrations

`0039_board_integrity` adds **no tables** — the board schema already exists
(migration 0005). It adds the partial unique index
`uq_board_mandate_open_per_category` on `club.board_mandates(club_id,
manager_id, season, category) WHERE status IN ('pending','agreed')`, so a
(club, manager, season) can never hold two open mandates of the same category
(negotiation cannot be double-booked against the generated set).

## 9. Known MVP limits (deferred)

- **dna_alignment** resolves only strategic/financial promise-keeping; richer
  DNA adherence curves (style-of-play, player-profile preference) land with
  S10-03.
- **Supporters** are a single bloc; match results are the sentiment driver
  (§4a, IM33). The seeded `loyality`, `financial_sensitivity` and
  `rivalry_intensity_base` columns are not read by any scoring path yet. The
  one other mover is the academy shutdown, which applies an immediate
  `ShutdownSentimentPenalty` (15) and floors at the same 15 minimum.
- **Alternatives** uses world reputation only; candidate pool modelling (past
  success, wage expectations) lands with the S07 hiring flow (OPD-09).
- A review runs at most once per `(manager_id, world_tick)`, triggered either by
  the month-boundary `Board.Review` or lazily when the manager opens
  `GET /api/manager/me/board` (which refreshes the current tick's snapshot so the
  board page is never stale). A manager who checks the board daily therefore
  blends sentiment more often than one who never opens it, so the displayed
  figure is path-dependent; a live pipe to the frontend follows the S07-01
  realtime replay work.