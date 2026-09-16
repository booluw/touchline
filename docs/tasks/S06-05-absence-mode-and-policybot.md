# S06-05 — Implement absence mode delegation and PolicyBot fallback execution

**Status:** Implemented  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §§45–47, 71–73; technical plan §10, §16; OPENCODE.md  
**Depends on:** S05-01, S06-01

## What to do

Implement manager delegation policies (`Policy` rows in `manager` schema) for absence mode and deadline resolution. Unify human commands and automated actions through single command handlers (`SelectSquad`, `RespondToBid`, `SetTrainingFocus`). When a manager misses an action deadline (e.g. pre-match line-up submission or bid expiry window), execute the handler via `PolicyBot` as the `ActorID`. Extend this mechanism to drive unmanaged AI-controlled clubs using personality-weighted policy parameters.

## Acceptance criteria

- Managers can configure standing delegation policies for squad selection (e.g. "Pick best fitness", "Rotate for cup"), bid responses (e.g. "Reject under 120% valuation"), and training routines.
- Scheduled pre-match or event deadlines check for human input; if absent, `PolicyBot` evaluates the manager's saved `Policy` and invokes the exact same command handler a human would call.
- All PolicyBot actions emit auditable events explicitly tagged with `ActorID = PolicyBot` for transparency and auditability.
- AI-controlled clubs use the PolicyBot architecture parameterized by club DNA and board expectations, eliminating duplicate simulation code.
- Managers receive clear summary logs upon returning from absence mode detailing all automated actions taken on their behalf.

## Delivery evidence

- **Migration `0042`** (`manager`): `manager.managers` gains `away_since`/`away_auto`/`consecutive_missed`/`last_activity_at`; `manager.policies` table (world-scoped, per-manager, per-decision-type params JSONB, unique on `(manager_id, policy_type)`); partial unique `uq_managers_world_policy_bot` (one club-less bot per world); `club.club_lineups` gains `updated_at`.
- **`internal/policybot` build-out** (7 packages touched):
  - `model.go`: `PolicyType` (squad/transfer/training), `Rule` (best_eleven/rotate/best_fitness), `SquadPolicy`/`TransferPolicy`/`TrainingPolicy` structs, `AbsenceState` (IsAway/AwayAuto helpers), defaults (`SellFloorPct=100`, `AcceptAbovePct=120`, `AutoCounter=true`), `MissedFixtureThreshold=3`, `HeartbeatInterval=1h`, event/error constants.
  - `store.go`: Policy CRUD; `LoadAbsence`→`AbsenceState`, `SetExplicitAway`, `TouchActivity` (throttled via `make_interval(hours => $2)`), `AttendOrMiss` (attended = `last_activity_at >= prevClubFixture.scheduled_at`; 3 consecutive → `away_since=now(), away_auto=TRUE`), `GetOrCreateAbsenceBot`, `LoadFixture`→`FixtureSnapshot`, `PrevClubKickoff`/`NextClubFixture`, `ManagerForClub`, `ClubsWithAwayManagers`, `PendingSellerBids`, `PlayerAttrs` (canonical transfer valuation query).
  - `resolver.go`: `ResolveSquad` (best_eleven → `SelectStartersWithLineup`; rotate → `SelectStartersRotated`; best_fitness → `SelectStartersByFitness`; tactics style → `AllowedFormations(style)[0]`), `TrainingArchetype` (competitive_ambition mapping), `ResolveTransfer` (accept ≥120%V, reject <100%V, else counter at accept-above threshold; asking price raises the counter, never lowers it).
  - `service.go`: `NewService(pool, bus, squadStore, tacticsSvc, trainingSvc, transferSvc)`, `ensureAway` (change transfer to bot), `ensureMatchInputs` (idempotent), `EnsureTraining` (weekly hook), `RespondToBidsForAbsent` (daily hook), public policy/absence CRUD, `emit`/`emitAbsence` via `eventbus.EventBus`.
  - `resolver_test.go`: Unit tests for `ResolveTransfer` (accept, reject, counter, auto-counter-off, asking-price raises counter).
  - `policybot_integration_test.go` (build-tag `integration`, compile-checked): policy CRUD, SetAway oracle, EnsureMatchInputs lineup-written, EnsureTraining archetype-submitted, AttendOrMiss auto-away streak, RespondToBidsForAbsent bid-status change.
- **Club-scoped cores extracted** (bots have no club, `requireOwnership`/`actorClub` skipped): `tactics.SetLineupForClub`/`SetTacticsForClub` (over `validateLineupSlots`+shared core), `training.SubmitPlanForClub` (over `submitPlan` core), `transfer.RespondToBidForClub` (4-return capture over `respondToBid` core).
- **Seam**: `match.AbsenceDever` interface defined in `match` package; policybot queries `match.fixtures` via pool (no policybot→match import). Hook called at top of `PlayFixture` (before Begin) and in `KickoffMatchday` loop (before `kickoffFixture`).
- **HTTP surface** (documented in `openapi.yaml`, docs-coverage green, 69 operations): `GET/PUT/DELETE /api/managers/me/policies/:type`, `GET/PUT/DELETE /api/managers/me/absence`, `GET /api/managers/me/absence-summary` — handlers in `httpapi/policy_handlers.go`, routes in `router.go`.
- **Heartbeat**: folded into `requireAuth` middleware (`TouchActivity`, throttled, nil-guarded so it degrades cleanly when PolicySvc is absent).
- **Wiring** (`internal/app/app.go`): `Policy *policybot.Service` on App; daily tick: `Policy.RespondToBidsForAbsent` → `Transfers.DailyTick`; weekly tick: `Policy.EnsureTraining` → `Training.ApplyWeekly`. `httpapi.Options.Policy` wired to server.
- **Build**: `go build ./...`, `go vet ./...`, `go vet -tags integration ./...`, gofmt on all touched files — all green. `go test ./...` — 19 packages pass, zero failures.
- **Docs**: `docs/design/policybot-numerics.md` (source of truth for attendance thresholds, heartbeat cadence, transfer math, training archetype DNA mapping); `docs/tasks/S06-05-absence-mode-and-policybot.md` flipped to Implemented.

## Notes

- Attendance is heartbeat-only (`last_activity_at`); `club.club_lineups.updated_at` exists in schema but is not used for attendance, avoiding the write-creates-attendance anti-pattern when the bot itself submits the lineup.
- One deviance from the original plan: `EnsureTraining(ctx, worldID)` has no `weekTick` parameter; the training slice already determines the current week internally.
- The bot never overrides an active training plan (`GetPlan` returns `ClubID == uuid.Nil` → no active plan → bot writes).
- Transfer counter fixes the offer at the accept threshold (or the counterpart's asking price when supplied); the initial bid fee is ignored as a midpoint anchor, matching the documented design in `policybot-numerics.md` §5.1.