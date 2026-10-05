# Chapter 8 — Cup competitions: domestic, regional and qualification

Cups are single-elimination knockouts that run alongside leagues. There are two
scopes:

| Scope | Type | Field comes from | Task |
| --- | --- | --- | --- |
| **Domestic** (a country) | `domestic_cup` | every league club in the country, with staged entry | IM04 |
| **Regional / continental** (a region) | `continental` | per-league **position bands** over last season, via the qualification engine | IM06–IM10 |

Both are `competition_rules.format = 'knockout'`, single-legged, with
**golden-goal** tie-breaks. A **cup campaign** is one `competition.seasons` row
for the cup.

## 8.1 Domestic cups (IM04, OPD-32)

### Eligibility and staging

Every club with a `role='league'` membership in the cup's country is eligible —
all tiers, automatically. Two admin variables (`qualification_rules`) shape the
bracket:

| Variable | Meaning |
| --- | --- |
| **N** `first_tier_bye` | the top N tier-1 clubs skip the early rounds |
| **X** `survivor_threshold` | early rounds run among everyone else until exactly X survive; then the N join (field X+N) |

"Top N" uses most-recent standings (points → GD → GF → name), else deterministic
club order. Validation (`422`): N ≥ 0, X ≥ 1, X + N ≥ 2, and every round must
have even ties ending in a two-club final. If the bottom pool already equals X
the N enter immediately; if smaller, the admin must lower X.

### Creating and starting

```bash
POST /api/admin/cups  {"world_id", "country_id", "name", "first_tier_bye":8, "survivor_threshold":14}
POST /api/admin/worlds/$W/countries/$C/cups/$CUP/campaign
```

The campaign transaction writes the season (`in_progress`), the `role='cup'`
memberships and entries (≤ 3 cups per club, `ErrCupLimit`) and **Round 1
only**. A second campaign while one is live → `409`.

## 8.2 Bracket mechanics (both scopes)

- **Lazy materialisation.** When the last tie of a round gets its result, the
  winners (canonical order) are drawn into the next round in the same
  transaction. No "TBD vs TBD" fixtures exist.
- **Deterministic draws.** Every draw is a pure function of
  `world_seed ⊕ cup_id ⊕ round` over the advancing set; home advantage is drawn
  deterministically.
- **Completion.** The final's winner entry becomes `champion`, others
  `eliminated`; the cup season closes. Cup results never touch league standings
  or the rollover.
- `match.fixtures.matchday` doubles as the round index. The week-grouped
  league calendar does not apply; cups have their own round view.

## 8.3 Cup calendars (IM05, IM10)

The cup ladder is planned **backward from the final**:

1. **Final date** by policy (IM10, OPD-38):
   - `calculated` (default): first allowed weekday at least
     `final_offset_days` (default **3**) game-days after the **scope's latest
     league fixture** — the country's leagues (domestic) or the **union of
     participating countries' league days** (regional). Re-derived each season.
   - `fixed`: an absolute `final_date`, played exactly, never recalculated.
2. **Earlier rounds** walk backward by a seeded 2–3 day gap (3 with 75% just
   before the final), each **snapped to a league-free day** while one fits,
   keeping the two-day rest and honouring the cup's `allowed_weekdays`.
3. Round dates are stamped on the persisted ladder (`roundPlan.date`) at
   campaign start and honoured when each round materialises.

Editing (`PATCH /api/admin/cups/:id/final-date`):

- calculated cups accept a per-campaign `final_date` override (re-stamps the
  final; unfrozen earlier rounds walk back); `null` clears it; editing
  `final_offset_days` re-derives future seasons.
- fixed cups replace the date; offsets are rejected.
- **History never moves**: rounds at/below the last materialised round are
  frozen; edits on or behind it are `422`; once the final has fixtures,
  `ErrCupFinalDateLocked`. Successful edits publish a `scheduling` news story.
- `PATCH /api/admin/cups/:id/scheduling` sets the cup's `allowed_weekdays` for
  future campaigns.

## 8.4 Golden goal

There is no extra time and no penalty shootout. A knockout tie level after 90':

1. the engine keeps simulating deterministically in sudden-death mode;
2. the first goal wins; the persisted result records the deciding minute (>90),
   e.g. `2-1 (96')`;
3. the same seed always reproduces the same golden goal.

**Live pacing shortcut.** A level tie can need hundreds of extra simulated
minutes, so the live step after 90 flushes the whole extra-time block at once:
the tie ends one `tick.match_cadence` later, and the feed still shows the true
minute (`2-1 (133')`). The engine's extra-time loop is bounded so an unbreakable
tie still returns. League fixtures never use golden goal.

## 8.5 Regional cups (IM06–IM08, OPD-30, OPD-35)

### Building blocks

- **Region** — a world-scoped group of countries ([Ch. 5](05-worlds-and-setup.md)).
- **Soft tier** (`competition.competitions.tier`) — a label used by the wizard's
  default bands and by tier precedence (§8.6). It **never filters** entrants.
