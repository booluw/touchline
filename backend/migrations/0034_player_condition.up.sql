-- =====================================================================
-- SCHEMA: player — match-condition trackers (S05-01)
-- =====================================================================

-- Per-player match condition dims consumed at matchday by internal/match and
-- updated every weekly training tick (docs/design/tactics-training-numerics.md
-- §2). All in [0,1]. injury_risk starts from hidden injury_susceptibility
-- (susceptibility/200) for generated squads; pre-existing players get the
-- column default until the weekly handler touches them.
CREATE TABLE player.player_condition (
    player_id              UUID PRIMARY KEY REFERENCES player.players(id) ON DELETE CASCADE,
    fatigue                NUMERIC(5,4) NOT NULL DEFAULT 0    CHECK (fatigue BETWEEN 0 AND 1),
    fitness                NUMERIC(5,4) NOT NULL DEFAULT 1    CHECK (fitness BETWEEN 0 AND 1),
    sharpness              NUMERIC(5,4) NOT NULL DEFAULT 0.5  CHECK (sharpness BETWEEN 0 AND 1),
    injury_risk            NUMERIC(5,4) NOT NULL DEFAULT 0.25 CHECK (injury_risk BETWEEN 0 AND 1),
    tactical_familiarity   NUMERIC(5,4) NOT NULL DEFAULT 0.5  CHECK (tactical_familiarity BETWEEN 0 AND 1),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);