# pkg/matchsim — Addendum v1.6 (per-player attribution and match ratings)

Status: **PROPOSAL** — structural part of S08-02 (dynamic player development) so
the engine can emit player-linked events and per-player 1–10 match ratings. The
rating weights are the **proposal** block in `Tuning.Ratings` (data-only swap on
sign-off). Companion to: `translate-v5.md`'s caster (moved in below),
`docs/design/development-numerics.md`, `docs/tasks/S08-02-*.md`.

This addendum makes the match feed and the score sheet **player-aware**: every
castable event links to a player, and each side's appearances carry minutes,
goals, assists, and a 1–10 rating. To do that without touching the v1.5 replay
contract, attribution runs as a **strict post-pass over its own RNG streams**:

- The canonical draw order (addendum v1.4 Part 3 / v1.5) is byte-for-byte
  unchanged. `Simulate` now finishes the canonical pass, **then** walks the
  finished feed and casts events to players.
- Each side's attribution draws come from an **independent per-side stream**
  seeded `matchSeed ⊕ fnv64a(club UUID bytes) ⊕ castTag`. The first two terms
  reproduce the orchestration layer's pre-v1.6 caster exactly
  (`internal/match/caster.go`), so an already-started match, re-simulated under
  v1.6, links the **same** players it would have linked live.
- When a side supplies no `Team.Lineups`, it is skipped entirely: its events
  keep blank player ids and no ratings are produced (lineup-less callers such as
  the golden replays are numerically untouched — `canonicalDigest` is unchanged).

## What is new

1. **`Team.Lineups *PlayerLineups`** — optional, per side:
   `{ XI []PlayerRef, Bench []PlayerRef, Taker string }`, with
   `PlayerRef{ ID, Position, Weight }`. `Position` uses the canonical keys
   (`GK/RB/LB/CB/DM/CM/AM/RW/LW/ST`); `Weight` mirrors the orchestration
   layer's attribute weight (the continuity of `squad.MemberWeight`).
   Absent or empty lineups disable attribution for that side.
2. **Event linkage** — `MatchEvent` gains `PlayerID` (primary) and
   `RelatedPlayerID` (secondary); marshalled `player_id` / `related_player_id`.
   The linkage contract matches the persisted convention:
   - Goal → `{player}` = scorer; assist → `{player}` = assister,
     `related` = the last open-play scorer; `{player}`/`{assist}` tokens in
     descriptions were already placeholders for this role.
   - Substitution → `player_id` = the player coming **ON**, `related_player_id`
     = the player going **OFF** (or the injured player).
   - Cards/chance → the cast member; red card + injury remove the player from
     the on-pitch set; injury marks its sub as pending.
   - Penalty scored/missed → the designated taker when set, else a cast draw.
   - Structural markers (kickoff/half/full time, penalty awarded) stay blank.
3. **`MatchResult.HomePlayerRatings / AwayPlayerRatings []PlayerRating`** —
   an entry per XI member plus every sub-in:
   `PlayerRating{ PlayerID, Minutes, Goals, Assists, Chances, YellowCards,
   RedCards, PenaltiesScored, PenaltiesMissed, Rating }`. Minutes derive from
   the XI (90) adjusted by each substitution's minute; `Goals` counts open-play
   goals only — penalties are `PenaltiesScored`, so
   `score = Σ(Goals) + Σ(PenaltiesScored)` (the persisted
   `player_appearances.goals` sums both). Ratings are clamped integers in
   `[1,10]`.
4. **`Tuning` gains `Ratings RatingsSpec`** with the proposal block
   (below), plus the version bump to `EngineVersion = "1.6-proposal"`.

## Attribution pass

Runs after the final full-time emission, before `Simulate` returns:

