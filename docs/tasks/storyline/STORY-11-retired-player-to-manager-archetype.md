# STORY-11 — Retired Player to Manager Archetype & Former-Club Affinity

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline Engine & Manager Career Arc)  
**Source:** User request 2026-10-07, `docs/design/retired-player-manager-archetype.md`  
**Depends on:** STORY-01, STORY-10 (player retirement), S06-02 (board & job offers)  

---

## 1. Goal

Implement the former-club hiring affinity system (`manager.EvaluateFormerClubAffinity`), tactical DNA inheritance from playing career (`manager.InheritTacticalDNA`), and the `prodigal_son_manager_return` narrative arc archetype for retired players becoming managers (modeled on Pep Guardiola, Xabi Alonso, and Cesc Fàbregas).

---

## 2. Requirements & Acceptance Criteria

### 2.1 Former-Club Hiring Affinity Modifiers (`backend/internal/manager/affinity.go`)

- Implement `EvaluateFormerClubAffinity(ctx context.Context, managerID string, clubID string) (*AffinityBonus, error)`:
  - Check player appearance history at `clubID`.
  - Calculate `AffinityScore = BaseAffinity(15) + (Caps * 0.5) + TrophyBonus + LegendBonus`.
  - Lower board minimum reputation barrier by **-25%**.
  - Increase board negotiation tolerance by **+2**.
  - Set starting Supporter Sentiment floor to **70** upon job acceptance.

### 2.2 Tactical DNA Inheritance (`backend/internal/manager/tactical_dna.go`)
- When a player actor retires and transitions to manager:
  - Analyze managers played under during career.
  - Assign inherited `TacticalDNA` (Formation preference, Passing style, Pressing intensity).
  - Grant +10% tactical familiarity bonus when deploying inherited formation style.

### 2.3 The `prodigal_son_manager_return` Narrative Arc (`backend/internal/storyline/archetypes/prodigal_son.go`)
- **Trigger**: Retired player appointed as head coach of a former club played for (appearance count ≥ 50).
- **Stage 1 (Hero's Welcome)**: Emits `announcement` media news story celebrating the return of the club icon.
- **Stage 1 Dilemma**: "Coaching Former Teammates" (Assert Authority vs Collaborative Captain-Coach Stance).
- **Stage 3 Resolution**:
  - Exceeding/Meeting board mandate → Emits `LEGENDARY_RETURN_CELEBRATION` event, +25 manager reputation.
  - Sacking → Emits `TRAGIC_ICON_SACKING` news story, -20 manager reputation penalty.

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/manager/affinity_test.go`:
  - Test former-club affinity calculation based on player match history.
  - Test reputation discount and board tolerance boost during job offer generation.
  - Test tactical DNA inheritance math.
- Write `internal/storyline/archetypes/prodigal_son_test.go`:
  - Test `prodigal_son_manager_return` arc state machine and resolution outcomes.
- Run `go test ./internal/manager/... ./internal/storyline/...`.

### 3.2 Verification Commands
```bash
gofmt -w internal/manager/ internal/storyline/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/manager/... ./internal/storyline/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] Former-club affinity scoring engine implemented
- [ ] Tactical DNA inheritance calculation verified
- [ ] `prodigal_son_manager_return` arc state machine and dilemmas tested
- [ ] Unit and integration test suite passing clean
