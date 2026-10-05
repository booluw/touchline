# Chapter 23 — The board: match ratings, confidence, mandates and sacking

The board decides whether a manager keeps the job. It works on **two
cadences**:

| Cadence | What happens | Code |
| --- | --- | --- |
| **After every completed match** | each manager gets a 0–100 **match rating**; supporter sentiment moves; a fan-reaction story is published (human clubs) | `board.RecordCompletedMatch` |
| **Every month boundary** (default world day 30, 60, …) — and on demand when the manager opens the board page | the **board review**: mandates graded, seven factors scored, **confidence** computed and snapshotted, sacking guard applied | `board.Service.Review` / `BoardView` → `reviewManager` |

> **Quick answer — "how often is the manager's performance rating
> calculated?"** The per-match rating is calculated **after every match**. The
> board's *Performance factor* — the mean of the manager's **last 8** match
> ratings at the club — and the overall **confidence** are recalculated at
> **each month boundary** (`calendar.days_per_month`, default every 30 world
> days, IM02), and additionally whenever the manager opens
> `GET /api/managers/me/board` (at most once per world tick). Confidence never
> moves per match (OPD-58).

## 23.1 Board personas

Seeded from the club archetype ([Ch. 9](09-clubs-dna-supporters.md)). Each row
weights the seven factors (sums to 1.0) in the order *performance,
expectations, financial, board relationship, club-DNA alignment, supporter
sentiment, alternatives*:

| Persona | Weights | Negotiation tolerance |
| --- | --- | --- |
| `patient_owner` | .18 .12 .18 .18 .14 .10 .10 | 3 |
| `demanding_owner` | .30 .25 .12 .10 .10 .08 .05 | 0 |
| `financial_conservative` | .18 .12 .30 .12 .10 .08 .10 | 2 |
| `academy_owner` | .12 .10 .12 .12 .32 .10 .12 | 2 |
| `prestige_owner` | .28 .22 .10 .10 .12 .12 .06 | 0 |
| `political_board` (fallback) | .18 .18 .14 .22 .08 .12 .08 | 1 |

## 23.2 Expectations: the mandate set

Four mandates per `(club, manager, season)`, created lazily on first board view
(insert `ON CONFLICT DO NOTHING`; `uq_board_mandate_open_per_category`,
migration 0039):

```
expectedFinish = clamp(round((110 − ambition) / 8  [+1.5 if patience ≥ 70]), 1, 24)
expectedPoints = clamp(round(88 − 3.5 × expectedFinish), 10, 95)
```

| Category | `target_type` | Target | Graded against |
| --- | --- | --- | --- |
| primary | `league_finish` | expectedFinish | league position |
| secondary | `points_target` | expectedPoints | league points |
| strategic | `operating_balance` | 0 (break-even) | operating profit |
| financial | `wage_structure` | 0 (budget) | committed wages vs wage budget |

States: `pending → agreed` (after negotiation) `→ met | broken`. Only open
mandates are graded.

### Grading rules (each review)

Progress = played / total league fixtures (0–1).

- **league_finish** — slack shrinks: 3 (< 40%), 2 (< 80%), 1 (≥ 80%), 0
  (complete). Met when `position ≤ target + slack`; broken mid-season when
  `position > target + 6`; at season end strict (`position > target`). No
  standings → neutral.
- **points_target** — scaled target `round(target × progress)`. Met when
  points ≥ scaled; broken when points ≤ scaled − 10 or any shortfall at season
  end.
- **wage_structure** — allowance `budget × (1 + targetPP/100 + 5%)`; within →
  met, else broken. No budget → ungraded.
- **operating_balance** — profit ≥ 0 → met; a loss beyond ¼ of the wage budget
  → broken; small front-loaded losses stay open.

## 23.3 Per-match rating (IM33, OPD-58)

Inside the match-completion transaction, for each side with a manager (bots
are rated too; vacant seats skipped):

```
expected = clamp(0.5 + 0.004 × (ownRep − oppRep) ± 0.08 (home + / away −), 0.10, 0.90)
actual   = 1 win · 0.5 draw · 0 loss
rating   = clamp( round(50 + 60 × (actual − expected)) + 5 × clamp(GD, −3, 3), 0, 100 )
```

Stored in `manager.match_ratings (fixture_id, club_id)` as `board_rating` with
`sentiment_before/after` — the key makes a replayed completion a no-op.

Then **supporter sentiment** moves toward the rating
(`α = 0.08`, ×2 in a rivalry game, clamp 15–95; [Ch. 9](09-clubs-dna-supporters.md)),
and for **human-managed** clubs a `fan_reaction` news story is published:
headline + 2–3 named fans, mood band from `(sentiment + rating) / 2` (< 30
angry, < 50 worried, < 70 content, else delighted; ~1 in 4 voices a band off),
deterministic per fixture and club, filed under the club's country when
`club.clubs.country` matches a `world.countries` code or name, else world-wide.

Example: a 70-rep club at home vs a 60-rep club → expected
`0.5 + 0.04 + 0.08 = 0.62`. A 2–0 win rates `50 + 60 × 0.38 + 10 ≈ 83`; a 0–0
draw `50 − 7 ≈ 43`; a 0–1 loss `50 − 37 − 5 ≈ 8`.

## 23.4 The monthly review

