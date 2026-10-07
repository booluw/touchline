# Storyline Engine & Emergent Narrative Numerics Ledger

This document is the authoritative numerics and state-machine specification for Touchline's Storyline Engine (`internal/storyline`). It defines the dynamic arc evaluation triggers, dilemma weighting, mechanical consequence calculations, mind-game modifiers, and season chronicle synthesis algorithms.

---

## 1. Core Principles & Philosophy

1. **Emergent over Scripted**: Storylines in Touchline are state-driven narrative arcs triggered by real game events (match outcomes, morale drops, derby fixtures, transfer requests).
2. **Server-Authoritative & Deterministic**: Arc triggers and dilemma generators use seeded RNG derived from `(world_id, world_tick, entity_id)` to maintain determinism.
3. **Async-First Choice Architecture**: Dilemmas do not pause the world clock; they publish actionable items to the manager's Dashboard (`GET /api/dashboard`). Unresolved dilemmas resolve via staff/PolicyBot defaults at the next tick.
4. **Mechanical Consequences**: Choices carry explicit, explainable trade-offs across Morale, Board Confidence, Supporter Sentiment, Finance, and Card/Tactical modifiers.

---

## 2. Story Arc State Machine & Schemas

### 2.1 State Transitions

Every active storyline operates as a state machine:

```
[DORMANT] ──(Trigger Evaluation)──► [TRIGGERED]
                                        │
                                 (Tick / Match Event)
                                        ▼
                                  [ESCALATING] ──(Dilemma Prompt)
                                        │
                                 (Choice Made / Climax Match)
                                        ▼
                                    [CLIMAX]
                                        │
                                 (Post-Match / Settlement)
                                        ▼
                                   [RESOLVED]
```

- **Dormant**: Arc is inactive; checked during cadence passes.
- **Triggered**: Pattern matched; initial news story / notification emitted.
- **Escalating**: Arc progresses across 1–3 world weeks; dilemmas may be presented.
- **Climax**: High-stakes resolution point (e.g. key match day, deadline day).
- **Resolved**: Cooldown period (default 30 world days per arc type per club).

---

## 3. The 5 Initial Arc Archetypes & Numerics

### 3.1 Archetype 1: `underdog_miracle` (Giant-Killing / Unbeaten Run)

- **Trigger Conditions**:
  - `club.reputation` ≤ **60** AND
  - Unbeaten run of ≥ **4 league games** OR victory against a top-3 team with `reputation` ≥ **club.reputation + 20**.
- **Escalation Logic**:
  - Media spotlight active for **14 world days**.
  - Player Overconfidence Risk: `overconfidence_factor = clamp((unbeaten_matches - 3) * 0.05, 0.0, 0.20)`.
  - Tactical penalty: Opponents get +5% tactical familiarity against underdog as match preview scouting reports analyze the streak.
- **Dilemma (Mid-Streak)**: "Managing Media Hype"
  - *Option A (Embrace Hype)*: Supporter Sentiment +10; Morale +0.05; Overconfidence +0.10.
  - *Option B (Downplay Expectations)*: Supporter Sentiment -5; Overconfidence -0.10; Tactical Focus +5%.
- **Resolution**: End of streak or season conclusion; emits `STORY_ARC_COMPLETED` event.

---

### 3.2 Archetype 2: `mutiny_faction_split` (Dressing Room Rebellion)

- **Trigger Conditions**:
  - squad leader / influencer player (`internal/squad/influencers.go`) has `morale` ≤ **0.35** AND
  - `faction_alignment_strength` ≥ **0.40** (at least 3 other players in same faction with morale ≤ 0.45).
- **Escalation Logic**:
  - Faction Morale Decay: Faction members lose **-0.02 morale per world tick** while unresolved.
  - Leaked to Media: `fan_reaction` story published after 3 ticks of inaction.
- **Dilemma**: "The Captain's Ultimatum"
  - *Option A (Back Manager / Bench Ringleader)*:
    - Ringleader morale: -0.20 (enters transfer request).
    - Opposing faction morale: +0.10.
    - Faction members morale: -0.10.
    - Board Discipline factor: +15.
  - *Option B (Fine Ringleader)*:
    - Ringleader morale: -0.30.
    - Board Financial factor: +5.
    - Supporter Sentiment: +5 (likes tough manager).
  - *Option C (Capitulate & Promise Playing Time/Transfer)*:
    - Ringleader morale: +0.25.
    - Faction members morale: +0.05.
    - Board Relationship factor: -10.

---

### 3.3 Archetype 3: `ex_factor_revenge` (Former Player / Manager Clash)

- **Trigger Conditions**:
  - Upcoming fixture against opponent featuring a former starter player (sold/released within last 365 world days) OR former manager.
- **Escalation Logic**:
  - Pre-match build-up news story published 48h before match.
  - Ex-Player Motivation Floor: Ex-player match performance expectation +10%; condition decay -5%.
