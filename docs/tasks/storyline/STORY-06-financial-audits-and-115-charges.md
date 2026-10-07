# STORY-06 — Financial Audits, PSR Points Deductions & The "115 Charges" Arc

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline & Emergent Narrative Engine)  
**Source:** User request 2026-10-07, `docs/design/financial-sustainability-and-ffp.md`  
**Depends on:** STORY-01, S05-02 (finance ledger), S06-02 (board confidence)  

---

## 1. Goal

Implement the 3-Season Rolling PSR Audit engine (`finance.EvaluatePSR`), creative accounting dilemmas, Independent Commission Investigation trial arcs, and automated league standings points deductions.

---

## 2. Requirements & Acceptance Criteria

### 2.1 3-Season PSR Audit Engine (`backend/internal/finance/ffp/audit.go`)

- Implement `AuditClubPSR(ctx context.Context, worldID string, seasonNumber int, clubID string) (*AuditResult, error)`:
  - Query sum of operating debits over trailing 3 seasons from `finance.ledger_entries`.
  - Compare total operating loss against league tier spending cap ($35M Tier 1, $20M Tier 2, $10M Tier 3).
  - If loss > cap OR `audit_risk_score >= 60%`, initiate **Independent Commission Trial Arc**.

### 2.2 Creative Accounting Dilemmas (`backend/internal/storyline/dilemmas/creative_accounting.go`)
- Generate dilemma when cash reserves < $2M or committed wage ratio > 90%:
  - *Option 1 (Inflated Owner Sponsorship)*: Adds +$20M transfer budget; increases `audit_risk_score` by +35%.
  - *Option 2 (Off-Book Agent Commission)*: Reduces reported wage bill on ledger by 25%; increases `audit_risk_score` by +40%.

### 2.3 Independent Commission Investigation Arc (`backend/internal/storyline/archetypes/charges_investigation.go`)
- 4-stage story arc:
  - **Stage 1 (Audit Flag)**: Publish league notification news story; Board Relationship factor drops -15%.
  - **Stage 2 (Trial)**: Present manager defense dilemma (Elite Legal Counsel vs Plead Guilty vs Challenge League).
  - **Stage 3 (Verdict & Sanctions)**: Apply points deduction penalty (-6 to -12 points) to `league.standings.points_deducted`, impose transfer embargo, emit `announcement` news story.
  - **Stage 4 (Legal Appeal)**: Process legal appeal reduction if defense counsel was hired.

### 2.4 Standings Integration (`backend/internal/league/standings.go`)
- Update `GET /api/competitions/:id/standings`:
  - Calculate `effective_points = points - points_deducted`.
  - Include breakdown explaining points deduction penalties.

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/finance/ffp/audit_test.go`:
  - Test 3-season PSR loss calculation over ledger entries.
  - Test creative accounting risk accumulation.
  - Test points deduction application to standings.
- Run `go test ./internal/finance/... ./internal/storyline/...`.

### 3.2 Verification Commands
```bash
gofmt -w internal/finance/ffp/ internal/storyline/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/finance/... ./internal/storyline/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] PSR audit engine implemented & tested
- [ ] Creative accounting dilemma options wired
- [ ] Independent Commission Trial arc state machine implemented
- [ ] Standings points deduction application verified
- [ ] Verification tests passing clean
