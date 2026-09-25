# IM08 — Regional (continental) cup campaigns: creation, preview, anchored calendar, membership, bracket

**Status:** Implemented
**Sprint:** Improvements (competition scheduling)
**Source:** Product decision (manual session)
**Depends on:** IM04 (knockout bracket, golden goal, `applyKnockoutResult`, the
`cupPlan.ladder`/`materializeRound` seams, campaign lifecycle, `ErrCupLimit`
cap); IM05 (anchored cup calendar + live re-pacing); IM06 (regions, `region_id`,
league reputation); IM07 (the qualification engine — position bands + champion
entitlement).

## What to do

Deliver the **system-seeded regional cup**: an admin creates a
`continental`-type knockout cup scoped to a **region** (not a country), sets a
**soft tier** label, defines per-league **position-band qualification** (with a
reputation-informed wizard default), previews the projected field, and starts a
campaign. The campaign builds its field from the IM07 engine, honours the
reigning-champion +1, schedules rounds on the **union of the participating
countries' league days** (IM05-style anchoring), and runs the bracket exactly
like a domestic cup (IM04).

1. **Create a regional cup** — `scope: "region"` records `region_id + tier`
   alongside the name, prize pool, and scheduling rules already supported for
   country cups. A fresh `POST /api/admin/cups` accepts both scopes; a region-
   scope cup carries `competition_type 'continental'`, `country_id NULL`.
2. **Qualification editing** — `POST /admin/cups/:id/qualification` writes the
   `cup_qualification` rows (bands per league). The API rejects overlapping
   bands (per league) and out-of-range positions.
3. **Wizard default bands from reputation** — for a tier-K regional cup, each
   country in the region offers its **tier-K league** with a default band from
   the league's reputation (rep ≥90 → `1..4`; ≥75 → `1..3`; ≥60 → `1..2`;
   else → `1..1`); the admin edits freely before creating. These defaults are a
   UI input, never a constraint.
4. **Preview** — `POST /api/admin/cups/preview {draft config}` returns the
   projected field with per-entrant origins (`position`, `champion_*`) plus
   warnings (league missing a completed season, empty band, field < 2, double-
   booked champs flagged for IM09). Writes nothing.
5. **Campaign start** — `POST /api/admin/cups/:id/campaign` (region scope)
   freezes/validates qualification, computes the field via `QualifyField`,
   writes `role='cup'` memberships + `competition_entries` + the
   `competition.seasons` row, and materializes Round 1 on the anchored calendar.
6. **Reads** — regional cups appear in `GET /api/cups` and `GET /api/cups/:id`
   campaign views exactly like country cups; club fixtures, calendar, and the
   scout `next-fixture` surface them through the existing `role='cup'` plumbing
   (asserted, not re-implemented).

## Behaviour

### Scope, tier, and qualification rules

- `POST /api/admin/cups`:
  - `{world_id, country_id, name, prize_pool?, scheduling_rules?, first_tier_bye?, survivor_threshold?}` — **country scope** (existing IM04 contract, unchanged).
  - `{world_id, region_id, tier?, name, prize_pool?, scheduling_rules?, qualification: [{league_id, from_position, to_position?}]}` — **region scope**, `competition_type 'continental'`, `country_id NULL`.
- `tier` reuses the existing `competition.competitions.tier` column. **Soft
  semantics**: a tier is a label + the wizard's default-band generator. It is
  *not* a filter at qualification time and not a scheduling constraint — only
  the IM09 precedence sweep compares tiers (`higher tier wins`).
- Validation: `404` world/region/league; a banded league must belong to a
  country **inside the region** (else `422`); overlapping bands on the same
  league `422`; `tier` optional (default empty = lowest); `name` collision in
  the world `409`.
- The `qualification` payload is stored as rows (IM06); the engine always reads
  the rows. `competition_rules.qualification_rules` JSONB keeps a **human-
  readable mirror** (bands, tier, `"entry": "regional_league_bands"`) purely for
  introspection, populated at creation like IM04 — the rows are authoritative.

