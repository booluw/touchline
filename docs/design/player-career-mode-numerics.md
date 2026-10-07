# User-as-Player Career Mode Design Ledger

This document is the authoritative design specification for Touchline's **User-as-Player Career Mode (Be-A-Pro)** under the Phase 4 Multi-Role Actor Framework (`internal/actor`).

---

## 1. Product Concept & Architecture

In Touchline, users can sign up or create an account as a **Player Actor**. Human Players exist in the exact same persistent world as Human Managers and AI Managers.

```
                         ┌─────────────────────────────────┐
                         │         Touchline World         │
                         └─────────────────────────────────┘
                                    │           │
                 ┌──────────────────┘           └──────────────────┐
                 ▼                                                 ▼
      Human Manager Account                             Human Player Account
      - Tactics & Squad Lineup                          - Individual Match Rating
      - Contract Offers & Wages                         - Energy & Personal Morale
      - Benching Decisions ────────────────────────────► - Transfer Requests & Sulking
```

---

## 2. Player Actor Lifecycle & Archetypes

### 2.1 Player Character Creation (`actor.CreatePlayerProfile`)

When creating a Player Actor, the user selects:
1. **Primary Position**: (e.g. `ST`, `CAM`, `CM`, `CB`, `GK`).
2. **Player Style / Archetype**:
   - *Target Man / Power Striker*: High Strength, Heading, Shot Power.
   - *Pacy Winger / Dribbler*: High Acceleration, Pace, Agility, Dribbling.
   - *Playmaker / Maestro*: High Passing, Vision, Technique, Composure.
   - *Ball-Winning Defender*: High Tackling, Marking, Workrate, Aggression.
3. **Starting Age**: 17–19 years old (placed into a Club Academy or Free Agent pool).
4. **Agent Personality**:
   - *Money-Oriented Agent*: Prioritizes high signing bonuses and wage demands.
   - *Career-Oriented Agent*: Prioritizes game time, continental competitions, and international call-ups.

---

## 3. Position-Locked Match Rating & Performance

During live match simulation ([Chapter 15](file:///Users/bfree/Desktop/booluw/touchline/docs/touchline-book/15-match-engine.md)), a Human Player receives a **Dynamic Match Rating (0.0 to 10.0)** updated minute-by-minute:

```
MatchRating = 6.0 + PositionImpactBonus - MistakesPenalty + TacticalDisciplineBonus
```

### Position Rating Factors:
- **Forwards (`ST`, `LW`, `RW`)**: Goals (+1.5), Shots on Target (+0.3), Key Passes (+0.4), Offside (-0.2), Missed Big Chance (-0.5).
- **Midfielders (`CM`, `CAM`, `DM`)**: Pass Completion % (+0.5 for >85%), Assists (+1.2), Interceptions (+0.4), Dispossessed (-0.3).
- **Defenders (`CB`, `LB`, `RB`)**: Successful Tackles (+0.4), Clean Sheet (+1.0), Aerial Duels Won (+0.3), Foul Conceded (-0.3), Own Goal (-2.0).

---

## 4. Human Manager ↔ Human Player Interpersonal Dynamics

When a Human Manager and a Human Player belong to the same club:

1. **Lineup & Benching Conflicts**:
   - If a Human Manager benches a Human Player for 3 consecutive matches, the Human Player receives a **Dilemma Prompt**:
     - *Option A (Request Transfer)*: Submits formal transfer request; Morale drops to 0.30; triggers Dressing Room Faction Split.
     - *Option B (Confront Manager in Private)*: Sends an in-game direct message / prompt to the Human Manager.
     - *Option C (Work Harder in Training)*: Consumes extra energy (-15%), boosts training performance by +10%.

2. **Contract & Wage Negotiations**:
   - Contract offers from Human Managers are submitted directly to the Human Player's inbox.
   - The Human Player accepts, rejects, or counters wage demands asynchronously.

---

## 5. Player Energy, Attribute Development & International Caps

### 5.1 Energy & Training Load
- **Stamina/Energy Pool**: 0–100%. Matches consume 20–35% energy based on work rate.
- **Weekly Focus**: Player selects training focus (*Physical*, *Technical*, *Tactical*, *Mental*).
- **Attribute Growth**: Attributes increase incrementally based on training focus + match rating performance.

### 5.2 National Team Selection
- Player actors with average match rating ≥ **7.5** over 10 games become eligible for National Team call-ups during International Breaks ([Chapter 30](file:///Users/bfree/Desktop/booluw/touchline/docs/touchline-book/30-roadmap-and-open-decisions.md)).

---

## 6. Player Off-Pitch Actions & Agent Mechanics

1. **Agent Selection**:
   - `money_oriented`: Demands +30% higher wages and agent commission (5%).
   - `career_driven`: Prioritizes continental competition clauses and starter guarantees.
   - `loyal_family`: Low commission (2%), prioritizes long-term contract stability.

2. **Public Social Media Statements**:
   - `express_loyalty`: Supporter Sentiment +10, Manager Trust +5.
   - `complain_playing_time`: Manager Relationship -15, Faction Alignment +10.
   - `tease_transfer`: Fan Sentiment -10, Transfer Interest +20%.

3. **Personal Lifestyle & Sponsorships**:
   - Sponsorship income credited to player's personal wallet.
   - Personal investments: `hire_personal_physio` (+10% recovery rate), `hire_media_manager` (+10% fan sentiment), `nightlife_partying` (-10% stamina, risk of scandal arc).

---

## 7. In-Match Interactive Actions & Reactions

During match simulation:
- **Pre-Match Objectives**: Player sets goal (`score_goal`, `provide_assist`, `pass_accuracy_90`, `clean_sheet`). Success adds +0.5 to rating.
- **Call for Ball**: Teammates +25% pass probability to player; dispossessed drops composure -5%.
- **Argue with Referee**: 60% chance ref leniency (-1 foul severity), 40% yellow card risk.
- **Goal Celebration**:
  - `passionate_badge_clap`: Supporter Sentiment +5.
  - `shush_opposing_fans`: Rival Hostility +25, Yellow Card risk +10%.
  - `dedicate_to_manager`: Manager Relationship +10.

---

## 8. Career Awards & Post-Retirement Transition

1. **Seasonal Awards**:
   - Evaluated at season rollover: Golden Boot, Playmaker of the Year, Player of the Season, World Footballer of the Year (Ballon d'Or).
2. **Post-Retirement Options**:
   - Ages 34–38: Player actor retires and chooses post-career path:
     - **Human Manager**: Transition to managing a club.
     - **Club Scout**: Transition to scouting player talent.
     - **Player Agent**: Transition to agent representing player actors.

---

## 9. Table of Constants & Parameters

| Constant Name | Value | Purpose |
| --- | --- | --- |
| `DefaultPlayerBaseRating` | `6.0` | Base match rating starting score |
| `BenchedMatchThreshold` | `3` | Consecutive benched matches triggering player dilemma |
| `PlayerEnergyMatchCostMin` | `20` | Minimum energy cost per 90-minute match |
| `PlayerEnergyMatchCostMax` | `35` | Maximum energy cost per 90-minute match |
| `InternationalCallUpRatingThreshold` | `7.5` | 10-match rating average threshold for national team call-up |
| `RefArgumentYellowCardRisk` | `0.40` | Probability of receiving yellow card when arguing with ref |

---
*Source: Touchline Architecture, `docs/touchline-book/10-players.md`, `docs/touchline-book/18-morale-and-transfer-requests.md`.*
