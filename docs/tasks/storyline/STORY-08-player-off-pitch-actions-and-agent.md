# STORY-08 — Player Off-Pitch Actions, Agent Management & Social Statements

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline Engine & Player Actor Mode)  
**Source:** User request 2026-10-07, `docs/design/player-career-mode-numerics.md`  
**Depends on:** STORY-07 (player actor foundation), S06-04 (social & messaging)  

---

## 1. Goal

Implement the complete off-pitch action framework for Player Actors: hiring/firing agents, contract demands (release clauses, guaranteed starter role, shirt number), formal transfer/loan requests, public social media statements, and lifestyle/recovery management.

---

## 2. Requirements & Acceptance Criteria

### 2.1 Agent Management & Hiring (`backend/internal/player/agent.go`)

- **Agent Types**:
  - `money_oriented`: Demands +30% higher wages and agent commission; aggressive in contract talks.
  - `career_driven`: Focuses on continental competition clauses and playing time guarantees.
  - `loyal_family`: Low commission, prioritizes long-term contract stability.
- **APIs**:
  - `GET /api/player/me/agents` - List available player agents for hire.
  - `POST /api/player/me/agent/hire {agent_id}` - Hire agent; deducts 5% commission from salary.
  - `POST /api/player/me/agent/fire` - Terminate agent contract.

### 2.2 Contract Demands & Negotiations (`backend/internal/player/contract_demands.go`)
- Player actors can attach **Contract Conditions** when negotiating with Human or AI Managers:
  - `guaranteed_starter_role`: If benched for >2 games, contract breach trigger activates.
  - `release_clause`: Specific buyout fee.
  - `preferred_shirt_number`: Requests specific squad shirt number (1-99).
- API `POST /api/player/me/contract/counter`: Submit counter-proposal to manager.

### 2.3 Transfer & Loan Requests (`backend/internal/player/transfer_request.go`)
- API `POST /api/player/me/transfer-request`:
  - Request type: `permanent_transfer` OR `loan_move`.
  - Target preference: `bigger_club`, `more_playing_time`, `specific_league`.
  - Sets player status `transfer_listed = true`; lowers morale by -0.15; triggers `Dressing Room Faction` reaction.

### 2.4 Public Social Media Statements (`backend/internal/player/social_statements.go`)
- API `POST /api/player/me/statement`:
  - Statement categories:
    - `express_loyalty`: Supporter Sentiment +10, Manager Trust +5.
    - `complain_playing_time`: Manager Relationship -15, Faction Alignment +10.
    - `tease_transfer`: Fan Sentiment -10, Transfer Market Interest +20%.
  - Emits news story in `world.news_stories` (category `fan_reaction`).

### 2.5 Personal Lifestyle & Sponsorships (`backend/internal/player/lifestyle.go`)
- Track player personal cash earnings (separate from club ledger):
  - Personal sponsorships (Boots deal, Brand Endorsement).
  - Spending options: `hire_personal_physio` (+10% recovery rate), `hire_media_manager` (+10% fan sentiment), `nightlife_partying` (-10% stamina, risk of scandal arc).

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/player/agent_test.go`:
  - Test agent contract commission deductions and negotiation modifiers.
  - Test contract demand validation (starter role breach triggers).
  - Test public statement news generation and sentiment shifts.
- Run `go test ./internal/player/...`.

### 3.2 Verification Commands
```bash
gofmt -w internal/player/ internal/httpapi/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/player/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] Agent hiring/firing engine implemented
- [ ] Contract demand conditions (starter role, release clause) verified
- [ ] Transfer and loan request workflows functioning
- [ ] Public social media statements generating news and sentiment shifts
- [ ] Personal lifestyle & sponsorship mechanics verified
