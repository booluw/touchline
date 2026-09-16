# S08-01 — Implement academy investment tiers and procedural youth intake

**Status:** Implemented  
**Sprint:** 08 — Academies and player development  
**Source:** PRD §23; technical plan §16; OPENCODE.md  
**Depends on:** S01-04, S05-02, S07-04

## What to do

Build the youth academy investment model (`club.academies`, `club.facilities`). Implement configurable annual investment tiers, maintenance overhead costs appended to `finance.ledger_entries`, and annual procedural youth intake generation. Parameterize `pkg/playergen` by region to produce nationally plausible youth prospects matching club academy location. Support tactical academy management options including shutting down academies for emergency cash relief or expanding investment for higher prospect potential distribution.

## Delivery evidence

- **`pkg/playergen`**: `TalentClass` enum + `rollTalent` (tier-shifted odds, per-world+club+season deterministic RNG), `TalentOddsByTier`, `rollPotentialFloor`/`rollGenerationalFloor` (talent floors potential — high-ceiling prospects cannot roll journeyman), `rollTalentProfile` returning `(class, qualityOffset)`, `BadgeTier`/`role` handling for intake badges. Tests in `pkg/playergen/talent_test.go` + `attributes_test.go` (determinism, odds sums, floor-vs-journeyman band gap, bonus ordering) — all green.
- **`internal/squad`**: canonical `PositionalOverall` (cap 99), `OverallDelta` (signed, unclamped, zero-denominator safe), `HeadlineKeyForPosition`/`HeadlineDeltas` (category-collapsed keys for morale-bearing surface) in `internal/squad/overall.go` + overall_test.go — green.
- **`internal/academy`**: `model.go` (tier tables, annual cost, youth contract term/wage, `PotentialFloor`, sentiment hit), `store.go` (load/store/upsert + migration-aware rows), `service.go` (`EnsureAcademies`, `IntakeForWorld`/`IntakeForCountry`/`IntakeForClub` with deterministic intake keys + in-season idempotent cohort), `model_test.go` — green; HTTP surface in `internal/httpapi/academy_handlers.go` + covered in openapi + docs-coverage green.
- **Finance**: `internal/finance/academy.go` `SignAcademyProspects` (3-season youth contract + `finance.wage_commitments` twin, `YouthWeeklyWage` tier-scaling), `internal/finance/academy_ledger.go` maintenance debit (annual/12, per-tick idempotency key, upgrade-only costs); tests green.
- **Training/S08 weekly deltas**: `migrations/0043_player_attribute_changes` (+0044 youth-contract twin), `internal/training/deltas.go` `recordDeltas`/`upsertDelta`/`deltaSummary` incl. moral pseudo-key weekly swings (`morale`), pinned in `applyClubWeekly`; deltas_test.go green.
- **Seasonal hook**: `SEASON_COMPLETED` payload now carries `country_id` (competition rollover); `internal/app` wires `dailyAcademyMaintenance` (WORLD_TICK monthly debit, idempotent per world+tick) + seasonal intake on SEASON_COMPLETED and tick fallback for league-less worlds.
- **Read models / canonical OVR**: `PersistGeneratedPlayer` exported from `internal/playerpool` and free-agent listing + club roster surface OVR through the canonical `squad.PositionalOverall` read (academy prospects and first-team agree on one numerics surface). Dashboard hints cite the same canonical numbers.
- **Docs**: `docs/design/academy-numerics.md` (tiers, wages, numerics, read-model canonical-OVR contract) — the source of truth mirroring the other design docs; task flips S08-01/A04/A05.

## Acceptance criteria

- `club.academies` tracks investment tier, facility level, regional location, and annual operational costs.
- Annual youth intake events trigger `pkg/playergen` parameterized by club region, instantiating a cohort of 15-18 year-old youth players in `player.players`.
- Academy maintenance expenses are debited as recurring append-only entries in `finance.ledger_entries`.
- Managers can upgrade, downgrade, or shut down academy operations; shutting down yields immediate budget savings but emits negative board/supporter explanation events.
- Higher investment tiers probabilistically skew initial attribute baselines and ceiling potential for generated prospects.

## Delivery evidence

- Pending.