```
for each side with Lineups:
    state = { stream: newSplitMix64(matchSeed ⊕ fnv64a(clubID) ⊕ castTag),
              onPitch: XI, off: {}, bench, taker, pendingSub, lastScorer }
for each event in finished feed:
    if no state for event.ClubID → skip
    linked = cast(state, event)           # goal/assist/chance/card/pens as caster
    if event is substitution:
        if manager LiveInput exists at (minute, club) AND NOT injury-derived:
            linked = forced(subIn, subOut)   # exact manager picks, zero draws
        else: linked = cast(sub)             # bench draw like the caster
    event.PlayerID, event.RelatedPlayerID = linked
    accrue tallies (goal/assist/chance/cards/pens; substitution minutes)
ratings = for each XI∪sub player: minutes from subs; matchRating(...)
```

- **Forced (manager) substitutions** — an engine substitution event already
  consumed the manager's `LiveInput` at the canonical window. The pass replays
  that same input: `substitutionAt(minute, clubID)` and its
  `Detail["player_in"] / Detail["player_out"]` become the linked ids (S04-02 /
  OPD-21 behavior preserved: the feed shows the players the manager actually
  picked). The lone exception is the **injury-derived** substitution: an injury
  at the same minute immediately followed by a substitution is the forced
  injury change, which keeps the caster's pending-player linkage
  (`related` = the injured player).
- **Determinism** — every draw consumed by the pass comes from the side's own
  stream; the canonical pass never sees it. `seed + ordered inputs + lineups ⇒`
  identical events, linkage, and ratings. `TestAttributionIsDeterministic` and
  `TestAttributionLeavesCanonicalFeedUntouched` pin both invariants.

## Rating formula (proposal)

Player rating from the event tallies, clamped to `[1,10]` integers:

```
raw = Base
      + Goals×Goal + Assists×Assist + Chances×Chance
      + YellowCards×Yellow + RedCards×Red
      + PenaltiesScored×PenaltyScored + PenaltiesMissed×PenaltyMissed
if Minutes < HalfMinutes: raw = Base + (raw − Base) × Minutes/HalfMinutes   # part-time proration
rating = round(raw), clamped [1,10]
```

| Spec | Value (proposal) |
| --- | --- |
| Base | `6.0` |
| Goal | `0.6` |
| Assist | `0.3` |
| Chance | `0.05` |
| Yellow | `−0.3` |
| Red | `−1.0` |
| PenaltyScored | `0.4` |
| PenaltyMissed | `−0.6` |
| HalfMinutes | `45` |

A 90-minute starters' baseline (6.0 + a chance or two) lands in the 6–7 band;
goals lift into 8–9; a red card drags toward 4–5; a 10-minute cameo with one
positive action sits near base (proration). Consumed by the S08-02 development
pass (`player.player_appearances.rating`), the morale pass, and any future
rating surfaced in the app. Weights are the proposal block — flipping them is a
data change with a tuning version bump.

## Versioning

- New `Tuning.Version` + `EngineVersion = "1.6-proposal"`; `match.matches.
  engine_version` records it per match.
- Canonical RNG consumption is unchanged, so lineup-less matches (and the v1.5
  golden digest) are numerically identical to v1.5. Attribution is additive for
  lineup-carrying matches; replay exactness applies within one engine version
  (documented limitation, unchanged).

## Orchestration contract (S08-02)

1. `internal/match` stamps `Team.Lineups` (XI/Bench/Taker from the kickoff
   fixture) into the match inputs snapshot; the engine's post-pass links events
   and produces ratings.
2. `internal/match` stops casting samples itself (`caster.go` is removed) and
   persists the engine's linkage once per event.
3. On finalizing, appearances are built from `HomePlayerRatings` /
   `AwayPlayerRatings` + the fixture XI (authoritative minutes, replacing the
   legacy substitution-feed reconstruction) and written to
   `player.player_appearances` including `rating`, `goals`, `assists`.
4. Legacy matches rehydrated for continuation keep blank linkage (no lineups in
   their snapshots) — they simply never cast, and their appearances stay as
   recorded.