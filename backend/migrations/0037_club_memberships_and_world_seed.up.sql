-- =====================================================================
-- 0037: season-independent club↔competition membership + world seed
-- (launch-model seeding, S04-01)
--
-- The launch model separates "materialize the world" (seed = clubs +
-- players + memberships) from "run a season" (seasons + fixtures). Until now
-- the only club↔competition link was competition.competition_entries, which
-- is SEASON-scoped — a club could not belong to a competition outside a
-- season, and the old fused seed created seasons just to record membership.
--
-- club_competitions is the season-independent membership table (OPD-01/OPD-20
-- analogue for leagues + the future knockout cups):
--   1. one league per club is enforced at the schema level (partial unique
--      index on role='league');
--   2. cup membership (role='cup', Phases 3+) is capped at 3 per club by the
--      competition service -- the column exists now so seeding can record
--      league membership without waiting for the cup implementation.
--
-- world.worlds.world_seed is the replay seed for the world's first seed run:
-- the world is incremented/expanded by re-running the seed button, and every
-- later run must reproduce the same output (or fill only the new leagues
-- deterministically) from this single stored seed.
-- =====================================================================

CREATE TABLE competition.club_competitions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id       UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    club_id        UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    competition_id UUID NOT NULL REFERENCES competition.competitions(id) ON DELETE CASCADE,
    role           TEXT NOT NULL CHECK (role IN ('league', 'cup')),
    joined_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (club_id, competition_id)
);
CREATE INDEX idx_club_competitions_competition
    ON competition.club_competitions(competition_id, role);
CREATE INDEX idx_club_competitions_club
    ON competition.club_competitions(club_id, role);
-- A club plays exactly one league at a time, enforced by the schema.
CREATE UNIQUE INDEX uq_club_one_league
    ON competition.club_competitions(club_id)
    WHERE role = 'league';

-- Replay seed for the world's first (and every subsequent) seed run.
ALTER TABLE world.worlds
    ADD COLUMN world_seed BIGINT;