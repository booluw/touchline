-- =====================================================================
-- 0050: cup knockout bracket (IM04)
--
-- The per-round participant structure of a knockout cup campaign. Round 1
-- rows are written at campaign start; each later round's rows are appended
-- inside the result transaction when its preceding round completes, so the
-- advancing set (winners ∪ byes) is known without scanning fixtures back.
-- `seed` is the positional draw order inside the round: the first 2×ties rows
-- are the pairings (fixtures reference the same draw order via scheduled_at),
-- and the trailing is_bye rows are the clubs that advance without a tie. The
-- draw itself is deterministic from world_seed ⊕ cup_id ⊕ round over the
-- canonically sorted advancing set, so the table is a read-back, never an
-- independent source of truth.
-- =====================================================================
CREATE TABLE competition.cup_bracket (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    season_id UUID NOT NULL REFERENCES competition.seasons(id) ON DELETE CASCADE,
    round     INT  NOT NULL,
    seed      INT  NOT NULL,
    club_id   UUID NOT NULL REFERENCES club.clubs(id),
    is_bye    BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (season_id, round, seed)
);
CREATE INDEX idx_cup_bracket_round ON competition.cup_bracket(season_id, round);