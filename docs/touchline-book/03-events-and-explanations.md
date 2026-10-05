# Chapter 3 — The event spine, outbox and explanations

Touchline's history, notifications, read models and audit trail all hang off
one table: **`world.events`**. This chapter explains that contract and the
companion contract every scored outcome follows: the **Explanation**.

## 3.1 `world.events` — the event spine

Every meaningful state change appends one row:

| Column (conceptual) | Meaning |
| --- | --- |
| `id`, `occurred_at` | identity (captured at insert with `RETURNING`) |
| `world_id` | scoping |
| `event_type` | e.g. `WORLD_TICK`, `SEASON_CREATED`, `MATCH_PLAYED`, `BID_ACCEPTED`, `BOARD_REVIEWED`, `MANAGER_SACKED` |
| `world_tick` | monotonic per-world counter for ordering/audit |
| actor | `system` / `manager` / `policy_bot` (+ actor id) |
| `payload` | typed JSON per event type |
| `explanation` | optional JSONB `Explanation` |
| `caused_by` / `related_event_id` | causal chain |

The event log is the replay source, the audit trail for anti-abuse, and the raw
material of history views ("history is a read model over events", Tech Plan
§4).

### Representative event catalogue

| Area | Events |
| --- | --- |
| Clock | `WORLD_TICK{granularity:"daily", day:N}` |
| Admin config (IM27) | `WORLD_CONFIG_CHANGED`, `COUNTRY_CREATED`, `LEAGUE_CREATED`, `LEAGUE_ADJACENCY_SET`, `LEAGUE_REPUTATION_SET`, `REGION_CREATED/DELETED`, `COUNTRY_REGION_SET`, `CUP_CREATED`, `CUP_QUALIFICATION_SET`, `CUP_FINAL_DATE_POLICY_SET` |
| World/bootstrap | `WORLD_BOOTSTRAPPED`, `CLUB_CREATED`, `COMPETITION_SEEDED`, `CLUB_JOINED_LEAGUE` |
| Seasons | `SEASON_CREATED`, `SEASON_STARTED`, `SEASON_COMPLETED`, `CLUB_PROMOTED`, `CLUB_RELEGATED`, `CUP_CAMPAIGN_STARTED` |
| Matches | `MATCH_PLAYED`, live kickoff/full-time, `RELATIONSHIP_CHANGED` |
| Squad | `LINEUP_SAVED`, `TACTIC_SET`, `TRAINING_PLAN_SET`, `TRAINING_WEEK`, `DEVELOPMENT_WEEK` |
| Players | `PLAYER_INJURED`, `PLAYER_RECOVERED`, `INJURY_UPDATE`, `PLAYER_RUSHED_RETURN`, `PLAYER_TRANSFER_REQUESTED`, `PLAYER_RETIRED`, `PLAYER_SIGNED`, `PLAYER_RELEASED`, `ACADEMY_INTAKE`, `COUNTRY_ACADEMY_INTAKE`, `AI_AUTO_FILL`, `SQUAD_UNREST_TRIGGERED` |
| Finance | `WAGE_POSTED`, `CONTRACT_COMMITTED` |
| Transfers | `PLAYER_LISTED`, `PLAYER_LISTING_WITHDRAWN`, `BID_PLACED`, `BID_COUNTERED`, `BID_ACCEPTED`, `BID_REJECTED`, `BID_WITHDRAWN`, `BID_EXPIRED`, `TRANSFER_COMPLETED`, `MARKET_VALUATIONS_REFRESHED` |
| Careers | `JOB_OFFER_ACCEPTED`, `MANAGER_SACKED`, resignation event, `MANAGER_AWAY_EXPLICIT`, `MANAGER_AWAY_CLEARED`, `MANAGER_AUTO_AWAY` |
| Board | `BOARD_REVIEWED`, `BOARD_MANDATE_MET`, `BOARD_MANDATE_BROKEN`, `BOARD_MANDATE_NEGOTIATED` |

## 3.2 The transactional outbox (OPD-23)

**Rule:** an event and the business write it describes commit in the **same
transaction**, together with the River dispatch job.

```go
// pkg/eventbus
bus.PublishTx(ctx, tx, ev)   // inserts world.events row + touchline_event River job in tx
bus.Publish(ctx, ev)         // = BEGIN → PublishTx → COMMIT
```

Consequences:

1. A committed state change with an undispatched event is impossible by
   construction.
2. **No silent fan-out.** A failed event insert/enqueue aborts the enclosing
   transaction; publish errors always propagate.
