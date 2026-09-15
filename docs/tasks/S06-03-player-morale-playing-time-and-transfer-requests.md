# S06-03 — Implement player morale, playing-time tracking, and transfer requests

**Status:** Implemented  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §§13–14; technical plan §§6, 16; OPENCODE.md  
**Depends on:** S05-01

## What to do

Implement player morale dynamics, playing-time tracking against agreed contract roles (Key Player, Squad Player, Youth, etc.), and unhappiness triggers. When player expectations are unmet over a rolling period, transition player state to unhappy, trigger manager interaction options (reassure, promise playing time, reject demand), and allow players to submit formal transfer requests. Morale changes must feed into match performance modifiers and emit detailed `Explanation` objects.

## Acceptance criteria

- `player.players` tracks current morale, happiness factors, agreed squad role, and rolling playing-time percentage.
- Match completion ticks evaluate actual playing time against expected role contracts; unfulfilled roles trigger morale degradation.
- Unhappy players generate notifications for the manager with explicit `Explanation` objects detailing the root cause (e.g. "Played 1 of last 5 matches despite Key Player contract").
- Managers can initiate basic player interactions (promise playing time, explain tactical benching, offer revised contract).
- Unresolved unhappiness leads to formal `PLAYER_TRANSFER_REQUESTED` events, placing the player on the transfer list and informing squad dressing room state.
- Low player morale applies deterministic negative attribute modifiers during match simulation calculations.

## Delivery evidence

- **Migration `0040`** (`player`/`social`): `player_condition.morale` + `playing_time_pct` + `transfer_request_cooldown_until`, `contracts.squad_role` (+ keyword backfill), `player.player_appearances`, `player.player_transfer_requests` (partial unique on one open `pending` per player), `social.relationship_events` + canonical `player↔manager` sentiment (partial unique `uq_relationship_player_manager`).
- **`internal/player` engine**: numerics constants + pure math (`numerics.go`), model rows, store layer, `morale.go` (match appearances → share refresh → alpha-weighted morale pull; squad + detail reads with `Explanation`), `weekly.go` (role backfill, recovery, promise grading kept/broken, deterministic transfer-request trigger for human-managed clubs, 21-day auto-list sweep), `transfer_requests.go` (approve → market listing, deny → morale drop + cooldown, reassure → 4-week promise, HTTP-facing by-player variants), `OnPlayerTransferred` fresh start (morale 0.85, share 0, open request withdrawn).
- **Hooks**: `match.service` `WithPlayers` — `Finalize` derives starters'/subs'/replaced-subs' minutes inside the completion tx and calls `RecordMatchAppearances`; `transfer.service` `PlayerLifecycle` — `acceptBid` (step 1b) calls `OnPlayerTransferred` right after the ownership flip.
- **Wiring**: `internal/app` constructs the player service after transfers, sets both hooks, adds `App.Players`, and runs `WeeklyTick` in the weekly pass (after training, before the board review). `internal/httpapi` gets `Options.Player`; the test server mirrors this wiring.
- **HTTP surface** (documented in `internal/apidocs/openapi.yaml`, docs-coverage green): `GET /api/clubs/:id/players`, `GET /api/clubs/:id/players/:playerID`, `POST …/promise-playing-time`, `POST …/transfer-request/approve`, `POST …/transfer-request/deny`.
- **Events**: `PLAYER_TRANSFER_REQUESTED`, `TRANSFER_REQUEST_APPROVED/DENIED/REASSURED/AUTO_LISTED` through the outbox with actor + `Explanation` payloads.
- **Tests**: `internal/player/numerics_test.go` (pure-math unit coverage); `internal/player/service_integration_test.go` (appearances/share/morale, weekly trigger, deny/approve, promises, fresh start — compile-checked, needs Postgres to run); `cmd/api/player_squad_integration_test.go` HTTP round-trip (read model, promise, deny 200, cross-club 403, unknown 404, anonymous 401).
- **Docs**: `docs/design/morale-numerics.md` (source of truth, proposal awaiting PM tune sign-off); `product_manager.md` OPD-03 morale row resolved; migrations README 0040 row.

## Notes

- "Played X of last 5 matches" notifications are not yet served: the weekly trigger emits an `Explanation` with the concrete share-vs-entitlement gap, and read surface (detail view) surfaces the reason live. Dressing-room state and in-match morale modifiers are explicitly deferred to S09-01 (morale feeds the pitch results only through the member experience, not through the simulation).
- Proposed numerics are interior-motor constants pending PM tuning sign-off (`docs/design/morale-numerics.md`); recalibration is data-only.