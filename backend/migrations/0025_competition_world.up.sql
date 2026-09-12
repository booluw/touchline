-- =====================================================================
-- 0025: country-scoped league administration + live match inputs (S04-01)
--
-- Records the OPD-01 resolution (OPD-20): leagues are admin-configured per
-- country, per world. Every world owns its countries; every country owns its
-- leagues; every league owns its tier, team count, and promotion/relegation
-- rule. No league size is invented by the engine — the admin declares it.
--
-- Also lays the S04-02 replay hook: live manager inputs (substitutions,
-- tactic changes) land in match.match_inputs as an ordered stream so that
-- (seed + inputs) reproduces the identical match and a worker restart can
-- rehydrate an in-progress match.
-- =====================================================================

-- World-scoped countries: the bridge between a world and its leagues.
CREATE TABLE world.countries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id    UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    code        TEXT NOT NULL,
    name        TEXT NOT NULL,
    UNIQUE (world_id, code)
);

-- A competition/league now belongs to a country, carries an explicit tier and
-- the admin-declared size (team_count). These feed S04-01 fixture generation.
ALTER TABLE competition.competitions
    ADD COLUMN country_id  UUID REFERENCES world.countries(id),
    ADD COLUMN tier        INT,
    ADD COLUMN team_count  INT;

-- Promotion/relegation lives on the 1:1 rules row: how many move up (to the
-- league above) and down (to the league below), and WHICH leagues those are.
-- Adjacency is explicit and admin-defined, never inferred from tier ordering.
ALTER TABLE competition.competition_rules
    ADD COLUMN promotions                      INT NOT NULL DEFAULT 0,
    ADD COLUMN relegations                     INT NOT NULL DEFAULT 0,
    ADD COLUMN promotes_to_competition_id      UUID REFERENCES competition.competitions(id),
    ADD COLUMN relegates_to_competition_id     UUID REFERENCES competition.competitions(id);

-- Real-time match dispatch (S04-02) scans fixtures by (world, status, kickoff).
CREATE INDEX idx_fixtures_world_status
    ON match.fixtures(world_id, status, scheduled_at);

-- Ordered live-manager inputs for a match (S04-02 replay/rehydration log).
CREATE TABLE match.match_inputs (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id              UUID NOT NULL REFERENCES match.matches(id) ON DELETE CASCADE,
    world_id              UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    sequence              INT NOT NULL,
    minute                INT NOT NULL,
    kind                  TEXT NOT NULL,  -- 'substitution' | 'tactic_change'
    payload               JSONB NOT NULL,
    created_by_manager_id UUID REFERENCES manager.managers(id),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (match_id, sequence)
);
CREATE INDEX idx_match_inputs_match ON match.match_inputs(match_id, sequence);