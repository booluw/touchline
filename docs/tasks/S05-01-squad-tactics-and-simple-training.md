# S05-01 — Implement MVP squad, tactics, and simple training commands

**Status:** Not started  
**Sprint:** 05 — Manager controls and finance  
**Source:** PRD §§3–4, 26, 52, 55–56, 73–74; technical plan §§10, 12, 16; matchsim Addendum v1.2/v1.3  
**Depends on:** S04-02  
**Key files:** `backend/internal/match`, `backend/internal/player`, `backend/pkg/matchsim`, `frontend/pages/squad/tactics.vue`, `frontend/pages/squad/training.vue`

## What to do

Deliver the server-side commands, domain services, and responsive management screens for squad selection, approved MVP tactics ("Simple Mode"), and simple training routines. Support deadline-based submissions, live match tactic changes via `LiveInputs` (replaying `seed + ordered inputs`), training tick handlers, and future PolicyBot delegation reuse.

## Acceptance criteria

- Authorized managers can view their squad and submit validated squad lineups, tactical setups, and training-plan commands through documented REST APIs (`POST /api/clubs/:id/tactics`, `POST /api/clubs/:id/training-plan`).
- Commands are persisted in database schemas (`club`, `player`, `match`), emitted to `world.events`, world-scoped, and validated on the server against ownership and matchday deadline rules.
- Simple Mode tactical controls conform strictly to the product-approved 5-style tactical specification below.
- Simple Mode training plans execute weekly via subscribed tick handlers, updating player attributes, fatigue, fitness condition, match sharpness, and injury risk according to the Training Archetype Matrix below.
- The orchestration layer (`internal/match`) translates manager tactical setups into effective `Attack`/`Defense`, possession modulation, and stamina multipliers before calling `matchsim.Simulate()`.
- Live in-match tactical changes submit ordered `LiveInput` commands (`kind: "tactical_change"`) consumed by `matchsim.Simulate(Options{LiveInputs: ...})` to ensure 100% deterministic replay.
- The command layer executes identically whether invoked by a human manager or `PolicyBot` actor without separate business logic.

---

## Tactical Engine Specification & Quantitative Gameplay Matrix

> **Product Mandate for Engineering:** In Simple Mode (Phase 1 MVP), tactics modulate match simulation outcomes by adjusting effective possession share, chance volume, shot xG quality, card rates, and player stamina decay. Below is the data-backed quantitative specification for engine implementation.

### 1. Tactical Archetypes & Mechanics

#### A. Low-Block / Counter-Attack (Deep Block, Compact Defensive 5-4-1 / 4-5-1)
- **Real-World Archetype:** Sit compact inside own defensive third, absorb pressure, restrict central high-xG space, and hit rapidly in transition when opponent over-commits. (e.g. Leicester 2016, Mourinho Chelsea, Real Madrid vs Man City).
- **Gameplay & Simulation Effects:**
  - **Possession Share:** Low possession ($\approx 35\% - 42\%$). Applies $-15\%$ to $-20\%$ penalty to base possession calculation.
  - **Opponent Chance Volume & xG:** Reduces opponent chance creation volume by $-20\%$, and lowers average opponent shot quality ($xG$ drops from $0.10$ to $0.06 - 0.07$ due to contested distance shots).
  - **Own Chance Quality (Breakaways):** Reduces overall chance frequency by $-30\%$, but counter-attack chance outcomes convert at a **high 18–25% goal rate** ($1$-on-$1$ transition opportunities).
  - **Cards & Fouls:** Low pressing fouls in opponent half, but $+15\%$ yellow card probability on defensive third break-stopping fouls.
  - **Stamina Expenditure:** Low stamina drain ($0.85\times$ baseline), preserving late-game physical attributes.

