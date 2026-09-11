-- =====================================================================
-- SCHEMA: match
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS match;

CREATE TABLE match.fixtures (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id          UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    competition_id    UUID NOT NULL,     -- FK added after competition.competitions exists
    home_club_id      UUID NOT NULL REFERENCES club.clubs(id),
    away_club_id      UUID NOT NULL REFERENCES club.clubs(id),
    matchday          INT,
    scheduled_at      TIMESTAMPTZ NOT NULL,
    status            TEXT NOT NULL DEFAULT 'scheduled' CHECK (status IN
                       ('scheduled', 'live', 'completed', 'postponed', 'cancelled')),
    CHECK (home_club_id <> away_club_id)
);
CREATE INDEX idx_fixtures_world_scheduled ON match.fixtures(world_id, scheduled_at);
CREATE INDEX idx_fixtures_clubs ON match.fixtures(home_club_id, away_club_id);

CREATE TABLE match.matches (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fixture_id        UUID NOT NULL UNIQUE REFERENCES match.fixtures(id) ON DELETE CASCADE,
    world_id          UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    seed              BIGINT NOT NULL,     -- deterministic replay input (plan section 9/68)
    engine_version    TEXT NOT NULL,
    home_score        INT,
    away_score        INT,
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'in_progress', 'completed')),
    started_at        TIMESTAMPTZ,
    ended_at          TIMESTAMPTZ
);
CREATE INDEX idx_matches_world_status ON match.matches(world_id, status);

CREATE TABLE match.match_events (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id              UUID NOT NULL REFERENCES match.matches(id) ON DELETE CASCADE,
    sequence              INT NOT NULL,           -- ordering within the match, independent of minute
    minute                INT NOT NULL,
    event_type            TEXT NOT NULL CHECK (event_type IN
                           ('kickoff', 'goal', 'assist', 'yellow_card', 'red_card', 'substitution',
                            'injury', 'chance_created', 'penalty_awarded', 'penalty_scored',
                            'penalty_missed', 'half_time', 'full_time')),
    club_id               UUID REFERENCES club.clubs(id),
    player_id             UUID REFERENCES player.players(id),
    related_player_id     UUID REFERENCES player.players(id), -- assist provider, sub replaced, etc.
    detail                JSONB,     -- free-text commentary line, xG, etc. — powers the text-feed viewer
    UNIQUE (match_id, sequence)
);
CREATE INDEX idx_match_events_match ON match.match_events(match_id, sequence);

ALTER TABLE player.injuries
    ADD CONSTRAINT fk_injuries_match FOREIGN KEY (match_id) REFERENCES match.matches(id);

