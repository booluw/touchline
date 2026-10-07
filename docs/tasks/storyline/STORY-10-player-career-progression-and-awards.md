# STORY-10 — Player Career Progression, International Caps & Ballon d'Or Awards

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline Engine & Player Actor Mode)  
**Source:** User request 2026-10-07, `docs/design/player-career-mode-numerics.md`  
**Depends on:** STORY-07, STORY-05 (season chronicles), S13-01 (national teams)  

---

## 1. Goal

Implement player international career call-ups, national caps, seasonal individual awards (Golden Boot, Player of the Season, World Footballer of the Year / Ballon d'Or), aging/decline mechanics, and post-retirement transition into Manager/Scout roles.

---

## 2. Requirements & Acceptance Criteria

### 2.1 International Career & Caps (`backend/internal/player/international.go`)

- **National Team Call-Up Evaluator**:
  - Monitors trailing 10 match ratings: Average rating ≥ **7.5** AND overall attribute ranking in top 3 for nationality/position.
  - Generates `INTERNATIONAL_CALL_UP` event and inbox notification.
  - Tracks national caps, international goals, and tournament appearances (World Cup, Continental Championship).

### 2.2 Season & World Awards (`backend/internal/player/awards.go`)
- Evaluate seasonal awards during season rollover ([Ch. 7](file:///Users/bfree/Desktop/booluw/touchline/docs/touchline-book/07-seasons-and-rollover.md)):
  - **Golden Boot**: Player with highest league goals scored.
  - **Playmaker of the Year**: Highest league assists.
  - **Player of the Season**: Highest mean match rating (min 20 starts).
  - **World Footballer of the Year (Ballon d'Or)**: Highest combined performance score across domestic league, cup, and international fixtures.
- Awards saved to `player.player_awards` and displayed on public profile.

### 2.3 Aging, Physical Decline & Retirement (`backend/internal/player/aging.go`)
- **Decline Curve**:
  - Ages 17–27: Growth phase based on training focus & game time.
  - Ages 28–31: Prime phase (attributes stay stable; mental attributes grow).
  - Ages 32+: Physical decline (Acceleration, Pace, Stamina decay by -1 to -3 per season).
- **Retirement Decision**:
  - Occurs between ages 34–38 (or earlier if major unrecovered injury occurs).
  - Emits `PLAYER_RETIRED` event.

### 2.4 Post-Retirement Manager / Scout Transition (`backend/internal/actor/transition.go`)
- Upon retirement, player actor user is presented with **Career Transition Options**:
  1. **Transition to Manager**: Converts actor profile to Human Manager, carrying over legacy reputation score.
  2. **Transition to Scout**: Converts actor to Club Scout (Phase 4 inhabitable role).
  3. **Transition to Player Agent**: Converts actor to Player Agent.

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/player/awards_test.go`:
  - Test Ballon d'Or and Golden Boot award calculation algorithms.
  - Test international call-up eligibility filters.
  - Test player aging decline curve math.
  - Test player-to-manager post-retirement transition.
- Run `go test ./internal/player/... ./internal/actor/...`.

### 3.2 Verification Commands
```bash
gofmt -w internal/player/ internal/actor/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/player/... ./internal/actor/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] International call-up and caps tracking implemented
- [ ] Seasonal awards (Golden Boot, Ballon d'Or) algorithm verified
- [ ] Player physical decline and retirement engine working
- [ ] Post-retirement transition to Manager/Scout role verified
