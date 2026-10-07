# STORY-05 — Season Storybook & Manager Career Chronicle

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline & Emergent Narrative Engine)  
**Source:** User request 2026-10-07, `docs/design/storyline-numerics.md`  
**Depends on:** STORY-01, S07-01 (season rollover)  

---

## 1. Goal

Implement the Season Storybook Digest generator during season rollover, compile career chronicle achievements and badges, and expose the Manager Career Storybook API (`GET /api/managers/:id/chronicle`).

---

## 2. Requirements & Acceptance Criteria

### 2.1 Season Storybook Digest Synthesis (`backend/internal/storyline/chronicle.go`)

- Hook into season rollover cadence pass ([Ch. 7](file:///Users/bfree/Desktop/booluw/touchline/docs/touchline-book/07-seasons-and-rollover.md)):
  - Run `GenerateSeasonChronicle(ctx context.Context, worldID string, seasonNumber int, clubID string) (*SeasonChronicle, error)`.
  - Analyze season event history for the club:
    - **Headline**: Synthesize dynamic headline based on league finish vs board target finish ([docs/design/storyline-numerics.md](../../design/storyline-numerics.md)).
    - **Key Story Arcs**: Extract completed story arcs during the season (`underdog_miracle`, `great_escape_relegation`, etc.).
    - **Breakout Player**: Identify homegrown youngster with highest match rating growth or goal contributions.
    - **Biggest Win & Bitterest Loss**: Extract fixtures with maximum positive and negative goal differences.
    - **Chronicle Digest**: Write structured prose summary storing narrative milestones in `story.chronicles`.

### 2.2 Manager Career Badges & Achievements (`backend/internal/storyline/badges.go`)
- Evaluate career milestones for active human managers:
  - `Giant Killer`: Defeated 3+ clubs with reputation ≥ 20 points higher in a single season.
  - `Academy Developer`: Graduated and gave starts to 3+ academy players in a season.
  - `Derby King`: Unbeaten in 5 consecutive rivalry games.
  - `Master of the Escape`: Avoided relegation in a `great_escape_relegation` arc.
- Badges stored in `manager.manager_badges` and displayed on the public manager profile (`GET /api/managers/:id/profile`, [Ch. 24](file:///Users/bfree/Desktop/booluw/touchline/docs/touchline-book/24-social-and-rivalries.md)).

### 2.3 Career Chronicle REST API (`backend/internal/httpapi/chronicle_handlers.go`)
- `GET /api/managers/:id/chronicle`:
  - Returns array of `SeasonChronicle` objects for all completed seasons in the manager's career, ordered by `season_number DESC`.
  - Includes club identity, league finish, badges earned, and text digest.
- Document route in `backend/internal/apidocs/openapi.yaml`.

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/storyline/chronicle_test.go`:
  - Test season chronicle digest synthesis against simulated season event logs.
  - Test manager badge assignment logic.
  - Test HTTP handler `GET /api/managers/:id/chronicle` response serialization.
- Run Gin router openapi validation tests (`TestDocsCoverRouter`, `TestDocsOpenAPIValid`).

### 3.2 Verification Commands
```bash
gofmt -w internal/storyline/ internal/httpapi/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/storyline/... ./internal/httpapi/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] Season chronicle synthesis engine integrated into season rollover
- [ ] Manager career badges system implemented
- [ ] `GET /api/managers/:id/chronicle` route registered and documented in OpenAPI
- [ ] Unit and integration tests passing clean