#### B. Gegenpress / High-Press (Heavy Metal 4-3-3 / High Defensive Line)
- **Real-World Archetype:** High defensive line, intense pressing upon loss of possession in the opponent's build-up zone, forcing high-turnover errors close to goal. (e.g. Klopp Liverpool, Flick Bayern Munich).
- **Gameplay & Simulation Effects:**
  - **Possession Share:** High possession ($\approx 55\% - 62\%$) achieved via high turnovers in the final third.
  - **Chance Generation:** High chance volume ($+25\%$ chance frequency). Turnover goals yield elevated $xG$ ($0.12 - 0.15$).
  - **Vulnerability (High-Line Risk):** Exposed to long balls over the top. Conceded opponent counter-attack chances convert at $+30\%$ higher goal rates.
  - **Cards & Fouls:** Significantly elevated pressing fouls and yellow card rates ($+40\%$ yellow card probability).
  - **Stamina Expenditure:** High stamina drain ($1.35\times$ baseline). Without substitutions, severe late-game fatigue penalties kick in post-75th minute.

#### C. Possession Control / Tiki-Taka (Controlled Short-Passing 4-3-3 / 3-2-4-1)
- **Real-World Archetype:** Dominate the ball, slow patient build-up, manipulate opponent defensive shape until a high-quality gap opens. (e.g. Guardiola Man City / Barcelona).
- **Gameplay & Simulation Effects:**
  - **Possession Share:** Dominant possession ($\approx 60\% - 70\%$). Applies $+15\%$ to $+25\%$ boost to possession calculation.
  - **Chance Generation & xG:** Filters out low-quality long shots. Moderate chance volume, but higher on-target shot ratio ($+15\%$ on-target proportion).
  - **Opponent Chance Suppression:** Opponent total chance volume is suppressed by $-35\%$ due to lack of ball access.
  - **Stamina Expenditure:** Moderate, controlled stamina drain ($1.0\times$ baseline).

#### D. Direct / Long-Ball (Target Man 4-4-2 / Vertical Transition)
- **Real-World Archetype:** Bypass midfield build-up with long aerial passes directly to physical target strikers, fighting for second-ball knockdowns in the opponent's box.
- **Gameplay & Simulation Effects:**
  - **Possession Share:** Low/Moderate possession ($\approx 42\% - 48\%$).
  - **Chance Generation:** Rapid chance resolution. Increases chance creation volume ($+15\%$) while bypassing midfield defensive ratings. Heavily scales off Striker `Physical` (Height/Strength/Heading) vs Opponent Defender `Physical` attributes.
  - **Shot Distribution:** Higher proportion of aerial headers; average $xG = 0.08$.
  - **Stamina Expenditure:** Standard stamina drain ($1.05\times$ baseline).

#### E. Balanced (Standard Baseline Setup)
- **Real-World Archetype:** Standard structural balance without extreme risk-taking or deep retreat.
- **Gameplay & Simulation Effects:** Engine baseline ($50/50$ possession between equal teams, $13$ chances/team/90 min, $10\%$ base $xG$ conversion, $2.6$ cards/match).

---

### 2. Quantitative Tactical Metrics Matrix

| Tactical Metric | Low-Block / Counter | Gegenpress / High-Press | Possession Control | Direct / Long-Ball | Balanced (Default) |
|---|---|---|---|---|---|
| **Possession Shift ($\Delta p_h$)** | $-15\%$ to $-20\%$ | $+10\%$ to $+15\%$ | $+15\%$ to $+25\%$ | $-5\%$ to $-10\%$ | $0\%$ |
| **Chance Volume Modifier** | $-30\%$ ($9$ chances/90) | $+25\%$ ($16$ chances/90) | $-10\%$ ($11$ chances/90) | $+15\%$ ($15$ chances/90) | $0\%$ ($13$ chances/90) |
| **Base Shot xG (Goal Conversion)** | $0.07$ (standard) / **$0.22$** (counter) | $0.14$ (high-turnover xG) | $0.12$ (patient xG) | $0.08$ (aerial xG) | $0.10$ ($10\%$ base) |
| **Conceded Shot xG Risk** | Low ($0.06$ per shot) | High ($0.18$ breakaway xG) | Moderate ($0.12$ counter xG) | Standard ($0.10$) | Standard ($0.10$) |
| **Yellow Card Rate Multiplier** | $+15\%$ (tactical fouls) | $+40\%$ (high-press fouls) | $-20\%$ (fewer tackles) | $+10\%$ | $1.0\times$ ($2.6$ cards/match) |
| **Stamina Decay Rate** | $0.85\times$ (low drain) | $1.35\times$ (heavy drain) | $1.0\times$ (standard) | $1.05\times$ (standard) | $1.0\times$ (standard) |
| **Key Player Attribute Weights** | Def: Tackling, Positioning<br>Att: Pace, Acceleration | Att: Stamina, Work Rate, Pressing<br>Def: Pace (Cover) | Att: Passing, Vision, Composure<br>Def: Interceptions | Att: Strength, Heading, Jumping<br>Def: Aerial Duels | Balanced attributes |

