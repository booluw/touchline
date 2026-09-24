# IMP-01 — Deterministic Competition Prize Pools, Fair Play Awards, and Board Budget Integration

**Status:** Approved Specification  
**Sprint:** Improvements / Post-MVP Financial & Competition Engine  
**Source:** PRD §§33, 36, 67, 74; Technical Plan §§6, 12, 16; `product_manager.md` (OPD-03, OPD-09)  
**Depends on:** S04-01, S05-02, S10-02  
**Key files:** `backend/internal/competition`, `backend/internal/finance`, `backend/internal/club`, `backend/pkg/matchsim`

---

## 1. Executive Product Overview

This specification introduces a deterministic competition prize pool model for domestic leagues and custom club tournaments, an automated Fair Play discipline award system, and an append-only financial ledger integration that feeds next season's board budgets.

The key product goals are:
1. **Realistic League Economy:** Prize money scales dynamically based on league quality (player attributes, stadium/facility levels, competition reputation).
2. **Fair Play & Discipline Incentives:** Teams maintaining clean discipline records are rewarded with a dedicated `Fair Play Prize` equal to the average merit payout of the bottom 5 clubs.
3. **Financial Transparency & Board Governance:** All payouts are recorded as immutable credit rows in `finance.ledger_entries`, and a board-governed percentage transitions into the next season's transfer and wage budget.

---

## 2. Technical & Mathematical Specification

### A. Domestic League Prize Pool Calculation
For standard domestic leagues, the total season prize pool $P_{\text{total}}$ is calculated deterministically at season launch using league quality indicators:

$$P_{\text{total}} = P_{\text{base}}(\text{Tier}) \times \left(1 + w_p \cdot \frac{\overline{\text{PlayerQuality}}}{100} + w_f \cdot \frac{\overline{\text{FacilityScore}}}{100} + w_r \cdot \frac{\text{LeagueReputation}}{100}\right)$$

Where:
- $P_{\text{base}}(\text{Tier})$: Base tier valuation defined in `world.world_config` (e.g. Tier 1 = $50,000,000, Tier 2 = $15,000,000).
- $\overline{\text{PlayerQuality}}$: Mean current overall attribute rating across all registered squad players in the league.
- $\overline{\text{FacilityScore}}$: Mean training & stadium facility level across participating clubs.
- $\text{LeagueReputation}$: Competition prestige score ($1 - 100$).
- Weights: $w_p = 0.5$, $w_f = 0.25$, $w_r = 0.25$ (configurable in `competition_rules`).

### B. Prize Pool Division & Merit Quotas
The total prize pool $P_{\text{total}}$ is divided into two distinct buckets:
1. **Merit Performance Pool ($P_{\text{merit}}$):** $90\%$ of $P_{\text{total}}$ (or remaining pool after Fair Play allocation).
2. **Fair Play Pool ($P_{\text{fairplay}}$):** Allocated to the top-ranked Fair Play club(s).

#### Merit Distribution Formula (N-Team League)
For an $N$-team league, each position $k \in \{1, 2, \dots, N\}$ receives a merit percentage $Q(k)$ based on final standings:

$$Q(k) = \frac{(N - k + 1)^\gamma}{\sum_{j=1}^{N} j^\gamma}$$

- $\gamma = 1.5$ for Tier 1 leagues (top-heavy distribution reward for European/title contenders).
- $\gamma = 1.0$ for lower tiers (equitable linear distribution).
- Payout for rank $k$: $\text{MeritPrize}(k) = P_{\text{merit}} \times Q(k)$.

---

### C. Custom / Manager-Created Club Competitions
For manager-created tournaments (`S10-02`):
- **Declared Prize Pool:** Explicitly set during tournament creation (`competition.competitions.declared_prize_pool`) and funded upfront by entry fees or organizer capital.
- **Knockout Quotas:** Distributed via fixed stage rules (e.g. Winner 40%, Runner-up 20%, Semi-finalists 10% each, Group Stage exit 5% each).
- **Fair Play Toggle (Approved Decision):** Configurable by the tournament creator during creation (Enabled by default; allocates $5\%$ of total declared pool to the Fair Play Award).

---

### D. Fair Play Discipline System & Prize Calibration

#### Disciplinary Point Scoring
During all competition matches, disciplinary actions increment a team's **Fair Play Points ($FPP$)**:
- **Yellow Card:** $+1 \text{ FPP}$
- **Second Yellow (Red):** $+3 \text{ FPP}$
- **Straight Red Card:** $+5 \text{ FPP}$
- **Manager Touchline Warning/Dismissal:** $+3 \text{ FPP}$

#### Fair Play Ranking & Tie-Breakers (Approved Decision)
Clubs are ranked in ascending order of $FPP$ (lowest $FPP$ = $1^{\text{st}}$ place in Fair Play).
In case of equal $FPP$, tie-breakers apply in order:
1. Fewest Straight Red Cards
2. Fewest Total Yellow Cards
3. Higher Final League Table Standing
4. Deterministic Seeded Draw (`(world_id, competition_id, seed)`)
*Note:* The Fair Play cash prize is split equally among tied teams **only** if all 4 tie-breakers are 100% identical.

