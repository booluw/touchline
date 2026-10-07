# STORY-02 — Core Narrative Arc Archetypes Implementation

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline & Emergent Narrative Engine)  
**Source:** User request 2026-10-07, `docs/design/storyline-numerics.md`  
**Depends on:** STORY-01  

---

## 1. Goal

Implement the pattern matchers and state machine logic for the 5 initial core narrative arc archetypes: `underdog_miracle`, `mutiny_faction_split`, `ex_factor_revenge`, `academy_local_hero`, and `great_escape_relegation`.

---

## 2. Requirements & Acceptance Criteria

### 2.1 Archetype Implementation (`backend/internal/storyline/archetypes/`)

Implement the pattern matcher interface for each archetype:

```go
type ArcMatcher interface {
    ArcType() string
    CheckTrigger(ctx context.Context, snapshot WorldSnapshot, clubID string) (*StoryArc, bool, error)
    AdvanceState(ctx context.Context, arc *StoryArc, tick int64) (*StateTransition, error)
}
```

1. **`underdog_miracle.go`**:
   - Evaluates club reputation ≤ **60**.
   - Monitors trailing 4 fixtures for unbeaten streak OR victory against opponent with reputation ≥ club.reputation + 20.
   - Computes `overconfidence_factor = clamp((unbeaten_matches - 3) * 0.05, 0.0, 0.20)`.
   - Triggers `STORY_ARC_TRIGGERED` with news story in `fan_reaction` category.

2. **`mutiny_faction_split.go`**:
   - Evaluates squad influencers (`internal/squad/influencers.go`) for `morale ≤ 0.35`.
   - Checks dressing room faction alignment (`internal/dressingroom`) for ≥ 3 players with low morale in same faction.
   - Applies faction morale decay of **-0.02 per tick** while active.
   - Emits `mutiny_faction_split` dilemma prompt.

3. **`ex_factor_revenge.go`**:
   - Monitors upcoming fixture schedule for matches against clubs employing a former starter player (transferred/released within 365 world days) or former manager.
   - Boosts ex-player performance expectation +10% and condition decay -5%.
   - Triggers pre-match rivalry news story 48h prior to kickoff.

4. **`academy_local_hero.go`**:
   - Checks players with `age ≤ 20` and `is_homegrown = true`.
   - Tracks consecutive match ratings ≥ **75** in 3 starts.
   - Increases supporter sentiment by +0.5 per rating point over 75.
   - Triggers contract renewal vs transfer interest dilemma.

5. **`great_escape_relegation.go`**:
   - Evaluates `league_position` in bottom **3** with remaining league fixtures ≤ **8**.
   - Increases board review frequency to every **2 fixtures**.
   - Sets motivation floor floor = **65** for relegation battles.

### 2.2 Cooldown & Deduplication Rules
- Enforce `StoryArcCooldownDays = 30`: A club cannot re-trigger the same `arc_type` within 30 world days of resolving a previous instance.
- Cap active story arcs to `MaxActiveStoryArcsPerClub = 2`.

---

## 3. Verification Plan

### 3.1 Unit Tests
- Write `internal/storyline/archetypes/*_test.go` with deterministic mock data for each of the 5 archetypes:
  - Test underdog streak detection and overconfidence calculation.
  - Test mutiny faction morale decay and trigger conditions.
  - Test ex-player fixture detection.
  - Test academy hero rating streak evaluation.
  - Test relegation escape fixture threshold triggers.

### 3.2 Verification Commands
```bash
gofmt -w internal/storyline/archetypes/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/storyline/archetypes/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] All 5 pattern matchers implemented in `internal/storyline/archetypes/`
- [ ] Cooldown and deduplication logic verified
- [ ] Unit test suite clean and passing