### Preview endpoint

`POST /api/admin/cups/preview` `{world_id, region_id, tier?, name?, qualification}` →
`200 {field: [{club_id, country, league, origin, rank}], champion: {club, origin},
warnings: […], field_size}` with:

- per-league: "no completed season" (band yields nothing until one exists),
  "band partially full" (clamped past table length), "band empty";
- field-wide: `field_size < 2` (recommend widening), champions double-booked
  (`conflicts` list, resolution deferred to IM09);
- the **reigning-champion +1 applied** exactly as IM07 computes it, so the
  admin sees the final count ("always 5 total when a league has a champ").
- No writes, no season reads beyond completed tables; errable only on scope
  validation.

### Campaign start (region scope)

1. Re-validate qualification rows (belongs to region, no overlap) → `422` fail-fast.
2. Each banded league must have a completed season → `ErrQualificationUnavailable` `422`.
3. `GenerateField(cup)` = IM07 `ComputeField` (bands + champion entitlement).
4. `field_size >= 2` or `422 ErrQualificationField`.
5. Write, single transaction:
   - `competition.seasons` (`in_progress`, `start_date`/`end_date` per calendar).
   - `role='cup'` memberships for the field. The **champion and next-best
     cascade entrants are exempt from the 3-cup cap** (`ErrCupLimit`) — they can
     never be denied their entitlement; positional entrants remain capped.
   - `competition_entries` (`qualified`) with the origin recorded for preview/news.
   - Round-1 fixtures on the anchored calendar.
   - Second call while live → `409` (mirrors campaign-exists guard).
6. The cup campaign closes exactly as IM04 (`completed` + `end_date`, champion
   entry recorded) — the return `champion` entry feeding IM07's next cycle.

### Regional calendar (IM05 generalization)

- `planCupCalendar` for region cups: **anchor days = union over participating
  countries** — the countries whose leagues have band rows (and the champion's
  country) — of `countryLeagueDays`. A regional final must land **≥3 days after
  the latest participating league ends**; earlier rounds spaced per the cup's
  `scheduling_rules` (default pacing / seeding-rules gap walk), reusing IM05's
  backfill + live re-pacing exactly.
- League days are **union, sorted, deduped**: cross-country shared days produce
  one anchor each. When the union is empty (no participating country has a
  configured calendar yet), fall back to the weekly day formula (legacy cup
  path) and warn in the campaign response.
- No new columns: the nullable `matchday` on `match.fixtures` stays the round
  index; kickoff rotation and `world_seed ⊕ cup_id ⊕ round` bracket draws are
  the IM04 contracts unchanged.

### Reads

- `GET /api/cups` / `GET /api/cups/:id` include regional cups (region field,
  tier, qualification rows) with the same campaign view (rounds → ties →
  windows → scores → winner) as country cups; the frontend can label them
  "Regional / Continental".
- Club fixtures / calendar / scout next-fixture: no new code — assert in tests
  that a regional cup tie appears in `ListClubFixtures`, season calendar, and
  `next-fixture` via the existing `role='cup'` plumbing.

## Changes

### internal/competition

- `cup.go`: `CreateCup` branch "region scope" (region_id + tier + qualification
  rows); `SetQualification`; `PreviewField` (call IM07 `ComputeField` over
  injected providers, return origins + warnings); campaign start uses the same
  `ComputeField`; cap-exempt membership writes.
- `calendar.go`: `planCupCalendar` region branch (union of participating
  countries' league days, ≥3-day final gap after the latest league end, weekly
  fallback); `cupPlan.ladder`/`materializeRound` untouched (IM04).
- `service.go`: `ErrQualificationUnavailable`, `ErrQualificationField`,
  region-scope validation, `ErrRegionMismatch`.
- Depends on IM07 `qualify.go` (new this milestone is a pure Leaf; no engine
  columns change).

### internal/httpapi + openapi

- `cup_handlers.go`: extend `create` (scope region), add `POST
  /api/admin/cups/preview`, `PATCH /api/admin/cups/:id/qualification`; campaign
  route already covers both scopes (validate region cups pass `region_id`).
