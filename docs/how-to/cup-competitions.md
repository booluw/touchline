# How to: cup competitions

How a **domestic cup** (IM04) is created, who gets to play, and how ties
resolve. This is the knock-out complement to [seasons.md](seasons.md): leagues
are round-robin point-scoring seasons, cups are single-elimination brackets
with golden-goal ties.

Relates to: [seasons.md](seasons.md), [setup-and-launch.md](setup-and-launch.md)
(seed first — cups need clubs),
[cadences-and-time.md](cadences-and-time.md) (cup ties kick off on daily ticks
like any fixture), [glossary.md](glossary.md).

**In short:** an admin declares a cup for a country with two staging numbers
(how many first-tier clubs get a bye, and when they join), starts its
campaign, and every club with a league seat in that country is eligible. The
bottom-tier pool plays rounds among itself until X survivors remain; then the
top N join and the field plays out to a champion. A drawn tie is settled by a
deterministic golden goal, never a replay or a penalty shootout.

---

## 1. Who can enter (eligibility + staging)

Every club that holds a `role='league'` membership in a league whose country is
the cup's country is eligible — across all tiers, automatically. There are no
applications and no invites for the system cup (manager-created competitions
with reputation/entry criteria are the later S10-02 flow).

Two admin-set variables shape the bracket (`qualification_rules` on the cup):

| Variable | Meaning | Typical use |
| --- | --- | --- |
| **N** (`first_tier_bye`) | the top N clubs of the first tier that skip the early rounds | protect elite clubs from cramming their fixture lists |
| **X** (`survivor_threshold`) | the early rounds run among the rest until exactly X are left; then the N join | tune how many "giant-killing" rounds there are |

Formally: the eligible pool P is every country league club; the bottom pool is
P minus the top N of tier 1. Rounds 1..k knock the bottom pool down to exactly
X survivors; then the field becomes **X + N** and plays to a final of two.
"Top N" is ranked by most-recent standings
(points → goal difference → goals scored → club name); before a tier-1 season
has results, by the deterministic club order, then name.

The staging must produce a clean bracket — even ties in every round, two clubs
in the final — so the cup refuses to start (`422`) rather than emit a broken
one. If the bottom pool already equals X, the top N enter immediately; if it is
smaller than X the admin must lower X.

## 2. Creating a cup and starting its campaign

Both steps are admin-only and mirror the league flow ([setup-and-launch.md](setup-and-launch.md) Step 7).

```bash
# 1. Declare the cup (a country, a name, and the two staging numbers).
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/cups \
  -d '{"world_id":"'$WORLD_ID'","country_id":"'$COUNTRY_ID'",
       "name":"FA Cup","first_tier_bye":8,"survivor_threshold":14}'
# 201 → { "id": ..., "name": "FA Cup", "qualification_rules": {...} }

# 2. Start the campaign: brackets + memberships + Round-1 fixtures.
curl -c /tmp/jar -b /tmp/jar -X POST \
  localhost:8080/api/admin/worlds/$WORLD_ID/countries/$COUNTRY_ID/cups/$CUP_ID/campaign
# 201 → { "season": {...}, "round_1": { "ties": [...] }, "entrants": 16 }
```

What the campaign step does, in one transaction:

1. Writes a `competition.seasons` row (`status 'in_progress'`) for the cup.
2. Inserts the `role='cup'` memberships (`club_competitions`) and
   `competition_entries` for every genuinely drawn-in club — capped at **3
   cups per club**, the deferred contract from migration 0037.
3. Generates **Round 1** only: pairings and byes drawn deterministically from
   the world replay seed over the canonically-sorted bottom pool.

A second campaign call while one is live returns `409` (one campaign per cup,
mirroring `ErrLeagueAlreadySeeded` for leagues).

## 3. How the bracket plays out

- Later rounds are **materialized only when a round completes**: when the last
  tie of a round gets its result, the winners (canonical order) are drawn into
  the next round's fixtures in the same transaction. Nothing is pre-scheduled
  past the known clubs — there is no dangling "TBD vs TBD" fixture.
- Every draw is a pure function of `world_seed ⊕ cup_id ⊕ round` over the
  advancing set, so identical seeds and identical results reproduce identical
  brackets (same replay guarantee league seeding has).
- **Round pacing reuses the IM03 machinery:** each round is a fixture day
  spaced by the cup's `scheduling_rules` (default `days_per_week` spacing) at a
  kickoff hour from the kickoff rotation; `matchday` on the fixture row doubles
  as the round index. Beware: the **week-grouped season calendar
  (`GET /api/competitions/:id/calendar`) is league-shaped** — cups are read
  through their own round view instead.
- Ties are single-legged; home advantage is drawn deterministically.
- When the final's result is applied, the winner's entry becomes `champion`,
  every other entry is `eliminated`, and the cup season closes
  (`status 'completed'`). Cup results **never** write `competition.standings`
  and never trigger the country promotion/relegation rollover.

## 4. Golden goal (how draws are decided)

There is no extra time and no penalty shootout (matchsim implements neither).
If a cup tie is level after its regulation 90 minutes:

1. The engine keeps simulating deterministically, minute by minute, in
   sudden-death mode.
2. The first goal wins — that is the *golden goal*. The persisted result holds
   the deciding minute (>90), so the event stream shows e.g. `2-1 (96')`.
3. Because it is derived from the match seed, the same tie re-simulates to the
   same golden goal every time (live re-run and quick-play agree).

Regulation-state matches and league fixtures are unaffected: golden-goal mode
only engages for knockout fixtures that are level at 90'.

## 5. Reading cups

| Endpoint | Who | Returns |
| --- | --- | --- |
| `GET /api/cups` | manager | the caller's country cups |
| `GET /api/cups/:id` | manager (world-scoped) | the campaign: seasons, rounds, each tie with `scheduled_at`, `home/away`, `status`, scores, `winner` |
| `GET /api/clubs/:id/fixtures` | manager | a club's fixtures across *all* competitions — cup ties already appear here (IM03) |
| `GET /api/matches/:id/events` / matches feeds | manager | governed by tie `status`, like league matches |

The frontend shows a cup page with the rounds list (kickoff day/time from
`scheduled_at`, results, champion highlight); the admin console has the
create-cup form and the "start campaign" action.

## Recorded decisions

- System-seeded domestic cup first; **manager-created** competitions
  (eligibility evaluation, entry fees, invites, approval) remain S10-02 — this
  feature ships the bracket + entry machinery they will reuse.
- Eligibility = all country league clubs; the top N of tier 1 join late when X
  bottom-tier survivors remain; N and X are admin-configurable.
- Draws resolve by golden goal only; no penalties, no dedicated ET periods.
- Single-legged ties by default; two-legged aggregate rounds and live
  extra-time streaming are later tasks.
- Re-creating a fresh cup campaign each league season (cup reset when a league
  rolls over) is a later task — cups and leagues run independently today.
- A club's cup memberships are capped at 3 by the competition service
  (`ErrCupLimit`).