# Derby & Rivalry Determination

**Status:** Approved design direction (recorded alongside MatchSim v1.4, S04-02)
**Owner:** PM + engine (determinism contract)
**Purpose:** Pin down how a fixture is classified as a *derby* (and what its `intensity` is) so the match pipeline can feed `FixtureContext` — the object consumed by `internal/squad.ComputeMotivation` (rivalry motivation floor), card/aggression scaling in `pkg/matchsim`, and `ComputePlayerPerformanceFactor` (temperament/pressure divergence in high-stakes games). This doc exists because the realism addenda reference "derby/rivalry intensity" but never define how it is determined.

## 1. The source of truth: `club.rivalries`

A fixture between clubs X and Y is a **derby** if and only if a row exists in `club.rivalries` linking them:

```sql
CREATE TABLE club.rivalries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_a_id  UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    club_b_id  UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    intensity  INT NOT NULL DEFAULT 0 CHECK (intensity BETWEEN 0 AND 100),
    reason     TEXT,
    CHECK (club_a_id <> club_b_id),
    UNIQUE (club_a_id, club_b_id)
);
```

`intensity` is graded 0–100. Rivalries are **club-scoped (intra-world)**: `club.rivalries` has no `world_id`, and clubs are already world-scoped, so a rivalry row is implicitly within one world. Cross-world derbies do not exist here.

## 2. Derby rule (deterministic)

For any fixture with home club H and away club A:

```
IsDerby       = rivalry(H, A) exists  AND  intensity >= DerbyIntensityThreshold
DerbyIntensity = rivalry(H, A).intensity   (0 when the fixture is not a derby)
```

- `DerbyIntensityThreshold` is tuning data (proposal: **60**, set in `internal/squad`'s config block; any rivalry with `intensity >= 60` is a derby — a 0–59 intensity row keeps the rivalry bookkeeping/narrative but does not unlock the engine's derby mechanics).
- The lookup order is indifferent to direction: `(club_a_id, club_b_id)` and `(club_b_id, club_a_id)` are the same rivalry; the engine queries both arrangements.

## 3. How rivalries are established

Two entry paths:

1. **At club creation (preferred, forward-looking):** club-creation entries accept an optional `rivalry_club_ids` field. The creating layer (currently `competition.Seeding` for AI clubs; the S12-01 manager club-creation flow later) writes one `club.rivalries` row per rival with a **fixed intensity** (derby: `intensity = 80` proposal for city/regional rivals; otherwise `intensity` derived from `competitor_archetype` or a deterministic seeded draw in `[1, 99]`). The user's direction: "during club creation we can have rivalry club id that we can use to fix that."
2. **Ad hoc (admin/engine later):** rivalries may be added by an admin/data path for narrative reasons (historic clubs in a country sharing a league, supporter friction). Same table, same rule.

At the S04-02 seeding stage both clubs are created inside the `SeedCompetition` transaction; rivalry rows are written there so `FixtureContext` can be derived at match aggregation time with zero extra schema.

## 4. Where the derby flag is consumed

- **`FixtureContext.IsDerby` / `.DerbyIntensity`** — set during the pre-match aggregation in `internal/match`, from `club.rivalries` data for the two clubs.
- **Motivation floor** (`internal/squad.ComputeMotivation`): a derby sets a motivation floor near 1.0 or above regardless of `club_dna.competitive_ambition` — *nobody phones in a derby*.
- **Card/aggression** (`pkg/matchsim`): `Team.RivalryIntensity` feeds card probability.
- **Individual performance** (`ComputePlayerPerformanceFactor`): only high-stakes contexts (derby/six-pointer/cup) cause `temperament`/`pressure_handling` to diverge from neutral.

## 5. Non-goals / deferred

- Derby day crowd effects beyond the engine's home-advantage/referee-home-bias are out of scope for the simulation contract.
- Premier/preserved-matchday scheduling constraints around derbies are not this document's concern (`internal/competition` owns fixture scheduling).