#### Fair Play Award Amount Calibration
As mandated by product leadership:
$$\text{FairPlayPrize} = \frac{1}{5} \sum_{k=N-4}^{N} \text{MeritPrize}(k)$$

The Fair Play award amount is set **equal to the average merit prize of the bottom 5 finishing clubs**.
- The Fair Play winner receives this cash award credited as `FAIR_PLAY_PRIZE`.
- In a 20-team league, if the average payout for positions 16–20 is $1,200,000, the Fair Play winner receives $1,200,000.
- The amount is deducted from $P_{\text{total}}$ before $P_{\text{merit}}$ is calculated, or supplemented from competition reserve funds.

#### Disciplinary Fine for Reckless Play (Approved Decision)
Any team accumulating total disciplinary points exceeding **$2\times$ the league average $FPP$** incurs a **$5\%$ merit prize penalty fine**.
- The fine is deducted directly from their merit payout as `entry_type: 'DISCIPLINARY_FINE'`.
- Deducted fines are added to the league's Fair Play Prize pool for subsequent seasons.

---

### E. Financial Ledger & Board Budget Integration

#### 1. Immutable Ledger Entries
Upon seasonal completion, the competition engine appends immutable credit/debit rows to `finance.ledger_entries`:
- `entry_type`: `'COMPETITION_PRIZE_MERIT'`, `'COMPETITION_PRIZE_FAIR_PLAY'`, or `'DISCIPLINARY_FINE'`
- `amount`: Exact calculated monetary award / fine
- `description`: `"Premier League Finish Rank #3 Merit Award"` / `"Premier League Fair Play Season Award"`
- `metadata`: `{"competition_id": "...", "season": 2026, "rank": 3, "fpp": 24}`

#### 2. Board Budget Allocation & Next-Season Transfer Budget
At the end-of-season financial rollover tick:
- **Board Financial Policy Evaluation:** The board reviews total prize money earned ($M_{\text{total}} = \text{MeritPrize} + \text{FairPlayPrize}$) alongside current cash reserves and club debt.
- **Reinvestment Percentage ($\alpha$):**
  - **Standard Club (Healthy Cash):** Board allocates $\alpha = 70\%$ of prize earnings directly into the **Next Season Transfer & Wage Budget**, retaining $30\%$ in Cash Reserves.
  - **Ambitious / Sugar-Daddy Club DNA:** $\alpha = 90\%$ reinvested into transfer budget.
  - **Debt-Burdened / Financially Insolvent Club:** $\alpha = 20\%$ reinvested; $80\%$ retained to service debt and prevent bankruptcy.
- **Budget Increment Formula:**
  $$\text{NextSeasonTransferBudget} = \text{BaseBudget} + (\alpha \times M_{\text{total}})$$

---

## 3. Approved Product Enhancements & UX Directives

1. **Board Confidence Boost:** Winning the Fair Play Prize grants an explicit **$+3\%$ Board Confidence boost** under Brand & Public Reputation mandates, surfaced via `Explanation` breakdowns.
2. **Fair Play Trophy / Badge:** A silver **"Fair Play Winner" badge** is rendered on the club profile UI, competition history view, and manager career history page.
3. **Automated Media Story:** The news generator (`S09-03`) automatically publishes a dedicated news story celebrating the Fair Play Award winner alongside the League Champion at season completion.

---

## 4. Acceptance Criteria

1. **Deterministic League Prize Pool:**
   - Seasonal competition processing calculates $P_{\text{total}}$ deterministically from tier base value, average player quality, facility levels, and league reputation without hardcoded magic numbers.
   - Payouts reconcile byte-for-byte under replay testing (`(world_id, season, seed)`).

2. **Merit & Fair Play Distribution:**
   - Every participating club receives their merit prize according to final league standing.
   - Fair Play points ($FPP$) track yellow cards (+1), second yellows (+3), straight reds (+5), and manager warnings (+3).
   - The Fair Play winner is correctly identified using specified tie-breakers and receives an award equal to the average merit prize of the bottom 5 clubs.
   - Teams exceeding $2\times$ league average $FPP$ incur a $5\%$ merit prize fine.

3. **Append-Only Financial Ledger:**
   - Merit, Fair Play, and fine transactions generate append-only rows in `finance.ledger_entries`.
   - `SUM(ledger_entries)` matches reported financial balances exactly; zero mutable balance column edits occur.

4. **Board Budget & Confidence Rollover:**
   - Board financial policy determines reinvestment percentage $\alpha$ based on Club DNA and debt status.
   - Winning Fair Play grants $+3\%$ Board Confidence logged with explicit `Explanation` factors.

5. **API & UI Visibility:**
   - `GET /api/competitions/:id/standings` returns Fair Play standings alongside standard league tables.
   - Club and manager profile views render the Fair Play Winner silver badge.
   - `GET /api/finance/summary` displays prize money earned and projected budget rollover.

---

## 5. Delivery Evidence

- Pending.
