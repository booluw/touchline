# Chapter 9 — Clubs: DNA, archetypes, reputation and supporters

> Every club has a personality. The **club DNA** is that personality made
> numeric; the **board persona** and **supporters** are who judges the manager
> against it.

## 9.1 The club row

`club.clubs` carries identity (name, short name, `country` text), `reputation`,
`is_ai_controlled`, `current_manager_id`, and links to the club's DNA, board,
supporter group, finance account, academy, facilities, lineup/tactics/training
rows and form state.

- `is_ai_controlled = TRUE` → run by a PolicyBot manager; it can **issue job
  offers** ([Ch. 22](22-managers-and-job-offers.md)).
- `is_ai_controlled = FALSE` → a human manager holds it; kickoffs use the
  evening slot pool ([Ch. 6](06-leagues-and-scheduling.md)).

## 9.2 Club DNA (PRD §6)

`club.club_dna` stores the identity dimensions:

| Dimension | Range / values | Consumers today |
| --- | --- | --- |
| `competitive_ambition` | 0–100 | board expected finish ([Ch. 23](23-board-and-job-security.md)), match motivation ([Ch. 16](16-matchday-and-live-matches.md)), PolicyBot training choice ([Ch. 25](25-policybot-and-absence.md)) |
| `patience` | 0–100 | board expected finish (+1.5 places if ≥ 70), board relationship score |
| `academy_importance` | 0–100 | narrative / future |
| `managerial_control` | 0–100 | future |
| `star_power_preference` | 0–100 | future |
| `wage_tolerance` | 0–100 | future |
| `financial_philosophy` | `aggressive`, `balanced`, `self_sustaining`, `conservative`, `debt_tolerant`, `investor_funded` | future |
| `recruitment_philosophy` | `superstar_recruitment`, `academy_first`, `undervalued_players`, `domestic_youth`, `free_transfers`, `international_scouting` | future |
| `selling_philosophy` | `never_sell_stars`, `sell_when_replacement_exists`, `financially_driven`, `player_driven`, `sell_for_large_profit` | future |
| `tactical_identity` | `possession`, `counterattack`, `pressing`, `defensive`, `direct`, `adaptable` | future (S10-03) |
| `cultural_identity` | `prestigious`, `youth_oriented`, `local`, `working_class`, `international` | narrative |
| archetype | see below | persona mapping |

"Future" means seeded and stored but not yet read by a scoring path; richer DNA
adherence is S10-03 ([Ch. 30](30-roadmap-and-open-decisions.md)).

## 9.3 Archetypes (PRD §7)

Generated in `internal/bootstrap/profile.go` from a deterministic seed
(`fnv64a("profile:" + clubName + ":" + clubID)`), so a club's DNA is stable for
life. Values below are bases; each is **jittered ±10** (supporter loyalty and
financial sensitivity ±15).

| Archetype | Weight | Ambition | Patience | Academy imp. | Star power | Wage tol. | Board persona |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `community_club` | 30 | 45 | 85 | 70 | 40 | 60 | `patient_owner` |
| `survival_club` | 25 | 35 | 80 | 65 | 45 | 50 | `patient_owner` |
| `academy_club` | 15 | 70 | 75 | 95 | 35 | 55 | `academy_owner` |
| `fallen_giant` | 10 | 85 | 35 | 55 | 75 | 65 | `demanding_owner` |
| `moneyball_club` | 10 | 55 | 70 | 40 | 30 | 45 | `financial_conservative` |
| `giant` | 5 | 90 | 40 | 60 | 80 | 70 | `prestige_owner` |
| `investor_club` | 5 | 60 | 60 | 30 | 25 | 40 | `financial_conservative` |

Managerial control is 50 ± 10 for all. Tactical identity is a uniform draw.

## 9.4 The board

`club.boards.personality_type` holds one of six personas
(`patient_owner`, `demanding_owner`, `financial_conservative`, `academy_owner`,
`prestige_owner`, `political_board`). Each carries a 7-weight row and a
negotiation tolerance; an unknown stored persona falls back to
`political_board`. Everything about how the board judges — match ratings,
mandates, monthly confidence, sacking — is [Chapter 23](23-board-and-job-security.md).

## 9.5 Reputation

