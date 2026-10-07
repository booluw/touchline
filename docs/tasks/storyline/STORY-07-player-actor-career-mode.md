# STORY-07 — User-as-Player Career Mode (Be-A-Pro) Scaffolding

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline & Emergent Narrative Engine / Phase 4)  
**Source:** User request 2026-10-07, `docs/design/player-career-mode-numerics.md`  
**Depends on:** STORY-01, S15-01 (multi-role actor framework), S04-02 (match engine)  

---

## 1. Goal

Implement the User-as-Player (Be-A-Pro) character creation flow, position-locked match rating engine, Human Manager ↔ Human Player contract/benching interactions, and player energy/training progression.

---

## 2. Requirements & Acceptance Criteria

### 2.1 Player Actor Profile Creation (`backend/internal/actor/player.go`)

- Implement API `POST /api/actors/player`:
  - Request fields: `primary_position`, `style_archetype`, `starting_age` (17-19), `agent_personality`.
  - Generates player entity in `player.players` linked to caller's `account_id`.
  - Places new player into academy pool or free agent list.

### 2.2 Position-Locked Match Rating Engine (`backend/internal/matchsim/player_rating.go`)
- Calculate minute-by-minute dynamic match rating (0.0 to 10.0) during live match simulation:
  - Track position-specific actions (goals, assists, tackles, interceptions, pass completion %, key passes, fouls, cards).
  - Record match rating breakdown in `player.match_performances (fixture_id, player_id)`.

### 2.3 Human Manager ↔ Human Player Interpersonal Hooks (`backend/internal/storyline/player_dilemma.go`)
- Detect 3 consecutive matches where Human Player is benched:
  - Generate dilemma for Human Player inbox (Request Transfer vs Confront Manager vs Work Harder).
  - If Human Player requests transfer, emit `PLAYER_TRANSFER_REQUESTED` event and notify Human Manager dashboard.

### 2.4 Energy & Training Progression (`backend/internal/player/progression.go`)
- Track player stamina/energy pool (0-100%). Matches consume 20-35% energy.
- API `POST /api/player/me/training {focus}`: Set weekly training focus (*Physical*, *Technical*, *Tactical*, *Mental*).
- Incrementally grow player attributes based on training focus + match rating performance.

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/actor/player_test.go`:
  - Test player actor profile creation.
  - Test position-locked match rating calculation logic.
  - Test player energy consumption and attribute progression.
- Write Gin API integration tests for `/api/actors/player` and `/api/player/me/training`.

### 3.2 Verification Commands
```bash
gofmt -w internal/actor/ internal/matchsim/ internal/player/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/actor/... ./internal/matchsim/... ./internal/player/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] Player actor profile creation flow implemented
- [ ] Position-locked match rating engine verified
- [ ] Human Manager ↔ Human Player benching/transfer request hooks tested
- [ ] Energy and attribute progression system clean and passing tests
