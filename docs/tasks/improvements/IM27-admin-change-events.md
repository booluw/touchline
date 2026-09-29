# IM27 — Admin configuration changes land on the event spine

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (event spine completeness)
**Source:** Doc-vs-code audit against the product invariant "all state changes
emit typed `world.events`" (Tech Plan §4, `docs/product_manager.md`) and OPD-23
(event + state in one transaction). These admin writes recorded nothing:
`world.SetConfig`, `CreateCountry`, `CreateLeague`, `UpdateLeagueAdjacency`,
`SetLeagueReputation`, `CreateRegion`, `DeleteRegion`, `SetCountryRegion`,
`CreateCup`, `CreateRegionalCup`, `SetQualification`, `SetCupFinalDate`.
**Depends on:** OPD-23 transactional outbox (`eventbus.WriteTx`).

## What to do

Record one typed event per admin change, in the same transaction as the change
(converting pool writes to transactions where needed).

## Delivery evidence

### Backend

- `backend/internal/competition/admin_events.go` (new) — event constants and
  `recordAdminEvent` (system actor, current tick, via `recordSeedEvent`).
- `competition/service.go` — `COUNTRY_CREATED`, `LEAGUE_CREATED`,
  `LEAGUE_ADJACENCY_SET`.
- `competition/regions.go` — `REGION_CREATED`, `REGION_DELETED`
  (`DELETE … RETURNING world_id, name`), `COUNTRY_REGION_SET`,
  `LEAGUE_REPUTATION_SET` (with `previous`).
- `competition/cup_read.go`, `competition/regional_cup.go` — `CUP_CREATED`
  (domestic / continental), `CUP_QUALIFICATION_SET`.
- `competition/cup_final_date.go` — `CUP_FINAL_DATE_POLICY_SET` (mode, date,
  offset, rounds moved).
- `world/service.go` — `SetConfig` runs in a tx and records
  `WORLD_CONFIG_CHANGED {key, value}`.

### Tests

- `backend/internal/competition/admin_events_integration_test.go` —
  `TestAdminChangesRecordEvents` asserts each event count and the config
  payload.

### Verification

See [IM29](IM29-single-backend-image-deploy.md#verification).

## Recorded decisions

- Admin endpoints carry no manager actor; these events use the `system` actor,
  like the existing admin competition events (`COMPETITION_SCHEDULE_UPDATED`).
- No new realtime fan-out and no news stories; these are audit/history events.
- **This amends OPD-30(4)** (IM06), which had recorded that region and
  reputation mutations "publish no events/news". The product-level invariant
  "all state changes emit typed `world.events`" takes precedence, so they now
  record events; the "no news" half of OPD-30(4) still stands. Recorded as
  OPD-52 in `docs/product_manager.md`, with OPD-30(4) annotated.
- **Confirmed by the product owner (2026-09-29):** all admin changes to
  regions and reputation are recorded as events.
