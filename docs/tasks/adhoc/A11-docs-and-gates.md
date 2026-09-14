# A11 — Documentation and gate verification

**Status:** Not started
**Sprint:** Ad-hoc (player lifecycle)
**Source:** User design session
**Depends on:** A01–A10

## What to do

Write the design doc, update project docs, record product decisions, and run the full verification gate suite.

## Changes

### New: docs/design/player-lifecycle.md

Full design document covering:
- Country-scoped player pools (size, age distribution, replenishment)
- Draft model (squad template, pool selection algorithm)
- Club academy (PRD §23 model, intake formula, investment tiers)
- Country/street academy (street kids 13–15, intake range)
- Retirement model (probability curve, age/ability/injury/ambition modifiers)
- Match eligibility rule (professional contract + street 18+)
- Signing flow (human + AI auto-fill)
- Admin bulk creation (quality bands, metrics)
- Seasonal rollover sequence
- Event catalogue (`PLAYER_CLAIMED_FROM_POOL`, `ACADEMY_INTAKE`, `COUNTRY_ACADEMY_INTAKE`, `PLAYER_RETIRED`, `WORLD_LIFECYCLE_SEASON_COMPLETED`, `AI_AUTO_FILL`, `PLAYER_SIGNED`, `PLAYER_RELEASED`, `ADMIN_BULK_PLAYER_CREATED`)
- Finance interactions (youth contract wages, academy annual cost, signing wage commitment)

### Update: docs/product_manager.md

Record OPDs for:
- **OPD-17: Country academy / street kids** — new concept not in original PRD. Player origin `street` created 13–15, immediately signable, match-ineligible until 18.
- **OPD-18: Player pool draft model** — replaces per-club player generation. Clubs draft from shared country pool; free agents are an ongoing resource.
- **OPD-19: Match eligibility rule** — professional contract required; academy products have no age floor; street origin enforces 18+.
- **OPD-20: Squad size** — target 24 for first-team eligibility; academy youth don't count against target.

### Update: OPENCODE.md

- Add `internal/playerpool` (pool, sign, bulk, street intake).
- Add `internal/academy` (service, intake).
- Add `internal/lifecycle` (retirement, rollover hook).
- Add new API routes (free agents, academy, bulk create, release).
- Update worker `SEASON_COMPLETED` subscription.
- Update bootstrap + seed-competition descriptions.
- Add migration 0036.
- Add playerlifecycle.md to design docs list.
- Update "What is NOT built yet" — remove S08-01 references now implemented.

### Update: docs/how-to/setup-and-launch.md

- Add note: league seeding now creates free-agent pool + club academies.
- Add note: street kids enter the pool each season via rollover.
- Add note: retirement fires at rollover.
- Add note: `POST /api/admin/.../players/bulk` for bulk creation.

### Update: docs/tasks/README.md

Add a row for the ad-hoc sprint:

```
| A0x | Ad-hoc | Player lifecycle | A01–A11 |
```

### Gate verification

```bash
go vet ./...
go build ./...
go test -p 1 -tags integration ./...
```

And if applicable:
```bash
gofmt -l .
cd frontend && pnpm exec vue-tsc --noEmit
cd frontend && pnpm run lint
```

## Acceptance criteria

- All docs updated as listed above.
- All gates pass clean.
- `gofmt` shows no drift.

## Delivery evidence

- Pending.
