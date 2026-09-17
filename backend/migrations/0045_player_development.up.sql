-- =====================================================================
-- SCHEMA: player — development engine foundations (S08-02)
--
--   1. player.player_appearances is extended with the per-match rating
--      and goal/assist tallies the engine's v1.6 attribution pass emits
--      (the real per-player performance signal driving development).
--      rating is NULL for matches recorded before the engine produced
--      per-player ratings; the tallies default 0 for those.
--   2. player.player_development is the durable weekly development pass
--      state: last evaluated week (idempotency alongside the training
--      plan's last_applied_week stamp), cumulative development weeks,
--      consecutive stagnant weeks (lack of growth), the remaining lifetime
--      potential flex expansions, and the week a player's potential
--      ceiling became locked.
-- =====================================================================

ALTER TABLE player.player_appearances
    ADD COLUMN rating   SMALLINT CHECK (rating BETWEEN 1 AND 10),
    ADD COLUMN goals    SMALLINT NOT NULL DEFAULT 0 CHECK (goals >= 0),
    ADD COLUMN assists  SMALLINT NOT NULL DEFAULT 0 CHECK (assists >= 0);

CREATE TABLE player.player_development (
    player_id                  UUID PRIMARY KEY REFERENCES player.players(id) ON DELETE CASCADE,
    last_eval_week             BIGINT NOT NULL DEFAULT 0,
    cum_dev_weeks              INT    NOT NULL DEFAULT 0,
    consecutive_stagnant_weeks INT    NOT NULL DEFAULT 0,
    potential_expansions_remaining INT NOT NULL DEFAULT 3 CHECK (potential_expansions_remaining BETWEEN 0 AND 5),
    potential_locked_week      BIGINT,
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);