- **Position band** — a `competition.cup_qualification` row mapping a cup to a
  league with `from_position..to_position` (`NULL` = through last place),
  evaluated on the league's **last completed season**. Bands on the same league
  may not overlap; every banded league must be inside the cup's region
  (`ErrRegionMismatch`).
- **Reputation default band** (wizard suggestion only, `DefaultBandForReputation`):
  league rep ≥ 90 → `1..4`; ≥ 75 → `1..3`; ≥ 60 → `1..2`; else `1..1`.

### The qualification engine (IM07, `qualify.go`)

`Service.ComputeCupField(ctx, cupID)` is **pure (no writes)** and uses
**last-completed-season** standings only. It returns
`Field{Entrants, Conflicts, Unavailable, Cup, ClubCount}`:

- **Entrants** — union of bands over each banded league's last completed table,
  plus the **reigning champion +1** rule.
- **Unavailable** — banded leagues with no completed season, returned as a
  **list, never an error**. Preview warns; campaign start refuses
  (`ErrQualificationUnavailable`).
- **Conflicts** — clubs also projected into another continental cup.

**Reigning champion** = winner of the cup's latest completed campaign, resolved
to its *current* league. It always gets a seat:

| Champion's position | Result |
| --- | --- |
| league has no band / unavailable | direct entry |
| inside its league's band | keeps its rank **and** the band gains a **next-best** club (band + 1) |
| below its band | enters **out-of-band** on top of the field |

### Workflow

```bash
POST  /api/admin/cups            {region_id, tier, name, qualification:[{league_id, from_position, to_position}]}
POST  /api/admin/cups/preview    → {field, champion, warnings, field_size}   # no writes
PATCH /api/admin/cups/:id/qualification
POST  /api/admin/worlds/:id/cups/:cupID/campaign
```

Campaign start (one transaction): re-validate bands → require every banded
league to have a completed season → run the **resolution sweep** → require
`field_size ≥ 2` → write season, memberships, entries, Round 1 → anchor the
calendar to the union of participating countries (plus the champion's).

## 8.6 Double qualification and the resolution sweep (IM09, OPD-37)

A club can qualify for several continental cups (effectively always a reigning
champion). The sweep, run at every continental campaign start, leaves each club
in **exactly one**:

1. **Tier precedence (strict).** Higher tier *integer* wins (tier 3 beats tier
   1); no manager choice overrides it.
2. **Manager choice.** Among equal tiers, the club's recorded opt-in
   (`competition.manager_cup_choices`, `UNIQUE(club_id, cup_id)`) decides.
   Choosing a cup the club isn't projected for → `422`.
3. **Default.** Defend the cup it holds; else the **earlier-created** cup.
4. **Cascade.** The forfeited cup's slot goes to the next-best club past the
   band cut (`origin: cascade_replacement`, cap-exempt); that club may itself be
   double-booked, so the sweep loops to a fixpoint. Only the starting cup writes
   its field.
5. **Invariants or nothing.** If a champion would end with no cup, a cup would
   drop below 2, or a club still holds two cups → `ErrResolutionImpossible`
   (never a silent drop).

Each cascade touching the starting cup publishes a `general` news story (who
moved, both cups, the replacement or "place stays open"), tied to
`CUP_CAMPAIGN_STARTED`, in the same transaction.

Manager surface:

| Route | Purpose |
| --- | --- |
| `GET /api/clubs/:id/cup-qualifications` | projected entries, tiers, conflicts, choice, replacements |
| `POST /api/clubs/:id/cup-choices {"cup_id"}` | record the opt-in |

## 8.7 Reads

| Route | Returns |
| --- | --- |
| `GET /api/cups` | the caller's world's cups (domestic + continental; regional carry `region` + `tier`) |
| `GET /api/cups/:id` | campaign view: rounds, ties, `scheduled_at`, scores, winners |
| `GET /api/clubs/:id/fixtures` | includes cup ties |

## 8.8 Planned / deferred

- Manager-created competitions (eligibility, entry fees, invites) — S10-02.
- Two-legged ties, live extra-time streaming.
- Automatic fresh cup campaign each league season.
- Prize pools & fair-play awards — approved spec only
  (`docs/tasks/improvements/competition-prize-pools-and-fairplay.md`, OPD-34)
  ([Ch. 30](30-roadmap-and-open-decisions.md)).

## Connections

- League memberships and pacing: [Chapter 6](06-leagues-and-scheduling.md).
- Golden goal in the engine: [Chapter 15](15-match-engine.md).
- Scouting shows `golden_goal` for knockout ties: [Chapter 26](26-dashboard-news-scouting-realtime.md).
- Code: `internal/competition/{cup,regional_cup,qualify,resolve,cup_resolution,cup_choices,regions}.go`.
- Source: `docs/how-to/cup-competitions.md`, IM04–IM10, OPD-32, OPD-35, OPD-37, OPD-38.

---
[← Seasons](07-seasons-and-rollover.md) · [Contents](the-touchline-book.md) · [Next: Clubs →](09-clubs-dna-supporters.md)
