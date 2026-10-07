# Chapter 31 — Storylines, Arcs and Emergent Narrative

The Storyline Engine transforms Touchline from a data-heavy football simulation into an evolving, personal football saga. By monitoring simulation data and event streams, the engine constructs **emergent narrative arcs**, presents **crossroad dilemmas**, enables **pre-match mind games**, and logs **season chronicles**.

---

## 31.1 Product Thesis & Architecture

In sports management games, scripted visual-novel narratives quickly become repetitive and break down in persistent multiplayer environments. Touchline adopts an **emergent narrative design**:

```
 Observe World State → Pattern Match Event Spine → Trigger Narrative Arc
         │                                                    │
         ▼                                                    ▼
 Season Chronicle ◄─── Resolve Climax / Match ◄─── Manager Dilemma Choice
```

Every narrative state change is written to `world.events` accompanied by a typed `Explanation` payload ([Chapter 3](03-events-and-explanations.md)).

---

## 31.2 Database Schemas & Storage (`story.*`)

Storyline state lives in the `story` schema:

1. **`story.arcs`**: Tracks active and historical narrative sagas for each `(world_id, club_id, manager_id)`.
   - Columns: `id`, `world_id`, `club_id`, `manager_id`, `arc_type`, `state` (`triggered`, `escalating`, `climax`, `resolved`), `context_json`, `created_at`, `updated_at`.
2. **`story.dilemmas`**: Actionable managerial decision prompts.
   - Columns: `id`, `arc_id`, `world_id`, `manager_id`, `title`, `description`, `options_json`, `status` (`pending`, `resolved`, `expired`), `expires_at_tick`.
3. **`story.chronicles`**: End-of-season storybook summaries.
   - Columns: `id`, `world_id`, `season_number`, `club_id`, `manager_id`, `headline`, `summary_json`, `created_at`.

---

## 31.3 Core Narrative Arc Archetypes

The engine evaluates five initial arc patterns ([docs/design/storyline-numerics.md](../design/storyline-numerics.md)):

| Arc Type | Trigger Condition | Primary Dilemma | Narrative Resolution |
| --- | --- | --- | --- |
| `underdog_miracle` | Unbeaten run (≥4) by low-rep club (≤60) | Media Hype vs Low Expectations | Streak ends or Season Finish |
| `mutiny_faction_split` | Influencer player morale ≤ 0.35 + faction split | Bench Ringleader vs Fine vs Capitulate | Faction morale moves / Transfer |
| `ex_factor_revenge` | Fixture vs former player/manager | Pre-Match Mind Game Stance | Post-match headline & ratings |
| `academy_local_hero` | Homegrown player ≤ 20yo, ratings ≥ 75 | Improved Contract vs Firm Stance | Contract signed or player sold |
| `great_escape_relegation` | Bottom 3 position, ≤ 8 games remaining | Win Bonuses vs Double Training | Relegation avoided or suffered |

---

## 31.4 Managerial Crossroad Dilemmas

Dilemmas appear in the **Urgent** section of the manager's Dashboard (`GET /api/dashboard`, [Chapter 26](26-dashboard-news-scouting-realtime.md)) under category `dilemma`. 

### Choice Processing
- Managers submit decisions via `POST /api/storyline/dilemmas/:id/respond {option_id}`.
- If a manager is offline or un-responded after `DilemmaExpirationTicks` (default 7 days), **PolicyBot** ([Chapter 25](25-policybot-and-absence.md)) applies the conservative staff stance.
- Every choice generates immediate state changes across:
  - Squad Morale & Factions ([Chapter 18](18-morale-and-transfer-requests.md), [Chapter 19](19-dressing-room.md))
  - Board Confidence & Mandates ([Chapter 23](23-board-and-job-security.md))
  - Supporter Sentiment & News Stories ([Chapter 9](09-clubs-dna-supporters.md), [Chapter 26](26-dashboard-news-scouting-realtime.md))

---

## 31.5 Pre-Match Mind Games & Press Interaction

Before high-stakes fixtures (Derbies, Top-4 clashes, Cup Finals, Human vs Human), managers can set a **Pre-Match Tactical Stance**:

- `aggressive_combative`: Boosts tackling and derby supporter sentiment, but increases yellow/red card risk by +25%.
- `underdog_deflect_pressure`: Eases squad pressure (+5 composure floor), lowers fan rating expectations.
- `clinical_focused`: Boosts tactical familiarity (+3%).

Mind-game choices feed pre-match press news and influence match engine simulation parameters ([Chapter 15](15-match-engine.md)).

---

## 31.6 Season Storybook & Career Chronicle

During season rollover ([Chapter 7](07-seasons-and-rollover.md)), `story.GenerateSeasonChronicle` analyzes the season's event stream to construct a **Season Storybook Digest**.

- Highlights biggest wins, bitterest losses, underdog runs, and breakout academy stars.
- Displays as a permanent historical chapter on the Manager Career Profile page (`GET /api/managers/:id/chronicle`).

---

## Numbers Summary

| Parameter | Value | Location |
| --- | --- | --- |
| `MaxActiveStoryArcsPerClub` | `2` | `internal/storyline/model.go` |
| `StoryArcCooldownDays` | `30` | `internal/storyline/evaluator.go` |
| `DilemmaExpirationTicks` | `7` | `internal/storyline/dilemma.go` |

---

## Connections

- Event Spine & Explanations: [Chapter 3](03-events-and-explanations.md).
- Dashboard Integration: [Chapter 26](26-dashboard-news-scouting-realtime.md).
- Dressing Room Factions: [Chapter 19](19-dressing-room.md).
- Code: `backend/internal/storyline/{model,evaluator,dilemma,press,chronicle,service,store}.go`.
- Design Ledger: `docs/design/storyline-numerics.md`.

---
[← Roadmap](30-roadmap-and-open-decisions.md) · [Contents](the-touchline-book.md)
