# Retired Player to Manager Archetype Design Ledger

This document defines the managerial career progression, tactical inheritance, and board affinity mechanics for **Retired Players Turning Managers** in Touchline, modeled on real-life player-to-manager trajectories such as **Pep Guardiola**, **Xabi Alonso**, and **Cesc Fàbregas**.

---

## 1. Real-World Case Studies & Analytical Framework

```
                          ┌─────────────────────────────────────┐
                          │   Active Player Career Highlights   │
                          └─────────────────────────────────────┘
                                             │
             ┌───────────────────────────────┼───────────────────────────────┐
             ▼                               ▼                               ▼
    Pep Guardiola Arc                Xabi Alonso Arc               Cesc Fàbregas Arc
    - Barcelona Club Captain         - Sociedad / Madrid / Bayern  - Arsenal / Barca / Como
    - Learned Cruyffian DNA          - Tactical Mastery under Pep    - Como Player-Owner / Youth
    - Barcelona B → First Team       - Madrid Youth → Sociedad B     - Como Interim → Serie A
    - Immortalized Club Legend       - Leverkusen Unbeaten Double    - High Former-Club Affinity
```

### Key Real-World Invariants Identified:

1. **Former-Club Hiring Preference ("The Prodigal Son Affinity")**:
   - Clubs strongly favor hiring former players who represented the badge. Boards give higher tolerance for lack of senior managerial experience, and fans grant an immediate trust buffer.
2. **Tactical DNA Inheritance**:
   - A player's managerial tactical style is shaped by the tactical systems they played under during their playing career (e.g. Positional Play / Tiki-Taka, High-Press Counter-Attack, Fluid Possession).
3. **Apprenticeship & Low-Pressure Stepping Stones**:
   - Top players rarely jump straight into elite first-team roles without a stepping stone (Reserve/B-Team management, U19s, or taking over a lower-tier / struggling club mid-season).
4. **Elevated Stakes & Emotional Volatility**:
   - Managing a former club carries extreme emotional stakes: success creates an immortalized legend status, while failure damages career reputation and supporter relationships far more than managing a neutral club.

---

## 2. Mathematical Formulas & Engine Mechanics

### 2.1 Former-Club Hiring Affinity Score (`manager.EvaluateFormerClubAffinity`)

When a retired player (AI or Human) applies for a managerial vacancy at a club they previously played for:

```
AffinityScore = BaseAffinity + (CapsAtClub * 0.5) + TrophiesWonBonus + LegendStatusBonus
```

- **Base Affinity**: +15 points for any former club.
- **Caps at Club**: +0.5 per 10 appearances (max +20).
- **Trophies Won as Player**: +10 per major trophy won at the club.
- **Captain / Legend Bonus**: +15 if former club captain or overall rating ≥ 85.

#### Hiring & Board Evaluation Impact:
- **Board Negotiation Tolerance**: Persona tolerance increases by **+2** (e.g., a `demanding_owner` acts with `patient_owner` leeway).
- **Minimum Reputation Requirement**: Job offer reputation threshold reduced by **-25%** for former players.
- **Supporter Sentiment Starting Floor**: Initial supporter sentiment set to **70** (Content/Delighted) instead of default 50.

---

### 2.2 Tactical DNA Inheritance (`manager.InheritTacticalDNA`)

Upon transitioning from player to manager, the engine calculates the manager's initial **Tactical Preference**:

```go
type TacticalDNA struct {
    PrimaryFormation   string  `json:"primary_formation"`
    PassingStyle       string  `json:"passing_style"`       // short_possession | direct_transition | balanced
    PressingIntensity  int     `json:"pressing_intensity"`  // 1-100
    AttackingWidth     string  `json:"attacking_width"`     // wide | narrow | fluid
}
```

- If played >150 matches under a `short_possession` manager (e.g. Guardiola/Cruyff style) → Manager inherits `short_possession` preference with +10% tactical familiarity bonus when running 4-3-3 / 3-4-2-1.
- If played >150 matches under a `high_press_transition` manager (e.g. Klopp/Alonso style) → Manager inherits `high_press_transition` preference.

---

## 3. The `prodigal_son_manager_return` Narrative Arc

When a retired player accepts a head coach job at a former club, the Storyline Engine triggers the **Prodigal Son Manager Return Arc**:

```
[Job Accepted at Former Club] ──► [STAGE 1: HERO'S WELCOME]
                                         │
                                  (Pre-Season / Debut)
                                         ▼
                                 [STAGE 2: HONEYMOON PERIOD]
                                         │
                                  (First 10 Matches)
                                         ▼
                                 [STAGE 3: CLIMAX & JUDGEMENT]
                                 - Legendary Status (Title / Cup Win)
                                 - Brutal Fall (Sacked mid-season)
```

### 3.1 Arc Dilemmas & Events:

1. **Stage 1 Dilemma: "Addressing Former Teammates in the Squad"**
   - *Context*: Manager is now coaching former teammates/peers in the dressing room.
   - *Option A (Assert Absolute Authority)*:
     - Board Discipline factor +15; Teammate Morale -0.10; Faction alignment neutral.
   - *Option B (Collaborative Captain-Coach Stance)*:
     - Teammate Morale +0.15; Faction alignment +20%; Board Discipline factor -10.

2. **Stage 3 Resolution (Legend vs Tragic Fall)**:
   - **Legendary Finish**: Meeting or exceeding board mandate triggers `LEGENDARY_RETURN_CELEBRATION` news story; Manager Reputation +25; Supporter Loyalty locked to 90+.
   - **Tragic Sacking**: Sacking at former club causes Manager Reputation penalty (-20 points instead of standard -10) and fan heartbreak news stories.

---

## 4. Table of Constants & Parameters

| Constant Name | Value | Purpose |
| --- | --- | --- |
| `FormerClubReputationDiscount` | `0.25` | 25% lower reputation barrier for former players |
| `FormerClubBoardToleranceBonus` | `2` | Bonus board negotiation tolerance level |
| `FormerClubStartingSentiment` | `70` | Starting supporter sentiment for former player manager |
| `ProdigalSonSackRepPenalty` | `-20` | Manager reputation penalty if sacked by former club |

---
*Source: Touchline Architecture, `docs/touchline-book/22-managers-and-job-offers.md`, `docs/design/storyline-numerics.md`.*