- `openapi.yaml`: regional cup create + preview + qualification refs; region
  field on `Cup`; `CupQualificationBand`, `CupPreviewField`, warnings;
  `TestDocsCoverRouter` updated.

### Frontend

- Admin cup form ships a scope toggle; region scope shows region picker, tier,
  per-country league band rows (auto-filled via the reputation default), a
  "Preview field" button rendering origins + warnings, then create + start.
- Manager cup list/page label regional cups; no other manager change here
  (resolution UX is IM09).

## Tests

- Unit: default-band generator (reputation → band table incl. clamp at 100/0);
  `SetQualification` overlap/mismatch validation; calendar union math across
  two countries with overlapping league days, final gap ≥3 days after latest
  league end, weekly fallback when the union is empty.
- Integration (CI-only): region world (2 countries, leagues, completed seasons)
  → create tier-1 cup band `1..4`, preview matches the IM07 field, start
  campaign → field size, memberships (with cap-exemption proof for a champion),
  entries, Round-1 fixtures on anchored days, `409` double-start, `422`
  `ErrQualificationUnavailable` when a banded league has no completed season.
- Reuse of IM04/IM05 regression: knockout progression, golden-goal ties, no
  `competition.standings` writes, no rollover events, bracket determinism, club
  3-cup cap for *positional* entrants — all on top of the existing IM04 fixture
  matrices (regression-guard the untouched path).
- Docs: `TestDocsCoverRouter` + `TestDocsOpenAPIValid` green.

## Docs

- `docs/tasks/improvements/IM08-regional-cup-campaigns.md` (this file).
- `docs/how-to/cup-competitions.md`: regional-cup section (create → preview →
  bands → start; anchor semantics; cap exemption).
- `docs/how-to/glossary.md`: `regional cup`, `continental cup`, `soft tier`,
  `position band`, "reputation default band".
- `docs/product_manager.md`: recorded decision — regional cups are admin-seeded
  `continental` competitions with per-league bands + tier as a label, running
  on union league days; the manager *choice* mechanics are IM09.

## Recorded decisions

- **One universal qualification mechanism for cups**: per-league position bands
  via `cup_qualification` (this task), reusing IM07's engine. Country cups keep
  their existing "all clubs" default (a `1..NULL` band under the same table) and
  today's `first_tier_bye`/`survivor_threshold` staging — both rows coexist.
- **Tier is soft**: a label + wizard default-band generator. The engine never
  filters on it; only IM09's precedence sweep compares tiers.
- **Reputation shapes suggestions only**: the wizard's default bands come from
  league reputation (≥90→1..4, ≥75→1..3, ≥60→1..2, else→1..1); the admin edits
  every band and the engine reads only the committed rows.
- **Qualification is always historical**: campaign start reads the last
  completed season per banded league; an in-progress table is never used and a
  banded league without a completed season refuses to start — the preview
  surfaces this before the admin commits.
- **Champion entitlement +1 with cap exemption**: a reigning champion and its
  next-best cascade entrant enter regardless of the 3-cup cap; positional clubs
  stay capped (`ErrCupLimit`). Resolution of a double-booked champion is IM09.
- **Regional rounds follow union league days** (IM05 anchoring, ≥3-day final
  gap after the latest league end, weekly fallback when nothing is configured)
  because a region has no single country calendar to anchor to.

## Delivery evidence

**Status:** Implemented (backend + docs in one slice; the Nuxt admin form from
the Frontend section is deferred — plan-mode decision).

**Delivered files**

- `AGENTS.md` — repo-root agent guidance (layout, verification order,
  improvement workflow) shipping with this milestone.
- `backend/internal/competition/regional_cup.go` (new) — the IM08 service
  surface: `CreateRegionalCup`, `SetQualification`,
  `PreviewCupField`, `StartRegionalCupCampaign`, `CupScope`, pure helpers
  `DefaultBandForReputation`, `inputsToBands`, `validateBandsNoOverlap`,
  `bandsForCup`, `validateRegionalBands`, `unionLeagueDays` (in `cup.go`),
  `previewEntrants`/`previewWarnings`, cap-exempt `writeRegionalMemberships`,
  and the `regionalRulesJSON` `"entry":"regional_league_bands"` mirror.
