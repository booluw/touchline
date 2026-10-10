# IM61 — Team instructions and assistant manager advice

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** Product-owner request 2026-10-10; design `Touchline Screens.dc.html` (tactics: TEAM INSTRUCTIONS panel, OPPONENT action card, "Why? Tactical fit vs …"). See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM58 (xG on matches)

## What to do

1. Add mentality / pressing / width / tempo to the club's tactics; they must change the simulation.
2. Assistant manager suggestion for the next opponent, built from real stats, with an "Apply suggested plan" action.
3. A "Why?" section explaining the suggestion's fit.
4. Wire the tactics page to the design.

## Recorded decisions

- Dials are -1/0/+1 (`matchsim.Instructions`); 0 is neutral and an identity on the style block, so golden replays and old `sim_inputs` are unchanged (no engine version bump). Levers re-weight existing boundaries only; no RNG.
- `instructions` on POST is optional: omitted keeps saved dials (policy bot path uses this).
- Advice lives in `internal/scout` (its stated home for tactics briefings). Evidence = opponent's saved style + last 5 completed matches (xG when present, else goals).
- The design's "38% of goals from crosses" and "familiarity 86%" need data the engine does not store; not built (open question).
- The design's pitch / lineup editor is out of scope here; the page covers style, formation, instructions, advice and Why.

## Delivery evidence

Files: `pkg/matchsim/instructions{,_test}.go`, `pkg/matchsim/{team,simulate}.go`, `migrations/0062_tactic_instructions.{up,down}.sql` + README row, `internal/squad/store.go`, `internal/match/team.go`, `internal/tactics/{service,instructions,instructions_test,tactics_integration_test}.go`, `internal/scout/{brief,brief_test,scout_integration_test}.go`, `internal/httpapi/{scout_handlers,tactics_training_handlers,router,error_codes}.go`, `internal/apidocs/openapi.yaml`; frontend `pages/play/tactics.vue`, `composables/manager/tactics.ts`, `types/manager/tactics.ts` (removed unused `stores/tactics.ts`).

Verification (2026-10-10): gofmt clean; `go build`, `go vet`, `go vet -tags integration` pass; `go test ./...` passes (incl. golden replay, docs gates). Integration (embedded Postgres 16): `internal/tactics` all pass incl. new instructions test; `TestTacticalBriefReadsOpponentEvidence` passes. Pre-existing failures identical on clean HEAD: `TestNextFixtureScout` (league_position), `match` commentary/rivalry tests, 4 `policybot` tests (fixture insert lacks competition_id). Frontend: `vue-tsc` and eslint pass; not exercised in a browser.