---

## Training Engine Specification & Quantitative Attribute Progression Matrix

> **Product Mandate for Engineering:** In Simple Mode (`S05-01`), weekly training regimens modulate player attribute growth, short-term fatigue, injury risk, match sharpness, and tactical familiarity. Weekly tick handlers evaluate active training plans per club.

### 1. Training Regimen Archetypes & Attribute Mechanics

#### A. Technical & Ball Control (Tiki-Taka / Skill Focus)
- **Primary Objective:** Enhance ball manipulation, passing accuracy, vision, and composure under pressure.
- **Attribute Progression (+) :** `Passing` (+0.3/wk), `Vision` (+0.2/wk), `Technique` (+0.3/wk), `Composure` (+0.2/wk).
- **Attribute Regression / Decay (-) :** `Strength` (-0.1/wk if unmaintained), `Tackling` (-0.1/wk).
- **Physical & Match Impact:** Low weekly fatigue cost ($0.80\times$ baseline). Low injury risk ($0.70\times$ baseline). $+5\%$ match sharpness for midfielders/wingers. Synergizes with Possession Control tactics.

#### B. Physical & Endurance (Bootcamp / High-Intensity Conditioning)
- **Primary Objective:** Build stamina, work rate, natural fitness, and physical power. Essential for high-pressing Gegenpress setups.
- **Attribute Progression (+) :** `Stamina` (+0.4/wk), `Natural Fitness` (+0.3/wk), `Strength` (+0.3/wk), `Work Rate` (+0.2/wk).
- **Attribute Regression / Decay (-) :** `Composure` (-0.1/wk due to physical exhaustion); `Technique` flat.
- **Physical & Match Impact:** Heavy weekly fatigue cost ($1.40\times$ baseline). High injury risk ($1.35\times$ baseline). Requires rest rotation before matchdays.

#### C. Defensive Organization & Shape (Low-Block & Tactical Discipline)
- **Primary Objective:** Develop defensive line discipline, marking, tackling, and positioning.
- **Attribute Progression (+) :** `Positioning` (+0.4/wk), `Tackling` (+0.3/wk), `Marking` (+0.3/wk), `Concentration` (+0.2/wk), `Teamwork` (+0.2/wk).
- **Attribute Regression / Decay (-) :** `Off-the-ball` (-0.1/wk); `Pace`/`Acceleration` flat.
- **Physical & Match Impact:** Moderate weekly fatigue ($1.0\times$ baseline). Low injury risk ($0.85\times$ baseline). Boosts Low-Block tactical familiarity.

#### D. Attacking Movement & Transition Finishing (Counter & Direct Focus)
- **Primary Objective:** Sharpen final-third finishing, off-the-ball movement, pace acceleration, and transition shooting.
- **Attribute Progression (+) :** `Finishing` (+0.4/wk), `Off-the-ball` (+0.3/wk), `Pace` (+0.2/wk), `Anticipation` (+0.2/wk).
- **Attribute Regression / Decay (-) :** `Marking` (-0.1/wk), `Positioning` (-0.1/wk).
- **Physical & Match Impact:** Moderate-high fatigue ($1.20\times$ baseline). Moderate injury risk ($1.10\times$ baseline). $+6\%$ match sharpness for strikers.

