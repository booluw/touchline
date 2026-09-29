# IM31 — One world day is one real day (fixed real-time clock)

**Status:** Not started
**Owner:** unassigned
**Sprint:** Improvements (world clock)
**Source:** Product owner decision (2026-09-29): "the game should run on real
days — every real day equals one world day." Resolves OPD-53 and amends OPD-42
(IM16), which made the clock scale configurable through `tick.day_length`.
**Depends on:** IM16 fixed-scale clock (`internal/world/clock.go`), IM23
day-stamped ticks, IM25 world calendar date, IM26 bid TTL.

## What to do

Remove the configurable clock scale. The world clock always runs at one game
day per real day; `tick.day_length` stops being a setting. Everything that
already derives from the clock keeps working, but at a fixed 1:1 scale.

### 1. Clock (`backend/internal/world/clock.go`)

- Drop the `tick.day_length` read from `LoadScale`; the scale is always
  `DefaultDayLength` (24h). Simplify the API rather than carrying a constant
  parameter around:
  - `LoadScale(ctx, q, worldID) (playable bool, epoch time.Time, err error)`
  - `TargetDay(realNow, epoch) int64` = whole real days since `epoch`
    (UTC midnight of `COALESCE(launched_at, created_at)`).
  - `ScaleNow` becomes unnecessary (world now == real now); delete it and use
    `time.Now()` / an injected clock at call sites.
- Keep the epoch, the day counter (`current_day`), and IM16's bounded
  catch-up (`maxDaysPerFire = 7`) unchanged: after a pause or downtime the world
  still replays missed days, one stamped `WORLD_TICK` per day (IM23).

### 2. Config (`backend/internal/world/service.go`, `internal/httpapi`)

- Remove `"tick.day_length"` from the launch-time default config keys.
- `SetConfig` rejects `tick.day_length` with a new `ErrConfigKeyRetired`;
  `POST /api/admin/worlds/:id/config` maps it to **400**
  ("tick.day_length is retired: the world clock runs in real time").
- Migration `0057_retire_day_length` deletes existing
  `world.world_config` rows with `config_key = 'tick.day_length'`
  (down migration: no-op or re-insert 86400 — document which). Add the row to
  `backend/migrations/README.md`.
- Update `backend/internal/apidocs/openapi.yaml` (config endpoint: the retired
  key and its 400).

### 3. Call sites

| File | Change |
|---|---|
| `internal/scheduler/service.go` `FireTick` | `TargetDay(now, epoch)` with the new `LoadScale` |
| `internal/matchday/runner.go` `worldNow` | world now = the runner's clock (real time) |
| `internal/transfer/helpers.go` `bidTTL` | `BidTTLWorldDays × 24h`; no world lookup needed |
| `internal/world/service.go` defaults / docs | drop the key |

`grep -rn "day_length\|DayLength\|ScaleNow" backend` should end with only the
constant and the retired-key guard.

### 4. Tests: replace clock compression with an injected clock

Tests currently speed worlds up by setting `tick.day_length` (e.g.
`internal/matchday/runner_integration_test.go` sets 2-second days). Replace
that with an injectable clock instead of a config knob:

- Add a small clock seam (e.g. `type Clock func() time.Time`, default
  `time.Now`) with `WithClock(...)` options on `scheduler.Service`,
  `matchday.Runner`, and anywhere else that asks "what time is it in the
  world" (transfer TTL checks, `bidExpired`).
- Rewrite the compressed-scale tests to advance a fake clock (or backdate
  `launched_at`, as the scheduler tests already do with `backdateLaunch`).
- `internal/competition/admin_events_integration_test.go` uses
  `tick.day_length` only as an example key — switch it to
  `calendar.days_per_week`.
- Add tests: `SetConfig("tick.day_length")` → `ErrConfigKeyRetired`; the HTTP
  config endpoint returns 400; migration 0057 removes existing rows.

### 5. Promise deadlines and injury clocks (OPD-53)

No code change: player-promise deadlines, promise evaluation windows and
injury recovery already run on real time, which is now by definition world
time. Record this in the docs (§6).

### 6. Docs to update

- `docs/how-to/cadences-and-time.md` — the `tick.day_length` config row, the
  `worldNow` formula (now "world time is real UTC time"), and the "to run
  matchdays faster, lower `tick.day_length`" guidance (replace with: set the
  season kickoff date near today — IM11 — and use the staggered kickoff hours;
  tests use an injected clock).
- `docs/how-to/setup-and-launch.md` (§ around the `tick.day_length` curl
  examples and the troubleshooting row), `docs/how-to/seasons.md`
  (compression tips), `docs/how-to/glossary.md` (Cadence, Bid TTL),
  `docs/how-to/transfer-market.md` and `docs/design/transfer-numerics.md`
  (TTL is simply three days).
- `docs/tasks/improvements/IM16-fixed-scale-world-clock.md` — add a "Superseded
  in part by IM31" note; `IM25` / `IM26` — note the scale is now fixed.
- `docs/product_manager.md` — OPD-42 annotated "scale fixed at 1:1 by IM31";
  OPD-53 resolved (already recorded as OPD-57 on 2026-09-29).

## Acceptance criteria

- A world's `worldNow` equals real UTC time; `current_day` advances once per
  real day (catching up after pauses as before).
- `tick.day_length` cannot be set (400) and no longer exists in any world's
  config after migration 0057.
- Bid TTL is three real days.
- No test relies on clock compression; the compressed-scale tests pass using an
  injected clock.
- Verification (from `backend/`): `gofmt -l .`, `go build ./...`,
  `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`,
  `go test ./...`, and the integration suites of `internal/world`,
  `internal/scheduler`, `internal/matchday`, `internal/transfer`,
  `internal/competition`, `internal/httpapi` against a live Postgres.

## Recorded decisions

- **One world day = one real day, always** (product owner, 2026-09-29). No
  per-world or environment override in production code.
- **Dev/test speed comes from an injected clock, not a config knob.** Admins
  who want to see a match soon set an early season kickoff date (IM11).
- Promise and injury timers stay on real time — which now *is* world time
  (OPD-53 resolved).
