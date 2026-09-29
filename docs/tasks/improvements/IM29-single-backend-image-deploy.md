# IM29 — One backend image, one role per Helm chart

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (deployment)
**Source:** Doc-vs-code audit of `infra/` against OPENCODE.md (prod runs the
api / scheduler / worker subsystems in separate pods). The CI build matrix built
`infra/docker/Dockerfile.scheduler` and `Dockerfile.worker`, which do not exist
(the push-to-main image job fails), and the Helm charts passed no role, so every
pod would run the image default `touchline serve` (API + scheduler + worker).
**Depends on:** the single `cmd/touchline` binary (architecture restructure).

## What to do

Build one backend image (`ghcr.io/<owner>/touchline`) from `Dockerfile.api` and
select the role per chart with container `args`.

## Delivery evidence

- `.github/workflows/ci.yml` — `build-images` matrix is
  `{image: touchline, dockerfile: api}` + `{image: frontend, dockerfile: frontend}`;
  `file: infra/docker/Dockerfile.${{ matrix.dockerfile }}`.
- `infra/helm/{api,scheduler,worker}/values.yaml` — `image.repository:
  ghcr.io/touchline/touchline`; `args: ["touchline", "<role>"]`.
- `infra/helm/{api,scheduler,worker}/templates/deployment.yaml` — render
  `args` from values when set.
- `docker-compose.yml` is unchanged (it already runs `touchline serve`).

## Verification

Shared verification for IM23–IM30 (run from `backend/`):

- `gofmt -l .` clean; `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...` green.
- `go test ./...` green.
- **Integration suites were run against a local Postgres 16** (not only
  compile-checked): see the before/after table below.

Integration suites, `go test -p 1 -tags integration`, against a local
Postgres 16 (2026-09-29). **Baseline** = the branch before IM23–IM30 plus only
the three fixes needed for the suites to run to completion at all (IM30's
seeding column, empty-weekday loop and injury context query — without them
seeding fails everywhere and two packages hang until timeout). **After** = this
change. `TestHTTPInjuryReadAndRushReturn` was excluded from both: it still hangs
in both trees after its SQL is answered (open, see below).

| Package | Baseline pass / fail | After pass / fail | Newly passing | Newly failing |
|---|---|---|---|---|
| `cmd/ref-seed` | 6 / 0 | 6 / 0 | 0 | — |
| `internal/academy` | 9 / 0 | 9 / 0 | 0 | — |
| `internal/app` | 0 / 3 | 6 / 0 | 3 | — |
| `internal/auth` | 15 / 4 | 15 / 4 | 0 | — |
| `internal/board` | 13 / 3 | 13 / 3 | 0 | — |
| `internal/bootstrap` | 9 / 1 | 9 / 1 | 0 | — |
| `internal/competition` | 81 / 18 | 85 / 16 | 2 | — |
| `internal/dashboard` | 9 / 1 | 10 / 0 | 1 | — |
| `internal/development` | 11 / 0 | 11 / 0 | 0 | — |
| `internal/eventoutbox` | 2 / 0 | 2 / 0 | 0 | — |
| `internal/faction` | 21 / 0 | 21 / 0 | 0 | — |
| `internal/finance` | 11 / 0 | 11 / 0 | 0 | — |
| `internal/form` | 13 / 0 | 13 / 0 | 0 | — |
| `internal/httpapi` | 29 / 22 | 29 / 22 | 0 | — |
| `internal/injury` | 16 / 1 | 16 / 1 | 0 | — |
| `internal/lifecycle` | 11 / 1 | 12 / 1 | 0 | — |
| `internal/manager` | 4 / 1 | 4 / 1 | 0 | — |
| `internal/match` | 16 / 2 | 18 / 0 | 2 | — |
| `internal/matchday` | 4 / 2 | 4 / 2 | 0 | — |
| `internal/personality` | 2 / 0 | 2 / 0 | 0 | — |
| `internal/player` | 18 / 6 | 18 / 6 | 0 | — |
| `internal/playerpool` | 0 / 1 | 0 / 1 | 0 | — |
| `internal/policybot` | 7 / 4 | 7 / 4 | 0 | — |
| `internal/scheduler` | 8 / 0 | 8 / 0 | 0 | — |
| `internal/scout` | 0 / 1 | 0 / 1 | 0 | — |
| `internal/social` | 14 / 3 | 14 / 3 | 0 | — |
| `internal/squad` | 48 / 0 | 48 / 0 | 0 | — |
| `internal/tactics` | 6 / 0 | 6 / 0 | 0 | — |
| `internal/training` | 19 / 2 | 18 / 3 | 0 | `TestRecoveryDetrainsAfterThreeConsecutiveWeeks` — flaky, not a regression: it failed 5 of 6 isolated baseline runs and 1 of 3 after |
| `internal/transfer` | 13 / 8 | 23 / 0 | 8 | — |
| `internal/world` | 5 / 0 | 5 / 0 | 0 | — |
| `pkg/eventbus` | 8 / 0 | 8 / 0 | 0 | — |
| `pkg/explanation` | 8 / 0 | 8 / 0 | 0 | — |
| `pkg/jwt` | 7 / 0 | 7 / 0 | 0 | — |
| `pkg/matchsim` | 35 / 0 | 35 / 0 | 0 | — |
| `pkg/playergen` | 39 / 0 | 39 / 0 | 0 | — |
| `pkg/realtime` | 5 / 0 | 5 / 0 | 0 | — |
| **Total** | **522 / 84** | **545 / 69** | | |

New tests (all pass): `TestTickDay`, `TestDailyDispatchUsesStampedDayOnCatchUp`,
`TestSeasonalFallbackSkipsLeagueWorlds`, `TestLifecycleRetiresOncePerSeasonAcrossCountries`,
`TestAdminChangesRecordEvents`, `TestNextAllowedWeekdayEmptySetIsUnrestricted`,
`TestBidEventsCarryBothClubs`, `TestAIBidsFollowTheBidCommandRules`.

**Remaining failures are pre-existing** (identical in the baseline) and outside
this change's scope — e.g. manager login with no club scans a NULL club name
(`internal/auth`, A12), board mandate generation, bootstrap determinism, cup
qualification/regional sweep and membership tests in `internal/competition`,
22 `internal/httpapi` tests, player transfer-request and promise tests, and the
hanging `TestHTTPInjuryReadAndRushReturn`. They are recorded here so the next
task can pick them up; none was introduced by IM23–IM30.

Helm is not installed in the verification environment, so the chart templates
were reviewed by hand, not rendered.

## Recorded decisions

- **One image for every backend role**; the chart chooses the subsystem.
  Scheduler/worker keep their `/health` probe on `SCHEDULER_PORT` /
  `WORKER_PORT` (served by `touchline scheduler|worker`).
