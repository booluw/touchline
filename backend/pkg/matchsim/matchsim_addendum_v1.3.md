# pkg/matchsim — Addendum v1.3-proposed

### PM realism review (round 2) + design for form, morale, ambition, and referee bias

Status: Parts 1–5 (round 2: Attack/Defense split, Form, Variance, Squad Morale,
Motivation, Referee Bias) are **APPROVED** — see the PM sign-off in Part 5.
Part 6 is **new and proposed**, extending the approved design with
individual-player realism (hidden traits + personal emotional impact). It is
additive: nothing in Part 6 reopens or changes any Part 5 decision. Sprint:
S04-02.
Companion to: `README.md` (`pkg/matchsim`), `Touchline_Technical_Implementation_Plan.md`, `touchline_schema.sql`

This document does two things:

1. Records concerns from a second realism pass over the v1.0-proposed README, focused on internal consistency and on the explicit product requirement that outcomes must not be predictable from ability numbers alone.
2. Specifies the new inputs needed to fix them: an Attack/Defense split, a Form subsystem, a per-match variance roll, a Squad Morale factor (driven by manager–player interactions and leadership), a Club Ambition/Motivation factor, and a Referee Bias mechanic.

None of this breaks the core determinism contract. Every new modifier is either (a) computed once, upstream of `Simulate`, from data that's already persisted, or (b) drawn from the same seeded `SplitMix64` sequence as everything else in the engine, in a fixed position in the canonical draw order. Same seed + same computed inputs ⇒ same result, exactly as today.

---

## Part 1 — Concerns from the realism review

### Concern 6 (numbering continues from the v1.0 README's Concerns 1–5): Single `Ability` scalar can't represent tactical identity
`Team{ID, ClubName, Ability}` is one number reused as both "attAbility" and "defAbility" in Section 6's goal-scaling formula. This makes it structurally impossible for two clubs with the same `Ability` to actually play differently — a disciplined, low-scoring "Moneyball Club" and a chaotic, high-scoring "Giant" would simulate identically. Club DNA's `tactical_identity` field has nothing real to attach to. **Fix: split into `Attack` and `Defense`.** (Part 2, below.)

### Concern 7: No persistent form — this is the direct cause of the predictability problem
Nothing upstream of `Simulate` currently varies over time. If `Ability` (or its replacement) is computed fresh from static attributes every match, results will track team quality almost deterministically, which is the opposite of "some teams lose form, others go on a great run." **Fix: a Form subsystem plus a per-match variance roll.** (Part 2.)

### Concern 8: Cards and injuries have no in-match consequence
A red card should make a team measurably worse for the rest of the match; nothing in the spec currently adjusts `Ability`/`Attack`/`Defense` after a dismissal. Same issue for serious injuries forcing a like-for-like-unavailable substitution.

### Concern 9: Substitutions are cosmetic
The canonical draw order checks for a substitution at minutes 60–64 and 75–79, but nothing changes afterward. A fresh striker coming on should matter, or tactical subs aren't a real decision (cuts against the "every decision has consequences" principle).

### Concern 10: Event vocabulary doesn't match `match.match_events`'s CHECK constraint
The schema allows `assist`, `injury`, `penalty_awarded`, `penalty_scored`, `penalty_missed` — matchsim's `Type` enum doesn't emit any of them yet. These need to be reconciled before S04-02 implementation starts.

### Concern 11: No distinct penalty modeling
Real penalties convert around 76–80%, far above the ~10% open-play goal rate. Right now "foul" is a single bucket in the chance table with no in-the-box vs. open-play distinction.

### Concern 12: `Simulate`'s signature can't express OPD-21's live-input replay
OPD-21 requires replaying "seed + ordered live inputs" for live-match substitutions/tactic changes, but `Options` has no ordered-input field. **Fix: add `LiveInputs`.** (Part 2.)