`Board.Review` runs last in the month-boundary pass (after wages and academy
costs, [Ch. 4](04-world-clock-and-time.md)), for every active manager with a
club, each in its own transaction, idempotent per `(manager_id, world_tick)`:

1. Load inputs (standings, season progress, finances, mandates, last 8 match
   ratings **at this club**, DNA, sentiment, world reputation).
2. **Grade** open mandates (§23.2); emit `BOARD_MANDATE_MET` / `_BROKEN`.
3. **Score** the seven factors (§23.5).
4. **Weighted total** = `Σ round(wᵢ × factorᵢ)`, clamped 0–100. Because each
   explanation factor is that rounded contribution, the factors **sum exactly**
   to the total (tested).
5. **Snapshot** into `manager.job_security_snapshots` and emit
   `BOARD_REVIEWED` with the `board_confidence` explanation.
6. **Sacking guard** (after commit): if total ≤ **25**
   (`SackThresholdTotal`) **and** the manager is human, `manager.Sack` runs —
   club back to AI, `MANAGER_SACKED` with the explanation, reputation −10,
   immediate re-offer ([Ch. 22](22-managers-and-job-offers.md)). AI-managed
   clubs are scored identically but never sacked.

### On-demand review via the board page

`GET /api/managers/me/board` (`BoardView`) calls the **same `reviewManager`**
for the current world tick before returning, so the page is never stale. Since
the snapshot is unique per `(manager, world_tick)` this runs at most once per
tick per manager. Note the consequence: **opening the board page grades
mandates and applies the sacking guard** exactly like the monthly pass — a
manager at ≤ 25 can be sacked mid-month by viewing it. Treat this as a known
behaviour to revisit ([Ch. 30](30-roadmap-and-open-decisions.md)).

## 23.5 The seven factors (0–100 each)

| Factor | Formula |
| --- | --- |
| **performance** | mean of the last **8** match ratings at this club (`MatchRatingWindow`); with none, `clamp(50 + 8 × (expectedFinish − position), 0, 100)`; no league → 50 |
| **expectations** | `clamp(50 + 10 × met − 15 × broken, 0, 100)` over this season's resolved mandates |
| **financial** | `50 + round(40 × (1 − wageRatio)) ± 10` (profit/loss); wageRatio = committed/budget capped at 1; no budget → 60 (50 with committed wages) |
| **board_relationship** | `clamp(50 + (patience − 50)/5 + 25 × (met − broken)/resolved, 0, 100)` |
| **club_dna_alignment** | `clamp(50 + 15·met_strategic + 15·met_financial − 25·broken_strategic − 25·broken_financial, 0, 100)` |
| **supporter_sentiment** | stored `club.supporter_groups.current_sentiment` (read-only here) |
| **alternatives** | `clamp(50 + 5 × worldReputation, 10, 90)` — manager career reputation as replacement-pressure proxy |

Worked example — a `demanding_owner` club, factors performance 40,
expectations 35, financial 70, relationship 45, DNA 50, sentiment 48,
alternatives 75:

```
.30·40 + .25·35 + .12·70 + .10·45 + .10·50 + .08·48 + .05·75
= 12 + 9 + 8 + 5 (4.5→5) + 5 + 4 + 4 (3.75→4) = 47      → safe (> 25)
```

## 23.6 Negotiating targets

`POST /api/managers/me/board/mandates/:id/negotiate {target_value}`:

- only `league_finish` (± 3 places, 1–24) and `points_target` (± 8 points,
  10–95) are negotiable; out of window → `400`;
- a **worsening** proposal is accepted only within the persona's tolerance,
  else `409 ErrNegotiationRejected`; same-or-better always accepted;
- success sets the target, status `agreed`, emits `BOARD_MANDATE_NEGOTIATED`.

## 23.7 Reads and dashboard

- `GET /api/managers/me/board` → `{confidence, snapshot, explanation, mandates}`.
- Dashboard: **Urgent → board** when the latest total ≤ 25; **Important →
  board** on a drop of 15+ since the previous review
  ([Ch. 26](26-dashboard-news-scouting-realtime.md)).
- Error mapping: not employed 404; not your club / wrong world 403; unknown
  mandate 404; non-negotiable 400; out of window 400; beyond tolerance 409.

## 23.8 Known limits

- DNA alignment covers only strategic/financial promise-keeping; style-of-play
  and recruitment adherence wait for S10-03.
- Supporters are one bloc; `loyalty`, `financial_sensitivity`,
  `rivalry_intensity_base` aren't read yet.
- Alternatives ignore a real candidate pool (OPD-09).
- PRD sack triggers beyond confidence (cultural, behaviour) are not modelled.

## Connections

- Inputs: [9](09-clubs-dna-supporters.md) (DNA, sentiment), [20](20-finance.md) (finance), [6](06-leagues-and-scheduling.md) (standings), [22](22-managers-and-job-offers.md) (reputation, sack mechanics).
- Code: `internal/board/{numerics,evaluate,match,fannews,service,store}.go`.
- Source: `docs/design/board-numerics.md`, `docs/how-to/cadences-and-time.md` §6, OPD-03 (board), OPD-47, OPD-58, IM02, IM21, IM33.

---
[← Managers](22-managers-and-job-offers.md) · [Contents](the-touchline-book.md) · [Next: Social →](24-social-and-rivalries.md)
