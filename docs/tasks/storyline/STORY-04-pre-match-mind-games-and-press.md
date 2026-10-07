# STORY-04 — Pre-Match Mind Games, Tactical Stances & Rivalry Press

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline & Emergent Narrative Engine)  
**Source:** User request 2026-10-07, `docs/design/storyline-numerics.md`  
**Depends on:** STORY-02, S06-04 (social & rivalries), S04-02 (match engine)  

---

## 1. Goal

Implement the Pre-Match Mind Game system, allowing managers to select tactical pre-match stances before key fixtures (Derbies, Top-4 matches, Cup Knockouts, Human vs. Human), affecting match engine simulation parameters, card/foul risks, supporter sentiment, and press news output.

---

## 2. Requirements & Acceptance Criteria

### 2.1 Pre-Match Tactical Stances (`backend/internal/storyline/press.go`)

Implement stance selection model:
```go
type TacticalStance string

const (
    StanceAggressiveCombative   TacticalStance = "aggressive_combative"
    StanceUnderdogDeflect       TacticalStance = "underdog_deflect_pressure"
    StanceClinicalFocused       TacticalStance = "clinical_focused"
)
```

- **Selection API**: `POST /api/fixtures/:id/mindgames {stance}`
  - Allowed window: Between matchday scheduling and fixture lock (kickoff time).
  - Stores selection in `story.mind_game_stances (fixture_id, club_id, stance, selected_at)`.

### 2.2 Match Engine Simulation Parameters (`backend/internal/matchsim/`)
Pass active mind game stances into the deterministic match engine context:
- `aggressive_combative`:
  - Tackling intensity: +4%
  - Yellow card probability multiplier: **1.25**
  - Supporter sentiment movement in Derbies: **x1.5**
  - Opponent composure penalty: -5%
- `underdog_deflect_pressure`:
  - Composure floor floor: raised by +5%
  - Yellow card probability multiplier: **0.90**
  - Supporter sentiment expectation penalty: -5 rating points
- `clinical_focused`:
  - Tactical familiarity rating: +3%
  - Card/Foul risk: Neutral

### 2.3 Pre-Match News Story Generator (`backend/internal/storyline/fannews.go`)
- Generate news stories in category `fan_reaction` / `press_conference` 24h prior to fixture:
  - Headline examples: "Derby Escalation: [Manager A] Promises Combative Battle Against [RivalClub]", "[Manager B] Plays Down Pressure Ahead of Top-of-Table Clash".
  - Link `related_event_id` to fixture ID.

### 2.4 Human vs Human Rivalry Banter
- If both managers are human and have an active `manager↔manager` rivalry edge (strength ≥ 50, [Ch. 24](file:///Users/bfree/Desktop/booluw/touchline/docs/touchline-book/24-social-and-rivalries.md)):
  - Mind game selections trigger a direct social alert to the opposing manager (`social_message` / WebSocket push).
  - Post-match result adjusts rivalry strength by an additional +5 points if mind games were exchanged.

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/storyline/press_test.go`:
  - Test stance selection validation and window checks.
  - Test stance modifier integration into match engine simulation.
  - Test news story generation for pre-match build-up.
- Test OpenAPI routes in `openapi.yaml`.

### 3.2 Verification Commands
```bash
gofmt -w internal/storyline/ internal/matchsim/ internal/httpapi/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/storyline/... ./internal/matchsim/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] Mind game stance selection API implemented
- [ ] Match engine simulation parameter modifiers verified
- [ ] Pre-match news generator emitting build-up stories
- [ ] Human vs Human rivalry alerts and sentiment scaling verified
- [ ] Verification tests passing clean