### Concern 13: No channel for rivalry intensity into card/aggression probability
The PM's own Concern 2 (v1.0 README) asks for card probability to scale with "match derby/rivalry intensity," but there's no field for it and `club.rivalries.intensity` isn't wired to anything in this package.

### Minor/deferred: extra time & shootouts, own goals, chance "size" tiers (big chance vs. half chance), in-match clustering of momentum within a game (as opposed to across games, which Part 2 covers). Worth tracking, not blocking v1.2 sign-off.

---

## Part 2 — New inputs and subsystems

### 2.1 Attack/Defense split

Replace the single `Ability` with two ratings, each derived by the orchestration layer (`internal/match`) from position-weighted `player.player_attributes`:

- `Attack` — weighted from forwards/attacking mids' technical + mental attributes, plus a smaller contribution from wide players and creative midfielders.
- `Defense` — weighted from defenders/GK's defensive + physical + mental attributes, plus a smaller contribution from defensive midfielders.

Section 6's goal-scaling formula now does what it was written to imply: `(attackingTeam.Attack / defendingTeam.Defense)^GoalAbilityScale`, clamped as before. Possession (Section 6, first formula) uses a combined figure (e.g. `(Attack+Defense)/2` per team, tactically modulated — see Concern 3 in the original review) rather than a single `Ability`.

### 2.2 Form (the fix for "some teams lose form, others go on a run")

A new subsystem, **not** part of `pkg/matchsim` itself (which stays pure/DB-free) — owned by the orchestration layer, computed once per club per matchday and passed in as a modifier.

```go
// internal/form (new package — orchestration layer, not pkg/matchsim)
type FormState struct {
    ClubID          uuid.UUID
    CurrentRating   float64 // EWMA of recent performance vs. expectation, e.g. range [0.85, 1.15]
    LastUpdatedTick int64
}

// Updated after every completed match:
// actual_result_quality (goal difference vs. expected goal difference, given Attack/Defense at kickoff)
// blended into CurrentRating with a decay factor (e.g. alpha=0.2), so form is a rolling
// trend, not a single-match blip, and naturally decays back toward 1.0 (neutral) if unfed.
func UpdateForm(prev FormState, actualVsExpected float64, alpha float64) FormState
```

This is exactly what produces multi-week hot streaks and slumps: a team that keeps outperforming expectation sees `CurrentRating` climb and stay elevated for several matches before decaying back to neutral if performances normalize; a team on a losing run sees it sink. It's applied as a multiplier on `Attack`/`Defense` before they reach `Simulate`, clamped to a sane band (e.g. ±15%) so form trends but never dominates underlying quality.

### 2.3 Per-match variance roll (the "on the day" factor)

Even with form, two teams of known, stable quality should still be able to produce a surprising result on a given day — that's what makes a single match watchable rather than a foregone conclusion. Add one more seeded draw, **once per team per match**, at a fixed position in the draw order (see Part 3):

```go
// One extra SplitMix64 draw per team, at the start of the match, before minute 1.
// Produces a small multiplicative nudge, e.g. drawn from a bounded distribution
// centered at 1.0 (say, triangular or truncated-normal in [0.85, 1.15]).
type MatchDayVariance struct {
    HomeFactor float64
    AwayFactor float64
}
```

This is what makes "the better team lost anyway" possible on any given Saturday, independent of the multi-week form trend — the two concepts are deliberately separate: form is a trend across matches, variance is noise within one match.

### 2.4 Squad Morale — driven by manager interactions and leadership

Also computed upstream (candidate home: `internal/squad`, alongside the dressing-room/faction calculations from PRD section 15), not inside `pkg/matchsim`:

```go
type SquadMorale struct {
    ClubID  uuid.UUID
    Rating  float64 // multiplier, e.g. [0.90, 1.10], applied to both Attack and Defense
}

func ComputeSquadMorale(squad []PlayerMoraleInput) SquadMorale

type PlayerMoraleInput struct {
    PlayerID       uuid.UUID
    Leadership     int     // player.player_personality.leadership, [1,100]
    IsLikelyStarter bool   // starters weighted more than fringe squad members
    CurrentSentiment int   // derived from recent player.player_emotional_states rows
}
```

Design:

- Each player's contribution to squad morale is their current emotional sentiment, **weighted by their own leadership score** — a high-leadership player who's happy lifts the group more than a low-leadership player who's equally happy; the same high-leadership player, if angry (broken promise, dropped, etc.), drags the group down harder than a low-leadership player would.
- Weight starters more heavily than fringe players — a disgruntled reserve matters less than a disgruntled first-choice captain.
- This requires **no new mechanic to "set" morale** — it falls directly out of the manager-interaction system already specified in PRD section 16 (`player.interact`, praise/criticism, promises) via its effect on `player_emotional_states`. Neglecting your best leader costs you squad-wide; keeping a high-leadership captain onside is a genuine, explainable lever a manager pulls (and the `Explanation` pattern from the implementation plan should surface exactly which players are dragging morale down, same as board-confidence explanations).

### 2.5 Club Ambition / Motivation modifier — including "upset spice"

Also computed upstream, from `club.club_dna.competitive_ambition` plus fixture context:

```go
type MotivationModifier struct {
    ClubID uuid.UUID
    Factor float64 // multiplier, e.g. [0.85, 1.20]
}

func ComputeMotivation(
    dna ClubDNAInput,
    fixtureContext FixtureContext, // is this a rivalry? relegation six-pointer? dead rubber?
    opponentReputationGap int,     // opponent reputation - this club's reputation
    seed int64,                    // for the deterministic "giant-killing roll"
) MotivationModifier
```

Two distinct effects, deliberately not one:

1. **Ambient drag in low-stakes fixtures.** A low-`competitive_ambition` club in a fixture with nothing riding on it (mid-table, no rivalry, no relegation stakes) gets a small negative default — "going through the motions" is the *baseline* state for a club that genuinely doesn't prioritize winning.
2. **A rare, probabilistic upset roll — not a guaranteed boost.** When a low-ambition club faces a significantly higher-reputation opponent, a low-probability seeded draw (e.g. 8–12% chance, tunable) can flip the modifier sharply positive for that single match — "backs against the wall, nothing to lose." This must stay a dice roll: if it always fired, every "small club vs. big club" fixture would become predictably harder than it should be, which defeats the point. Most of the time, a low-ambition club playing a giant should just lose comfortably, exactly as their DNA suggests — the upset is meant to be a story people talk about specifically because it's rare.
3. **Rivalry floor.** Regardless of ambition, `club.rivalries.intensity` above a threshold sets a motivation floor near 1.0 or above — nobody phones in a derby, however unambitious the club's ownership is otherwise.

### 2.6 Referee bias — mostly noise, a little (transparent, tunable) bias

New engine-internal draw, seeded and replayable like everything else, resolving **marginal** decisions only — this never overturns a clear-cut call, it only nudges the outcome of the ambiguous ones already generated by the existing chance/foul table.

```go
// New Tuning fields:
type Tuning struct {
    // ...existing fields (Section 5 of the original README)...
    RefereeNoiseFactor float64 // e.g. 0.03 — probability a marginal call is decided "the other way," pure noise, no direction
    RefereeBiasFactor  float64 // e.g. 0.01 — small optional directional nudge (see below), default near-zero
    RefereeBiasSource  string  // "none" | "home_crowd" | "reputation_gap" — which signal (if any) drives the directional component
}
```

Design notes:

- **Noise dominates bias, by a wide margin.** `RefereeNoiseFactor` should be several times larger than `RefereeBiasFactor` in the default tuning — the mechanic should read as "referees are human and occasionally get close calls wrong in either direction," not "the game favors certain clubs." This matters for competitive fairness (PRD section 51): a strong reputation-driven directional bias would function as an invisible thumb on the scale of match outcomes, which is exactly the kind of thing the non-pay-to-win policy is trying to prevent conceptually, even though no money is involved.
- **If a directional component is enabled at all, keep it small and pick the source carefully.** `home_crowd` (a slight home-favoring nudge on marginal calls, independent of the separate `HomeAdvantageFactor` on goals/possession) is well-supported by real refereeing research and is the safer default. `reputation_gap` (bigger clubs get more benefit of the doubt) is more colorful and does happen in real football, and it's a legitimate option if you want "the ref always favors them" to be a thing players can genuinely complain about — but I'd ship with it **off by default** and gate it behind a config flag, since an always-on version could feel exploitative to anyone consistently managing a smaller club. Your call as PM, but I'd treat "noise-only" as the safe v1.2 default and "add reputation bias" as an opt-in variant to test separately.
- **Every referee-bias event is explainable, never hidden.** This is what turns it from "the game screwed me" into "great, a story." Each triggered event gets an `Explanation` (per the implementation plan's cross-cutting pattern) and a canonical commentary line in `MatchEvent.Detail`, e.g.:
  > *"The referee has waved away appeals for a penalty there, allowing play to continue."*
  > *"That was given as a foul, but replays would have shown minimal contact — a marginal call."*
- **Event vocabulary:** add `penalty_appeal_waved`, `var_review` (if/when VAR-style review is in scope), and reuse `chance_created`/`foul` detail fields for softer marginal-call flavor text, rather than inventing a large new taxonomy.

---

## Part 3 — Updated canonical draw order (v1.2-proposed)

The v1.0 README's draw order is the replay contract and "must never change without bumping `Tuning.Version`/`engine_version`" — this addendum bumps it. New/changed steps in **bold**:

**Pre-match (once per team, before minute 1):**
1. **`variance`** — per-team match-day variance roll (Section 2.3).

**For each minute `m` in 1..90:**
1. `possess` — possession draw (now using combined, tactically-modulated Attack+Defense, per the original README's Concern 3 fix, further scaled by Form × Morale × Motivation × Variance).
2. `chance?` — does the attacker create a chance this minute?
   - If yes: `outcome` (now ability-scaled off `Attack` vs. `Defense` separately, per Section 2.1; **in-the-box fouls resolve to a distinct penalty sub-outcome**, per Concern 11), then `feed?`.
3. `card` — card check, **now coupled to the foul outcome and scaled by aggression/rivalry intensity** (per the v1.0 README's own Concern 2), not an independent per-minute draw.
4. **`referee`** — only fires on marginal outcomes flagged by step 2/3 (foul-in-box, borderline card); resolves noise/bias per Section 2.6.
5. `sub` — substitution check at canonical windows, **now also consuming any manager-issued live input for this minute if present** (Section 2.7 below), rather than always being a random draw.

**After a red card or a substitution:** the affected team's effective `Attack`/`Defense` for all remaining minutes is recomputed (down a player for reds; swapped-in player's attributes for subs) — closing Concerns 8 and 9.

### 2.7 Closing the OPD-21 gap: `LiveInputs`

```go
type Options struct {
    Seed       int64
    Home, Away Team
    Tuning     Tuning
    LiveInputs []LiveInput // ordered, minute-tagged; empty for non-live/quick-result matches
}

type LiveInput struct {
    Minute int
    ClubID string
    Kind   string // "substitution" | "tactical_change"
    Detail map[string]any // e.g. {"out": playerID, "in": playerID} or {"mentality": "attacking"}
}

type Team struct {
    ID, ClubName        string
    Attack, Defense     int     // [1,100], position-weighted (Section 2.1)
    FormFactor          float64 // Section 2.2, multiplicative
    MoraleFactor        float64 // Section 2.4, multiplicative
    MotivationFactor    float64 // Section 2.5, multiplicative
    Aggression          int     // [1,100], feeds card probability (Concern 13)
    RivalryIntensity    int     // 0-100, from club.rivalries, raises Aggression + motivation floor
}
```

Replaying a live match is now genuinely `seed + ordered LiveInputs → identical outcome`, matching OPD-21 as written, instead of a signature that couldn't express it.

---

## Part 4 — Open items for PM sign-off

1. Confirm `RefereeBiasSource` default (`none` vs `home_crowd`) before v1.2 lock-in.
2. Confirm the giant-killing roll probability band (proposed 8–12%) against how often you actually want to see upset stories surface in news/media (PRD section 40) — too rare and it never becomes a talking point, too common and it stops feeling special.
3. Decide whether Form and Morale should be visible to the manager directly (a "squad mood" indicator on the dashboard) or only inferable from results and player interactions — affects UI scope, not the engine.
4. Sign off on the Attack/Defense weighting formula per position (needs the actual `player_attributes` category weights from S04-02/S05 once squads have real generated attributes to tune against).

---

## Part 5 — PM Sign-Off, Resolutions & Product Strategy Directives

> **PM Mandate & Sign-Off:** Addendum v1.2-proposed is **APPROVED** by Product Management. The introduction of Attack/Defense split, Form EWMA, Squad Morale, Match-Day Variance, and Referee Noise elevates matchsim to an authentic, data-backed football simulation. Below are the official PM decisions resolving the Part 4 open items:

### 1. Referee Bias Default (`RefereeBiasSource = "home_crowd"`)
- **PM Decision:** Set default `RefereeNoiseFactor = 0.03`, `RefereeBiasFactor = 0.01`, and `RefereeBiasSource = "home_crowd"`.
- **Rationale:** Real-world sports analytics (Opta/academic studies on top European leagues) show referee decisions on marginal calls shift ~1–2% in favor of the home team due to home crowd noise pressure. This integrates cleanly with Home Advantage without creating unfair non-pay-to-win balance issues. `reputation_gap` remains disabled by default (`false`) to ensure smaller clubs are not systematically penalized by arbitrary reputation scales.

### 2. Giant-Killing / Upset Roll Band (Approved: 10% Fixed Baseline)
- **PM Decision:** Approved 10% baseline probability draw (within the 8–12% band) for low-ambition/underdog clubs facing high-reputation opponents.
- **Rationale:** Real-world cup and league data shows underdog upsets occur in ~1-in-10 matches between mismatched teams. A 10% seeded draw generates memorable narrative news stories (PRD §40/§83) while preserving expectation over a full season.

### 3. Visibility of Form and Morale on Dashboard UI (Explicit & Explainable)
- **PM Decision:** Both Form and Squad Morale must be **explicitly visible to managers** on the home dashboard (`GET /api/dashboard`) and squad screens.
- **Rationale:** Aligns with PRD Principle 3 (*"Explain consequences — never make the player wonder why something happened"*). Form is displayed as a 5-match form string (`W-D-W-L-W`) plus a numeric factor (`1.05 (+5%)`). Morale is displayed on the Squad Dynamics screen accompanied by exact `Explanation` factor breakdowns (e.g. `+3% captain leadership`, `-5% missed promised playing time`).

### 4. Red Card & Substitution In-Match Degradation Rates
- **PM Decision:**
  - **Red Cards (Dismissals):** When a red card is drawn, the affected team's effective `Defense` is reduced by **15%** and `Attack` by **25%** for all remaining minutes of the match.
  - **Substitutions:** Swapping in a fresh player recalculates position-weighted `Attack`/`Defense` and applies a **+5% stamina/energy boost** to the substitute's attribute contribution over tired starters.
- **Rationale:** Resolves Concerns 8 & 9. Tactical substitutions and red cards now carry immediate, deterministic matchday consequences.

### 5. Reconciliation with DB Event Schema (`match.match_events`)
- **PM Decision:** Approve expanding `MatchEvent.Type` enum in `matchsim` to output `penalty_awarded`, `penalty_scored`, `penalty_missed`, `injury`, and `assist` matching the `match.match_events` database CHECK constraint.
- **Penalty Conversion:** In-the-box penalties draw against a 78% baseline conversion rate (Opta top-flight historical average: 76–80%).


---

## Part 6 — Round 3: individual player realism (hidden traits + personal emotional impact)

> **Scope note up front:** everything below is additive to the approved Part 5 design. The 78% penalty baseline, the red-card/substitution degradation rates, the referee-bias defaults, and the dashboard visibility requirement are unchanged — this section only refines how the *inputs* going into `Attack`/`Defense`/penalty conversion are assembled upstream, for the specific cases where an individual player's own traits and emotional state matter more than the squad average.

### Concern 14: `SquadMorale` only lets emotion matter diffusely — never individually

Section 2.4's `ComputeSquadMorale` weights each player's emotional sentiment by leadership and rolls it into one team-wide `MoraleFactor`. That's the right mechanism for "how does an unhappy captain affect the dressing room" — but it means an individual player's own emotional state never affects *their own* output, only their influence on everyone else's. A furious, anxious, or betrayed **starting striker** should personally play worse that match, independent of whether he's a leader who drags the whole squad down. Right now nothing in the doc does that.

### Concern 15: `player.player_hidden_traits` is entirely unconnected to match outcomes

`leadership` (from `player_personality`) is the only personality/trait field wired into anything in this addendum. None of `consistency`, `temperament`, `pressure_handling`, `professionalism`, or `learning_speed` (all in `player.player_hidden_traits`) feed the engine anywhere:

- **`consistency`** is the most direct miss. It exists specifically to answer "how much does this player's performance vary match to match" — but the only variance in the current design (`MatchDayVariance`, Section 2.3) is one flat roll applied at the *team* level, identical whether the XI is full of reliable role players or volatile boom-or-bust talents.
- **`temperament`** and **`pressure_handling`** are the obvious inputs for how a player performs in a derby, a cup final, a relegation six-pointer, or specifically when taking a penalty — none of which currently have an individual-level channel (only the team-level `MotivationModifier` from Section 2.5 reacts to fixture stakes at all).
- **`professionalism`** plausibly affects how quickly a player's personal form recovers from a bad emotional patch, but there's currently no personal form state to recover — only the club-level `FormState` from Section 2.2.

### 2.8 Individual Player Performance Modifier

Computed upstream, alongside `ComputeSquadMorale` (same package, same matchday cadence) — **not** a new input to `Simulate` itself, and **not** a per-minute mechanic inside the engine. It only changes how the orchestration layer aggregates the starting XI into the `Attack`/`Defense` scalars that already cross into `pkg/matchsim` today. This keeps the engine pure and keeps the "avoid fake complexity" principle intact: routine fixtures and fringe players are barely touched by this; it does real work specifically where it should — key players, big moments.

```go
// internal/squad (same home as ComputeSquadMorale) — orchestration layer, not pkg/matchsim.

type PlayerPerformanceInput struct {
    PlayerID         uuid.UUID
    Consistency      int  // player.player_hidden_traits.consistency, [1,100]
    Temperament      int  // player.player_hidden_traits.temperament, [1,100]
    PressureHandling int  // player.player_hidden_traits.pressure_handling, [1,100]
    Professionalism  int  // player.player_hidden_traits.professionalism, [1,100]
    CurrentSentiment int  // this player's own recent player_emotional_states — same source
                          // ComputeSquadMorale reads, but applied to THIS player only, unweighted
                          // by leadership (that weighting is morale's job, not this one's)
    IsKeyPlayer      bool // starter whose individual attribute contribution is large enough
                          // to be worth varying (captain, star performer, designated penalty taker)
}

// Returns a per-player multiplier applied only to that player's own weighted
// attribute contribution when the XI is aggregated into Team.Attack/Defense —
// squad-mates' contributions are untouched.
func ComputePlayerPerformanceFactor(
    in PlayerPerformanceInput,
    stakes FixtureContext, // same context object Section 2.5's ComputeMotivation already takes
) PlayerPerformanceFactor

type PlayerPerformanceFactor struct {
    PlayerID uuid.UUID
    Factor   float64 // multiplier, e.g. [0.85, 1.15], applied only to this player's contribution
}
```

Design:

- **`Consistency` sets the width of the player's personal variance band, not a single fixed number.** A high-consistency player's `Factor` stays close to 1.0 match to match; a low-consistency player's `Factor` swings wider (still seeded/deterministic — the draw for it lives in the same pre-match `variance` step as `MatchDayVariance`, just one additional per-key-player draw rather than a new engine phase). This is what actually lets "your star striker has an off day" happen for a *specific* reason tied to that player, distinct from the team-wide `MatchDayVariance` roll.
- **`Temperament`/`PressureHandling` only diverge from neutral when `FixtureContext` says the stakes are high** (derby, cup final, relegation six-pointer, penalty-taking moment) — reusing the same context object `ComputeMotivation` already consumes, rather than inventing a second stakes concept. In a routine midweek fixture, a poor-temperament player is not penalized for something that hasn't happened; the trait only bites when it would realistically matter.
- **`CurrentSentiment` here is deliberately unweighted by leadership** — that weighting already happened once, correctly, inside `ComputeSquadMorale` (Section 2.4), where it represents influence over teammates. This is the same emotional-state data used a second, different way: representing the player's own performance that day, which does not depend on how much of a leader they are. A quiet, low-leadership player who is furious about being dropped still personally underperforms if selected — he just doesn't drag anyone else down with him.
- **Only `IsKeyPlayer`s are worth computing this for.** A fringe rotation player's attribute contribution to the aggregated `Attack`/`Defense` is small enough that varying it isn't worth the complexity; scope this to whoever the orchestration layer already identifies as high-attribute-weight in the XI (typically 3–5 players per team) plus anyone with an unusually severe current emotional state regardless of role, so a genuinely explosive dressing-room situation still shows up even in a squad player.

### Refining the penalty conversion rate (compatible with the approved 78% baseline)

PM Part 5 §5 approved a 78% baseline penalty conversion rate — that stays the population-level default. What it doesn't yet account for is that real penalty conversion varies noticeably by *taker*, which is exactly what `Temperament`/`PressureHandling`/`Consistency` are for:

```go
type Team struct {
    // ...all existing fields from Part 3's Options/Team (unchanged)...
    PenaltyConversionRate float64 // defaults to Tuning's 78% baseline; adjusted per Section 2.8
                                  // by the designated taker's traits, bounded e.g. [0.65, 0.88]
}
```

The 78% figure remains the center of the distribution — this doesn't relitigate that number, it just says individual takers deviate around it the way real players do (a composed, high-pressure-handling penalty specialist converts more often than a nervous one stepping up under a temperament penalty). If no specific taker is identifiable (e.g. early-game squad generation before a "designated taker" concept exists), `PenaltyConversionRate` simply falls back to the approved 78% baseline unchanged.

### What this doesn't change

- `Simulate`'s signature is untouched beyond the one new optional `Team.PenaltyConversionRate` field — no new per-minute draw phase, no new `Options` field, no change to the canonical draw order from Part 3.
- Squad Morale (Section 2.4) is untouched — it continues to do exactly what it did before. This section adds a second, independent channel for emotion to matter, it doesn't modify the first.
- All five Part 5 PM decisions (referee bias defaults, the 10% giant-killing band, Form/Morale dashboard visibility, red-card/substitution degradation rates, and the 78%-baseline event-schema reconciliation) are unchanged.

### Open items for PM sign-off (Part 6)

1. Confirm the "key player" threshold (proposed: top 3–5 attribute-weight contributors per XI, plus any player with a severe current emotional state regardless of rank) — too narrow and the mechanic rarely fires; too broad and it re-introduces the per-player complexity this design is trying to avoid.
2. Decide whether an individual player's personal performance factor should ever surface to the manager directly (e.g., a "shaky under pressure" scouting note, or a locker-room-mood indicator on a specific player's profile), or stay inferable only from match outcomes and post-match reports — this extends, but doesn't override, the Part 5 §3 decision that Form/Morale are explicitly visible; individual performance factors are a separate, smaller-grain question.
3. Confirm the bounded ranges proposed above (`[0.85, 1.15]` for the general performance factor, `[0.65, 0.88]` for penalty conversion) against real-world variance in player-to-player consistency and penalty conversion rates once actual generated-attribute data exists to check them against.

---

## Part 7 — PM Sign-Off, Resolutions & Directives for Part 6 (Individual Player Realism)

> **PM Mandate & Sign-Off:** Part 6 (Addendum v1.3-proposed) is **APPROVED** by Product Management. The individual player performance modifier and taker-specific penalty conversion model solve Concerns 14 & 15 cleanly while keeping `pkg/matchsim` pure, memory-safe, and deterministic. Below are the official PM decisions resolving the Part 6 open items:

### 1. Key Player Selection Threshold (Approved: Top 3–5 per XI + Severely Emotional Starters)
- **PM Decision:** Approved. The orchestration layer (`internal/squad`) will compute `PlayerPerformanceInput` for the top 3–5 position-weighted attribute contributors in the starting XI (Goalkeeper, Captain, Primary Striker, Playmaker) plus any starter with a severe emotional state (`|current_sentiment| >= 20`).
- **Rationale:** Focuses compute resources and narrative impact where it matters most to managers. Routine squad players are aggregated via team-wide baselines, while star players and volatile locker-room situations produce individual performance variance.

### 2. UI Visibility & Transparency of Individual Traits & Emotional Factors (Explicit Notes)
- **PM Decision:** Individual hidden traits (`consistency`, `pressure_handling`, `temperament`) and performance factors must be **transparently communicated** to managers:
  - **Player Profile / Scouting Reports:** Unlocked hidden traits surface as descriptive scouting tags (e.g. *"Thrives Under Pressure"*, *"Inconsistent in Away Fixtures"*, *"Volatile Temperament"*).
  - **Pre-Match Lineup Warnings:** Lineup selector alerts managers when a key player's personal sentiment creates a negative multiplier (e.g. *"Star Striker: Distracted by broken contract promise (-10% performance modifier)"*).
- **Rationale:** Strictly enforces PRD Principle 3 (*"Explain consequences — never make the player wonder why something happened"*). If a manager loses a match because their star player was unhappy, the system must explain that cause explicitly via `Explanation` breakdowns.

### 3. Bounded Range Validations (Approved)
- **PM Decision:**
  - **Individual Performance Factor:** Approved `[0.85, 1.15]` multiplier range. Prevents an individual player's bad day from completely overwhelming underlying team tactical quality.
  - **Penalty Taker Conversion Rate:** Approved `[0.65, 0.88]` conversion rate range centered around the 78% baseline. Elite penalty takers (e.g. high `PressureHandling` + high technical attribute) convert at ~85–88%, while composed default takers convert at ~78%, and nervous/unsettled takers convert at ~65–68%. This aligns precisely with Opta historical penalty conversion distributions across European leagues.

### 4. Engine Purity & Zero Breaking Changes (Confirmed)
- **PM Directives:** Confirmed that `Simulate()` signature, canonical draw order, and replay contracts remain 100% backward compatible. All per-player calculations occur upstream in `internal/squad` during matchday aggregation.