- `backend/internal/competition/cup.go` — `Cup` now carries nullable `Country`
  (pointer), `Region *RegionRef`, `Tier *int`; `ListCups`/`getCup`/`scanCup`
  cover both `domestic_cup` and `continental` (IN list, LEFT JOINs);
  `cupPlan.Entrants` + `cupPlanEntrant` persist the field's origins;
  `planCupCalendar` takes precomputed `leagueDays` (loads moved to
  `unionLeagueDays`); `CupParams` gained region scope fields.
- `backend/internal/competition/qualify.go` — extracted `computeField` (no
  min-size error) so preview renders a sub-2 field as a warning; `ComputeField`
  keeps the `ErrQualificationField` wrap; `ComputeCupField` now reuses
  `bandsForCup`.
- `backend/internal/competition/service.go` — sentinels `ErrRegionMismatch`,
  `ErrQualificationOverlap`; `RegionRef`.
- `backend/internal/competition/scheduling.go` — `UpdateCupScheduling` accepts
  `domestic_cup`/`continental` via `requireCup`.
- `backend/internal/competition/club_overview.go` — `Country` pointer for the
  nullable field.
- `backend/internal/httpapi/cup_handlers.go` — `handleCreateCup` dispatches
  country vs region scope; new `handlePreviewCup`,
  `handleSetCupQualification`, `handleStartRegionalCupCampaign` (dispatched on
  `Service.CupScope`; returns `{season, warnings}`); route registration in
  `router.go`.
- `backend/internal/apidocs/openapi.yaml` — three new admin routes, `Cup`
  region/tier/nullable `country`, `RegionRef`, `CupQualificationBand`,
  `CupPreviewEntrant`/`CupPreviewResult`, `LeagueRef`; both
  `TestDocsCoverRouter` and `TestDocsOpenAPIValid` green.
- Tests — `backend/internal/competition/regional_cup_test.go`
  (`TestDefaultBandForReputation` incl. clamp, `TestInputsToBands`,
  `TestValidateBandsNoOverlap`) and
  `regional_cup_integration_test.go` (lifecycle: two-country region → create →
  preview = engine field → campaign → Round-1 fixtures → memberships →
  ListCups/GetCup reads → `409` double-start → out-of-region band `422`; plus
  champion re-preview, unavailable-league, and cap-exemption scenarios).

**Notable plan deviations (all kept in-scope, documented above)**

- `calendar.go` plan entry implemented as a `unionLeagueDays` helper + a
  `planCupCalendar` signature change in `cup.go`; no `calendar.go` edits.
- Qualification route is `PATCH /api/admin/cups/:id/qualification` (the plan
  sketched `POST`).
- Campaign start is a dedicated world-scoped route
  (`POST /api/admin/worlds/:id/cups/:cupID/campaign`) that dispatches on the
  cup's type via `CupScope`, rather than extending the country route.
- Integration tests compile-gated via `//go:build integration`; they cannot
  run locally here (no `TEST_DATABASE_URL`, Docker down) — verified by
  `go vet -tags integration ./internal/... ./pkg/...`.

**Docs**

- `docs/how-to/cup-competitions.md` — new "6. Regional (continental) cups".
- `docs/how-to/glossary.md` — `regional cup`, `soft tier`, `reputation default
  band`; `cup qualification (field)` updated with `ErrQualificationUnavailable`.
- `docs/product_manager.md` — recorded decision `OPD-35`.

**Verify**

```
gofmt -w                      # on every touched Go file
go build ./...                # clean
go vet ./...                  # clean
go vet -tags integration ./internal/... ./pkg/...   # clean (integration compile-gate)
go test ./...                 # green — includes TestDocsCoverRouter/TestDocsOpenAPIValid
```