- **Dilemma**: "Pre-Match Press Stance on Ex-Star"
  - *Option A (Target & Pressure Ex-Player)*:
    - Opponent ex-player composure -10%; yellow card risk +20%.
    - Own team fouls/cards risk +15%.
  - *Option B (Graciously Praise Ex-Player)*:
    - Supporter Sentiment neutral; own team composure +5%.
- **Resolution**: Evaluated immediately after fixture completion transaction.

---

### 3.4 Archetype 4: `academy_local_hero` (Youngster Breakout)

- **Trigger Conditions**:
  - Player age ≤ **20**, `is_homegrown` = `true`, AND
  - Earns `match_rating` ≥ **75** in 3 consecutive starts.
- **Escalation Logic**:
  - Supporter Sentiment boost: +0.5 per rating point above 75.
  - Transfer Interest: Top-tier clubs automatically add player to transfer shortlists.
- **Dilemma**: "Contract Renewal vs Transfer Interest"
  - *Option A (Offer Improved Long-Term Contract)*:
    - Player morale: +0.20.
    - Weekly wage budget committed: +15-30%.
    - Board Financial factor: adjusted by wage delta.
  - *Option B (Hold Firm on Current Deal)*:
    - Player morale: -0.15; Agent request filed.
- **Resolution**: Contract signed or player sold.

---

### 3.5 Archetype 5: `great_escape_relegation` (Relegation Battle Climax)

- **Trigger Conditions**:
  - `league_position` in bottom **3** AND
  - Remaining league fixtures ≤ **8**.
- **Escalation Logic**:
  - Board Review Cadence: Confidence evaluated every **2 fixtures** instead of monthly.
  - Match Intensity: Squad motivation floor raised to **65** for relegation battles.
- **Dilemma**: "Survival Bonus / Training Intensity"
  - *Option A (Offer Win Bonuses from Cash)*:
    - Cash cost: £50,000 per win.
    - Squad morale floor: +0.10.
  - *Option B (Double Training Load)*:
    - Match performance: +5%.
    - Injury probability multiplier: **x1.40**.
- **Resolution**: Relegation avoided or club relegated at season rollover.

---

## 4. Mind Games & Pre-Match Tactical Stances

Before key fixtures (Derbies, Top-4 Clash, Cup Knockouts, Human vs Human), managers can select a **Pre-Match Tactical Stance**:

| Stance | Squad Performance Mod | Card/Foul Risk Mod | Fan Sentiment Mod | Opponent Composure Mod |
| --- | --- | --- | --- | --- |
| `aggressive_combative` | +4% Tackling / Workrate | +25% Yellow Cards | +10 (in Derbies) | -5% Composure |
| `underdog_deflect_pressure` | +5% Composure floor | -10% Yellow Cards | -5 (Lowers expectations) | Neutral |
| `clinical_focused` | +3% Tactical Familiarity | Neutral | Neutral | Neutral |

---

## 5. Season Storybook Digest Generator Algorithm

At season rollover ([Ch. 7](file:///Users/bfree/Desktop/booluw/touchline/docs/touchline-book/07-seasons-and-rollover.md)), the engine generates a structured `SeasonChronicle` object stored in `story.chronicles`:

```go
type SeasonChronicle struct {
    SeasonNumber    int          `json:"season_number"`
    ClubID          string       `json:"club_id"`
    ManagerID       string       `json:"manager_id"`
    Headline        string       `json:"headline"`
    LeagueFinish    int          `json:"league_finish"`
    TargetFinish    int          `json:"target_finish"`
    KeyStoryArcs    []string     `json:"key_story_arcs"`
    BreakoutPlayer  string       `json:"breakout_player_name"`
    BiggestWin      MatchSummary `json:"biggest_win"`
    BitterestLoss   MatchSummary `json:"bitterest_loss"`
    ChronicleDigest string       `json:"chronicle_digest"`
}
```

- **Headline Generation Rules**:
  - `position == 1` → "Champions of [LeagueName]: A Season of Glory"
  - `position <= target_finish` → "Promises Kept: Mandates Fulfilled at [ClubName]"
  - `position > target_finish + 4` → "Stormy Waters: A Season of Disappointment and Tension"
  - `relegated == true` → "Heartbreak and Rebuilding: The Fall from Tier [Tier]"

---

## 6. Table of Constants & Tuning Parameters

| Constant Name | Value | Purpose |
| --- | --- | --- |
| `MaxActiveStoryArcsPerClub` | `2` | Prevents dilemma overload |
| `StoryArcCooldownDays` | `30` | Cooldown before same arc can re-trigger |
| `DilemmaExpirationTicks` | `7` | Days before an un-responded dilemma auto-resolves |
| `DefaultRingleaderMoralePenalty` | `-0.20` | Morale hit for benched mutiny leader |
| `PreMatchMindGameCardRiskMax` | `1.25` | Maximum card risk multiplier from aggressive stance |
| `AcademyBreakoutRatingThreshold` | `75` | Match rating threshold for local hero arc |

---
*Source: Touchline Product Architecture, `docs/touchline-book/the-touchline-book.md`, `docs/design/board-numerics.md`.*