#### E. Recovery & Tactical Rest (Light / Congested Schedule Week)
- **Primary Objective:** Rapid squad fatigue recovery, injury rehabilitation, tactical video analysis, and mental reset.
- **Attribute Progression (+) :** `Tactical Familiarity` (+5%/wk), `Decisions` (+0.1/wk). Physical attributes remain flat.
- **Attribute Regression / Decay (-) :** Slight physical attribute decay (-0.05/wk) if overused consecutively across 3+ weeks.
- **Physical & Match Impact:** Rapid squad fatigue removal (**$-40\%$ accumulated fatigue**). Minimal injury risk ($0.20\times$ baseline). Essential during 2-match congested weeks.

---

### 2. Quantitative Training Metrics Matrix

| Training Archetype | Primary Attribute Growth (+) | Secondary Attribute Impact (-) | Weekly Fatigue Cost | Injury Risk Multiplier | Match Sharpness Impact | Best Tactical Synergy |
|---|---|---|---|---|---|---|
| **Technical & Ball Control** | `Passing` (+0.3)<br>`Vision` (+0.2)<br>`Technique` (+0.3) | `Strength` (-0.1)<br>`Tackling` (-0.1) | $0.80\times$ (Low) | $0.70\times$ (Low) | $+5\%$ Sharpness | Possession Control |
| **Physical & Endurance** | `Stamina` (+0.4)<br>`Strength` (+0.3)<br>`Natural Fitness` (+0.3) | `Composure` (-0.1)<br>Technique (flat) | $1.40\times$ (Heavy) | $1.35\times$ (High) | $+2\%$ Sharpness | Gegenpress / High-Press |
| **Defensive Organization** | `Positioning` (+0.4)<br>`Tackling` (+0.3)<br>`Marking` (+0.3) | `Off-the-ball` (-0.1)<br>Pace (flat) | $1.00\times$ (Moderate) | $0.85\times$ (Low) | $+3\%$ Sharpness | Low-Block / Counter |
| **Attacking Movement & Finishing** | `Finishing` (+0.4)<br>`Off-the-ball` (+0.3)<br>`Pace` (+0.2) | `Marking` (-0.1)<br>`Positioning` (-0.1) | $1.20\times$ (Mod-High) | $1.10\times$ (Moderate) | $+6\%$ Sharpness | Direct / Counter-Attack |
| **Recovery & Tactical Rest** | `Tactical Familiarity` (+5%)<br>`Decisions` (+0.1) | Physical attributes (-0.05 if overused) | $-40\%$ Fatigue | $0.20\times$ (Minimal) | $-2\%$ Sharpness | Congested 2-Match Weeks |

---

### 3. Age & Potential Development Scaling Rules

- **Youth Prospects (Age 16–21):** Attribute growth multiplier = **$1.8\times$**. Rapid attribute absorption towards potential ceiling.
- **Prime Athletes (Age 22–29):** Attribute growth multiplier = **$1.0\times$**. Attributes maintain prime levels; training shifts attribute distribution.
- **Veterans (Age 30+):** Physical attributes (`Pace`, `Stamina`) naturally decay by $-0.2$/month unless Physical Endurance training is maintained; mental/tactical attributes (`Positioning`, `Decisions`) grow or remain stable ($1.2\times$ mental growth rate).

---

## Orchestration Layer Implementation Contract (`internal/match` & `internal/player`)

1. **Pre-Match Aggregation:** `internal/match` loads squad lineup and active tactical setup. It calculates position-weighted base `Attack` and `Defense` scores, then applies tactical multipliers from the Tactical Matrix to set `Team.Attack`, `Team.Defense`, `Team.Aggression`, and initial possession weights before invoking `matchsim.Simulate(opts)`.
2. **Weekly Training Processing:** Weekly tick handlers execute the active club training plan, updating `player.player_attributes`, accumulated player fatigue, fitness condition, and injury risk values in the database.
3. **Live Tactical Commands (`LiveInputs`):** When a manager submits an in-match tactical adjustment (e.g. switching from Balanced to Low-Block at minute 70), `internal/match` appends a `LiveInput` object (`Minute: 70, Kind: "tactical_change", Detail: {"style": "low_block"}`).
4. **Deterministic Replay:** Replaying a match passes `Seed` + `LiveInputs` array to `matchsim.Simulate()` to reproduce the byte-identical feed and scoreline.

---

## Delivery evidence

- Pending.
