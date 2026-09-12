# S02-02 — Implement world lifecycle and manager-club assignment boundaries

**Status:** Done  
**Owner:** opencode agent
**Sprint:** 02 — Authenticated, schedulable worlds  
**Source:** OPENCODE.md; technical plan §18; PRD §§5, 42–43, 74  
**Depends on:** S01-01, S02-01

## What to do

Implement the platform constraints for multiple worlds and one active club per manager. Expose only the lifecycle and assignment capabilities supported by a product-approved world-bootstrap design.

## Acceptance criteria

- A world can be created and independently identified; all subsequent operations scope data to that world.
- A manager cannot hold more than one active club assignment anywhere on the platform.
- Resignation/sacking can end an active assignment without deleting career history.
- Global manager reputation history is append-only/displayable, while any operational reputation calculation is world-scoped.
- World selection, initial club assignment, and initial league composition use an approved decision; implementation does not infer missing rules.

## Delivery evidence

- **Contract decisions** recorded as **OPD-16** in `docs/product_manager.md` (world lifecycle = admin surfaces; a first club = a job offer from an AI club, accepted/declined by the manager; one active assignment per manager platform-wide; events + append-only reputation at assignment borders; OPD-01 league composition deferred to S03-01). OPD-01 row updated to note the deferral.
- **AC1 (world created + scoped)**: migration `0024_manager_lifecycle_contract`; `internal/world/service.go` (`CreateWorld`/`GetWorld`/`ListWorlds`/`SetStatus`, `provisioning → active/open_beta/paused/archived`, archived terminal, `WORLD_*` events, `world.world_config` cadence seed on launch) + `cmd/api/lifecycle_handlers.go` (`POST /api/admin/worlds{,/:id/status}` behind `requireAdmin`). Integration + HTTP tests cover create/list/transitions/duplicate-name/config-seed/event-log/404.
- **AC2 (one active assignment)**: `uq_manager_one_active_person` partial index + `uq_manager_one_active_club` + `chk_managers_club_only_when_active` in migration 0024; `internal/manager/service.go` `AcceptJobOffer` stands down the incumbent AI manager before assigning; `Resign`/`Sack` clear the club. Tests: `TestJobOfferFullLifecycle`, `TestAcceptJobOffer_AlreadyEmployedRejected`.
- **AC3 (resign/sack without deleting history)**: `manager.manager_history` rows open on accept, close with `end_date` + `outcome_summary` on resign/sack — never deleted; `MANAGER_RESIGNED`/`MANAGER_SACKED` events. HTTP `POST /api/managers/me/resign` verified in tests + live smoke.
- **AC4 (rep boundary)**: `career` category added to `manager_reputation_events` CHECK; deltas `+5` accept / `-10` sack appended transactionally; `GetReputation`/`ListCareerHistory` global display reads, `WorldReputationTotal` world-scoped. Tests assert history rows and deltas.
- **AC5 (approved decisions, no inferred rules)**: OPD-16(6) — no league numbers/regional formats invented; OPD-15 governs world selection and logins; OPD-02 covers no-signup. Job-offer flow: `POST /api/admin/offers` → `GET /api/managers/me/offers` → `accept`/`decline` (`cmd/api` + `internal/manager`).
- **Verification**: integration suite green on local Postgres (`go test -p 1 -tags integration -race ./pkg/eventbus/... ./internal/auth/... ./internal/world/... ./internal/manager/... ./cmd/api/...`); `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...` all pass; CI (.github/workflows/ci.yml) integration job updated to include world/manager and passes config validation. Full live API smoke on a local cluster: admin create+launch world (archived terminal), config seeded, AI-club offer → accept (bot stood down, club handed over, history opened, +5 reputation) → resign (history closed, AI control restored) with double-resign 409.
