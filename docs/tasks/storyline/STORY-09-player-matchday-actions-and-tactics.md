# STORY-09 — Player Matchday Actions, Tactical Roles & On-Pitch Discipline

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline Engine & Player Actor Mode)  
**Source:** User request 2026-10-07, `docs/design/player-career-mode-numerics.md`  
**Depends on:** STORY-07, STORY-04 (mind games), S04-02 (match engine)  

---

## 1. Goal

Implement matchday actions and reactions for Player Actors: pre-match personal goal setting, tactical role requests, in-match interactive decisions (calling for pass, referee arguments, goal celebrations), and post-match interview quotes.

---

## 2. Requirements & Acceptance Criteria

### 2.1 Pre-Match Personal Objectives (`backend/internal/player/match_objectives.go`)

Before kickoff, the player actor selects a **Personal Match Objective**:
- Options: `score_goal`, `provide_assist`, `pass_accuracy_90`, `clean_sheet`, `outperform_rival_defender`.
- Completion reward: +0.5 Match Rating bonus; +5% confidence boost for next match.
- Failure penalty: -0.2 Match Rating penalty.

### 2.2 Tactical Role Requests (`backend/internal/player/tactical_requests.go`)
- API `POST /api/player/me/tactical-request`:
  - Request preferred role: `free_roam_playmaker`, `target_man`, `penalty_taker`, `free_kick_taker`, `captaincy`.
  - Manager receives prompt; if granted, player receives +5% performance bonus in requested role. If rejected, player morale drops -0.05.

### 2.3 In-Match Interactive Actions (`backend/internal/matchsim/player_actions.go`)
During live match simulation tick processing:
- **Call for Ball / Demand Pass**: Increases probability of teammates passing to player by +25%; if player loses ball, composure drops -5%.
- **Argue with Referee**:
  - 60% chance of ref leniency (-1 foul severity).
  - 40% chance of Yellow Card penalty.
- **Goal Celebration Stance**:
  - `passionate_badge_clap`: Supporter Sentiment +5, Rival Hostility neutral.
  - `shush_opposing_fans`: Rival Hostility +25, Yellow Card risk +10%.
  - `dedicate_to_manager`: Manager Relationship +10.

### 2.4 Post-Match Interview Quotes (`backend/internal/player/post_match.go`)
- After match completion, player actor responds to post-match press prompt:
  - `praise_team_and_manager`: Morale floor +0.05, Manager Trust +5.
  - `criticize_referee`: League fine $2,500; referee strictness modifier +10% in next match.
  - `express_frustration_at_sub`: Morale -0.10; Manager relationship -10.

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/matchsim/player_actions_test.go`:
  - Test pre-match objective evaluation against match stats.
  - Test goal celebration effects on supporter sentiment and card risk.
  - Test post-match press quote resolution.
- Run `go test ./internal/matchsim/... ./internal/player/...`.

### 3.2 Verification Commands
```bash
gofmt -w internal/player/ internal/matchsim/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/player/... ./internal/matchsim/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] Pre-match personal match objectives implemented & tested
- [ ] Tactical role request framework functioning
- [ ] In-match interactive actions (call for ball, ref argument, goal celebration) verified
- [ ] Post-match interview quotes updating sentiment and relationships
