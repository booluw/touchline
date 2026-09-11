-- =====================================================================
-- SCHEMA: competition
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS competition;

CREATE TABLE competition.competitions (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id                 UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    name                     TEXT NOT NULL,
    competition_type         TEXT NOT NULL CHECK (competition_type IN
                              ('league', 'domestic_cup', 'continental', 'regional', 'youth',
                               'preseason', 'international', 'custom')),
    created_by_manager_id    UUID REFERENCES manager.managers(id), -- null for system-generated competitions
    reputation                INT NOT NULL DEFAULT 10,
    prize_pool                 NUMERIC(14,2) NOT NULL DEFAULT 0,
    status                       TEXT NOT NULL DEFAULT 'active' CHECK (status IN
                                 ('proposed', 'active', 'archived')),
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_competitions_world ON competition.competitions(world_id);

ALTER TABLE match.fixtures
    ADD CONSTRAINT fk_fixtures_competition FOREIGN KEY (competition_id) REFERENCES competition.competitions(id);

-- Abuse-prevention configuration for manager-created competitions (plan section 13 / PRD section 33).
CREATE TABLE competition.competition_rules (
    competition_id                  UUID PRIMARY KEY REFERENCES competition.competitions(id) ON DELETE CASCADE,
    format                          TEXT NOT NULL CHECK (format IN ('round_robin', 'knockout', 'group_and_knockout')),
    qualification_rules             JSONB,
    min_reputation_to_enter         INT NOT NULL DEFAULT 0,
    max_prize_budget                NUMERIC(14,2),
    entry_fee                       NUMERIC(14,2) NOT NULL DEFAULT 0,
    is_home_and_away                BOOLEAN NOT NULL DEFAULT TRUE,
    scheduling_rules                JSONB,
    requires_association_approval   BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE competition.seasons (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id          UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    competition_id    UUID NOT NULL REFERENCES competition.competitions(id) ON DELETE CASCADE,
    season_label      TEXT NOT NULL,        -- e.g. "2031/32"
    season_number     INT NOT NULL,
    start_date        DATE NOT NULL,
    end_date          DATE,
    status            TEXT NOT NULL DEFAULT 'upcoming' CHECK (status IN
                       ('upcoming', 'in_progress', 'completed'))
);
CREATE UNIQUE INDEX idx_seasons_competition_number ON competition.seasons(competition_id, season_number);

CREATE TABLE competition.competition_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    season_id    UUID NOT NULL REFERENCES competition.seasons(id) ON DELETE CASCADE,
    club_id      UUID NOT NULL REFERENCES club.clubs(id),
    status       TEXT NOT NULL DEFAULT 'registered' CHECK (status IN
                  ('registered', 'qualified', 'eliminated', 'champion')),
    UNIQUE (season_id, club_id)
);

CREATE TABLE competition.standings (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    season_id        UUID NOT NULL REFERENCES competition.seasons(id) ON DELETE CASCADE,
    club_id          UUID NOT NULL REFERENCES club.clubs(id),
    played           INT NOT NULL DEFAULT 0,
    won              INT NOT NULL DEFAULT 0,
    drawn            INT NOT NULL DEFAULT 0,
    lost             INT NOT NULL DEFAULT 0,
    goals_for        INT NOT NULL DEFAULT 0,
    goals_against    INT NOT NULL DEFAULT 0,
    points           INT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (season_id, club_id)
);
CREATE INDEX idx_standings_season_points ON competition.standings(season_id, points DESC);