There are three distinct reputations; don't confuse them.

| Reputation | Where | Range | Used by |
| --- | --- | --- | --- |
| **Club reputation** | `club.clubs.reputation` | 0–100 | per-match expected result ([Ch. 23](23-board-and-job-security.md)), giant-killing gate ([Ch. 16](16-matchday-and-live-matches.md)), offer `club` block, academy quality offset ([Ch. 11](11-player-lifecycle-and-academy.md)) |
| **League reputation** | `competition.competitions.reputation` | 0–100 | default continental band ([Ch. 8](08-cups.md)) |
| **Manager career reputation** | `SUM(manager.manager_reputation_events.delta)` per world | unbounded | board `alternatives_score` ([Ch. 22](22-managers-and-job-offers.md)) |

## 9.6 Supporters

Each club has one supporter group (`club.supporter_groups`), seeded with the
club's `patience` and `ambition`, a `loyalty` (75 ± 15), an `identity`
(`prestigious`, `youth_oriented`, `local`, `working_class`, `international`),
`financial_sensitivity` (55 ± 15), `rivalry_intensity_base = 0` and
**`current_sentiment = 50`**.

### How sentiment moves (IM33, OPD-58)

Sentiment is a 15–95 number that moves **after every completed match** toward
that match's board rating:

```
sentiment ← clamp( round(sentiment + α · (rating − sentiment)), 15, 95 )
α = 0.08            (MatchSentimentAlpha)
α = 0.16 in a rivalry game (×2, MatchSentimentRivalryBoost)
```

A **rivalry game** = the two clubs' `rivalry` edge in `social.relationships`
has strength ≥ **50** (`RivalryStrengthThreshold`). Every meeting grows that
edge (a first meeting tops out at 40), so the threshold — not the edge's mere
existence — marks a true rivalry ([Ch. 24](24-social-and-rivalries.md)).

The only other mover is an **academy shutdown**: an immediate −15
(`ShutdownSentimentPenalty`), floored at 15; reopening does not restore it
([Ch. 11](11-player-lifecycle-and-academy.md)).

The monthly board review **reads** sentiment as its `supporter_sentiment_score`
factor; it no longer moves it. (Older docs describe a review-time EWMA at
α = 0.20 — that was superseded by IM33.)

`loyalty`, `financial_sensitivity` and `rivalry_intensity_base` are seeded but
not read by any scoring path yet.

### Fan-reaction news

For a **human-managed** club every match publishes a `fan_reaction` story
quoting two or three named fans on the manager, with mood band
`(sentiment + rating) / 2`: < 30 angry, < 50 worried, < 70 content, else
delighted; about one voice in four sits a band away. Deterministic templates,
no LLM ([Ch. 23](23-board-and-job-security.md) §23.3).

## 9.7 Facilities and academy

`club.facilities` rows (`training_ground`, `youth_facility`, `medical`,
`stadium`) carry levels 1–10 (default 5 when absent). They feed development
([Ch. 13](13-training-and-development.md)) and injury recovery
([Ch. 14](14-condition-and-injuries.md)). `club.academies` holds the investment
tier and coaching level ([Ch. 11](11-player-lifecycle-and-academy.md)). Stadium
condition has no state yet (pitch is a neutral factor).

## 9.8 Club rivalries vs. relationship rivalries

Two mechanisms coexist:

- **`club.rivalries`** — seeded at club creation with a fixed `intensity`
  (city/regional derby proposal **80**). `intensity ≥ 60` makes a fixture a
  **derby** for the match engine (motivation floor, card scaling, temperament
  divergence).
- **`social.relationships` `rivalry` edges** — grown by every meeting; drive
  supporter sentiment amplification and manager rivalries.

Both are explained in [Chapter 24](24-social-and-rivalries.md).

## Connections

- Board judging the club's manager: [Chapter 23](23-board-and-job-security.md).
- Finance account, budgets: [Chapter 20](20-finance.md).
- Code: `internal/bootstrap/{profile,service}.go`, `internal/board`.
- Source: PRD §§6–8, `docs/design/board-numerics.md`, IM21, IM33, OPD-47, OPD-58.

---
[← Cups](08-cups.md) · [Contents](the-touchline-book.md) · [Next: Players →](10-players.md)
