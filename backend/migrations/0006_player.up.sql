-- =====================================================================
-- SCHEMA: player
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS player;

CREATE TABLE player.players (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id                 UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    person_id                UUID NOT NULL UNIQUE REFERENCES person.people(id) ON DELETE CASCADE,
    club_id                  UUID REFERENCES club.clubs(id),  -- null = free agent
    primary_position          TEXT NOT NULL CHECK (primary_position IN
                               ('GK','CB','LB','RB','DM','CM','AM','LM','RM','LW','RW','ST')),
    secondary_positions        TEXT[] NOT NULL DEFAULT '{}',
    squad_number                INT,
    market_value                 NUMERIC(14,2) NOT NULL DEFAULT 0,
    status                        TEXT NOT NULL DEFAULT 'active' CHECK (status IN
                                  ('active', 'injured', 'suspended', 'on_loan', 'retired', 'free_agent')),
    is_academy_product             BOOLEAN NOT NULL DEFAULT FALSE,
    developed_by_manager_id         UUID,                        -- "Developed by manager X" — plan section 61
    created_at                       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_players_world ON player.players(world_id);
CREATE INDEX idx_players_club ON player.players(club_id);
CREATE INDEX idx_players_status ON player.players(world_id, status);

-- Football attributes as EAV rather than fixed columns: the attribute set
-- (technical/physical/mental/tactical/goalkeeping/positional sub-skills)
-- is large, position-dependent, and expected to be tuned over time without
-- a schema migration for every balance change.
CREATE TABLE player.player_attributes (
    player_id             UUID NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    attribute_category    TEXT NOT NULL CHECK (attribute_category IN
                           ('technical', 'physical', 'mental', 'tactical', 'goalkeeping', 'positional')),
    attribute_key         TEXT NOT NULL,             -- e.g. 'passing', 'pace', 'composure'
    value                 INT NOT NULL CHECK (value BETWEEN 1 AND 100),
    PRIMARY KEY (player_id, attribute_category, attribute_key)
);

CREATE TABLE player.player_hidden_traits (
    player_id                 UUID PRIMARY KEY REFERENCES player.players(id) ON DELETE CASCADE,
    potential                  INT NOT NULL CHECK (potential BETWEEN 1 AND 100),
    potential_ceiling_locked     BOOLEAN NOT NULL DEFAULT FALSE, -- becomes TRUE once potential stops flexing (plan sec. 25)
    consistency                   INT NOT NULL CHECK (consistency BETWEEN 1 AND 100),
    injury_susceptibility           INT NOT NULL CHECK (injury_susceptibility BETWEEN 1 AND 100),
    adaptability                      INT NOT NULL CHECK (adaptability BETWEEN 1 AND 100),
    professionalism                    INT NOT NULL CHECK (professionalism BETWEEN 1 AND 100),
    ambition                             INT NOT NULL CHECK (ambition BETWEEN 1 AND 100),
    loyalty                                INT NOT NULL CHECK (loyalty BETWEEN 1 AND 100),
    temperament                              INT NOT NULL CHECK (temperament BETWEEN 1 AND 100),
    pressure_handling                          INT NOT NULL CHECK (pressure_handling BETWEEN 1 AND 100),
    learning_speed                               INT NOT NULL CHECK (learning_speed BETWEEN 1 AND 100)
);

CREATE TABLE player.player_personality (
    player_id                 UUID PRIMARY KEY REFERENCES player.players(id) ON DELETE CASCADE,
    professionalism             INT NOT NULL CHECK (professionalism BETWEEN 1 AND 100),
    ambition                      INT NOT NULL CHECK (ambition BETWEEN 1 AND 100),
    loyalty                         INT NOT NULL CHECK (loyalty BETWEEN 1 AND 100),
    ego                               INT NOT NULL CHECK (ego BETWEEN 1 AND 100),
    sociability                        INT NOT NULL CHECK (sociability BETWEEN 1 AND 100),
    adaptability                         INT NOT NULL CHECK (adaptability BETWEEN 1 AND 100),
    patience                               INT NOT NULL CHECK (patience BETWEEN 1 AND 100),
    leadership                               INT NOT NULL CHECK (leadership BETWEEN 1 AND 100),
    emotional_volatility                       INT NOT NULL CHECK (emotional_volatility BETWEEN 1 AND 100)
);

-- Free-form personal preferences (preferred teammates, disliked teammates,
-- favourite clubs, preferred countries, language, etc — plan section 11).
CREATE TABLE player.player_preferences (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id             UUID NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    preference_type       TEXT NOT NULL CHECK (preference_type IN
                           ('preferred_teammate', 'disliked_teammate', 'favourite_club', 'former_club',
                            'preferred_country', 'preferred_manager', 'family_location', 'language',
                            'playing_time_expectation', 'wage_expectation')),
    preference_value      TEXT NOT NULL,
    strength              INT DEFAULT 50 CHECK (strength BETWEEN 0 AND 100)
);
CREATE INDEX idx_player_prefs_player ON player.player_preferences(player_id, preference_type);

CREATE TABLE player.player_emotional_states (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id             UUID NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    emotional_state       TEXT NOT NULL CHECK (emotional_state IN
                           ('happy', 'content', 'motivated', 'frustrated', 'anxious', 'angry', 'homesick',
                            'excited', 'betrayed', 'ambitious', 'confident', 'isolated')),
    cause                 TEXT NOT NULL,             -- e.g. 'dropped_from_starting_xi', 'promise_broken'
    intensity             INT NOT NULL CHECK (intensity BETWEEN 1 AND 100),
    related_event_id      UUID REFERENCES world.events(id),
    occurred_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at            TIMESTAMPTZ
);
CREATE INDEX idx_emotional_states_player ON player.player_emotional_states(player_id, occurred_at DESC);

CREATE TABLE player.contracts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id             UUID NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    club_id               UUID NOT NULL REFERENCES club.clubs(id),
    weekly_wage           NUMERIC(14,2) NOT NULL,
    signing_bonus         NUMERIC(14,2) NOT NULL DEFAULT 0,
    start_date            DATE NOT NULL,
    end_date              DATE NOT NULL,
    release_clause        NUMERIC(14,2),
    playing_time_promise  TEXT,               -- free text summary; structured version lives in social.promises
    status                TEXT NOT NULL DEFAULT 'active' CHECK (status IN
                           ('active', 'expired', 'terminated', 'renewed')),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_contracts_player ON player.contracts(player_id, status);
CREATE INDEX idx_contracts_club_end ON player.contracts(club_id, end_date);

CREATE TABLE player.player_history (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id             UUID NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    world_id              UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    season                INT NOT NULL,
    club_id               UUID REFERENCES club.clubs(id),
    event_type            TEXT NOT NULL CHECK (event_type IN
                           ('debut', 'goal_milestone', 'award', 'transfer', 'loan', 'injury_return',
                            'retirement', 'international_call_up')),
    description           TEXT NOT NULL,
    related_event_id      UUID REFERENCES world.events(id),
    occurred_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_player_history_player ON player.player_history(player_id, season);

CREATE TABLE player.injuries (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id                 UUID NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    injury_type                TEXT NOT NULL CHECK (injury_type IN
                                ('muscle', 'ligament', 'bone', 'concussion', 'illness', 'recurring')),
    severity                     INT NOT NULL CHECK (severity BETWEEN 1 AND 10),
    expected_recovery_date        DATE NOT NULL,
    actual_recovery_date           DATE,
    recurrence_risk                  NUMERIC NOT NULL DEFAULT 0 CHECK (recurrence_risk BETWEEN 0 AND 1),
    match_id                           UUID,               -- FK added after match.matches exists (see below)
    occurred_at                          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_injuries_player ON player.injuries(player_id, occurred_at DESC);