3. Identity is captured at record time (`RETURNING id, occurred_at`), so causal
   chains reference the persisted event.

This replaced an earlier design where producers committed state, then published
in a second transaction — a crash in between produced state with no
consequences (see `docs/em/event-delivery-atomicity.md` for the history).

### At-least-once delivery and idempotency

River delivers each job **at least once**. Every handler must therefore be
idempotent. The codebase uses four standard techniques — reuse them:

| Technique | Example |
| --- | --- |
| **Dedup key + `ON CONFLICT DO NOTHING`** | wage postings `wage:<tick>:<contract>`; academy maintenance `academy:maintenance:<club>:<tick>`; transfer ledger `transfer:<completionID>:buyer\|seller` |
| **Natural unique key** | `manager.match_ratings (fixture_id, club_id)`; `job_security_snapshots (manager_id, world_tick)`; `injury_setbacks (injury_id, week)` |
| **Status guard** | kickoff only fixtures still `scheduled`; season activation flips each season once |
| **`FOR UPDATE` relocks** | bid acceptance relocks the bid and re-validates |

### Outbox repair

`internal/eventoutbox.Sweep` re-enqueues an event row that has no River job.
Caveat recorded in `docs/em/outbox-repair-redelivery-retention.md`: River
deletes completed jobs after its retention period, so "no job row" must not be
the only proof an event was never delivered. Treat any change to the sweep or to
River retention as high-risk ([Chapter 30](30-roadmap-and-open-decisions.md)).

### Realtime is never authoritative

After commit, some handlers push a best-effort realtime envelope (Redis →
`/ws`). A push failure is logged and never rolls back or blocks gameplay
(IM23). The REST read is always the source of truth.

## 3.3 Explanations (OPD-12)

Every scored outcome carries a structured "why".

```json
{
  "subject": "board_confidence",
  "score": 47,
  "factors": [
    { "label": "league performance", "delta": 12 },
    { "label": "expectations",       "delta": 6 }
  ]
}
```

Rules:

- Lives in `pkg/explanation`. Wire shape is fixed; changes are additive only.
  All fields are always emitted.
- **Factors are the authoritative reason.** They are *not required* to sum to
  `score` (narrative explanations — e.g. an AI bid rationale or a weekly
  development note — carry score 0). An opt-in `Validate()` checks the sum for
  producers that promise it.
- Producers that **do** promise exact sums (tested): `board_confidence` (factors
  sum to the weighted total), `monthly_wages` (sum to the wage bill),
  `transfer_value` (market value + premium = agreed fee, IM26), the finance
  summary `factors` (sum to cash).
- Explanations persist with their event (`world.events.explanation`) and are
  rendered by clients — **never recalculated**.
- State-changing endpoints return the `Explanation` directly in the response.

## 3.4 Determinism and seeds

Randomness that affects competition or money is seeded:

| Seed | Derivation | Used by |
| --- | --- | --- |
| World seed | crypto-random `int64` minted at first seed (`world.worlds.random_seed`) | everything below |
| League names | `seed ⊕ leagueID` | club name scrambling |
| Fixtures | master `*rand.Rand` from `WORLD_BOOTSTRAPPED.random_seed` | seeding, fixture lists, kickoff rotation |
| Cup draws | `world_seed ⊕ cup_id ⊕ round` | brackets, round dates |
| Match | per-fixture seed → per-club seed | matchsim + orchestration draws |
| Injury | `fnv64a("injury-match:"+seed+":"+player)`; `WeekStream(week, player, "training")`; `SetbackStream(injury, week)` | injury engine |
| Intake | `world + club + season` (+ country for street) | academy |
| Dressing room | `PairStream(world_seed, a, b)` | relationship generation |
| AI bids | `fnv64(listingID) ^ (7919 × worldTick)` | transfer AI |

**Stream separation** is a hard rule: a new subsystem gets its own derived
stream so it never perturbs the match engine's canonical draw order (the golden
replay digest).

## Connections

- The clock event that drives everything: [Chapter 4](04-world-clock-and-time.md).
- Explanation consumers: board ([23](23-board-and-job-security.md)), finance ([20](20-finance.md)), transfers ([21](21-transfer-market.md)), development ([13](13-training-and-development.md)), injuries ([14](14-condition-and-injuries.md)).
- Source: OPD-12, OPD-23, `docs/em/*.md`, Tech Plan §§4, 8.

---
[← Architecture](02-architecture.md) · [Contents](the-touchline-book.md) · [Next: World clock →](04-world-clock-and-time.md)
