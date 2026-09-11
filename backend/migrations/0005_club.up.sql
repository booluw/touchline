-- =====================================================================
-- SCHEMA: club
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS club;

CREATE TABLE club.clubs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id            UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    short_name          TEXT NOT NULL,
    founded_year        INT,
    country             TEXT NOT NULL,
    city                TEXT,
    tier                INT NOT NULL DEFAULT 5,     -- 1 = global elite ... 5+ regional (plan section 30)
    reputation          INT NOT NULL DEFAULT 10,
    primary_color       TEXT,
    secondary_color     TEXT,
    stadium_name        TEXT,
    stadium_capacity    INT,
    current_manager_id  UUID,                      -- FK added after manager.managers exists (see below)
    is_ai_controlled    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_clubs_world ON club.clubs(world_id);
CREATE INDEX idx_clubs_tier ON club.clubs(world_id, tier);

CREATE TABLE club.club_dna (
    club_id                  UUID PRIMARY KEY REFERENCES club.clubs(id) ON DELETE CASCADE,
    competitive_ambition     INT NOT NULL CHECK (competitive_ambition BETWEEN 0 AND 100),
    financial_philosophy     TEXT NOT NULL CHECK (financial_philosophy IN
                               ('conservative', 'balanced', 'aggressive', 'debt_tolerant', 'investor_funded', 'self_sustaining')),
    recruitment_philosophy   TEXT NOT NULL CHECK (recruitment_philosophy IN
                               ('academy_first', 'domestic_youth', 'international_scouting', 'undervalued_players',
                                'superstar_recruitment', 'free_transfers', 'veteran_leadership')),
    academy_importance       INT NOT NULL CHECK (academy_importance BETWEEN 0 AND 100),
    patience                 INT NOT NULL CHECK (patience BETWEEN 0 AND 100),
    managerial_control       INT NOT NULL CHECK (managerial_control BETWEEN 0 AND 100),
    star_power_preference    INT NOT NULL CHECK (star_power_preference BETWEEN 0 AND 100),
    wage_tolerance           INT NOT NULL CHECK (wage_tolerance BETWEEN 0 AND 100),
    selling_philosophy       TEXT NOT NULL CHECK (selling_philosophy IN
                               ('never_sell_stars', 'sell_for_large_profit', 'sell_when_replacement_exists',
                                'player_driven', 'financially_driven')),
    tactical_identity        TEXT NOT NULL CHECK (tactical_identity IN
                               ('possession', 'counterattack', 'pressing', 'defensive', 'direct', 'adaptable')),
    cultural_identity        TEXT NOT NULL CHECK (cultural_identity IN
                               ('local', 'international', 'youth_oriented', 'star_oriented', 'working_class',
                                'prestigious', 'experimental')),
    archetype                TEXT CHECK (archetype IN
                               ('giant', 'academy_club', 'moneyball_club', 'community_club', 'fallen_giant',
                                'investor_club', 'survival_club') OR archetype IS NULL)
);

CREATE TABLE club.boards (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id              UUID NOT NULL UNIQUE REFERENCES club.clubs(id) ON DELETE CASCADE,
    personality_type     TEXT NOT NULL CHECK (personality_type IN
                           ('patient_owner', 'demanding_owner', 'financial_conservative', 'academy_owner',
                            'prestige_owner', 'political_board')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only populated for 'political_board' clubs where different members have different agendas.
CREATE TABLE club.board_members (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id             UUID NOT NULL REFERENCES club.boards(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    agenda               TEXT NOT NULL,
    influence            INT NOT NULL DEFAULT 50 CHECK (influence BETWEEN 0 AND 100)
);

CREATE TABLE club.board_mandates (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id              UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    manager_id           UUID,                       -- FK added after manager.managers exists
    season               INT NOT NULL,
    category             TEXT NOT NULL CHECK (category IN ('primary', 'secondary', 'strategic', 'financial')),
    description          TEXT NOT NULL,
    target_type          TEXT NOT NULL,               -- e.g. 'league_finish', 'competition_stage', 'academy_graduates'
    target_value         TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'agreed', 'met', 'broken')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at          TIMESTAMPTZ
);
CREATE INDEX idx_mandates_club_manager ON club.board_mandates(club_id, manager_id);

CREATE TABLE club.facilities (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id              UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    facility_type        TEXT NOT NULL CHECK (facility_type IN
                           ('training_ground', 'medical', 'youth_facility', 'stadium', 'scouting_network')),
    level                INT NOT NULL DEFAULT 1 CHECK (level BETWEEN 1 AND 10),
    upgraded_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_facilities_club_type ON club.facilities(club_id, facility_type);

CREATE TABLE club.academies (
    club_id              UUID PRIMARY KEY REFERENCES club.clubs(id) ON DELETE CASCADE,
    is_active            BOOLEAN NOT NULL DEFAULT TRUE,
    coaching_level       INT NOT NULL DEFAULT 1 CHECK (coaching_level BETWEEN 1 AND 10),
    recruitment_level    INT NOT NULL DEFAULT 1 CHECK (recruitment_level BETWEEN 1 AND 10),
    regional_reach       TEXT[] NOT NULL DEFAULT '{}', -- country/region codes the academy recruits from
    reputation           INT NOT NULL DEFAULT 10,
    shutdown_at          TIMESTAMPTZ,
    reopened_at          TIMESTAMPTZ
);

CREATE TABLE club.supporter_groups (
    club_id                UUID PRIMARY KEY REFERENCES club.clubs(id) ON DELETE CASCADE,
    patience               INT NOT NULL CHECK (patience BETWEEN 0 AND 100),
    ambition                INT NOT NULL CHECK (ambition BETWEEN 0 AND 100),
    loyalty                  INT NOT NULL CHECK (loyalty BETWEEN 0 AND 100),
    identity                  TEXT NOT NULL,
    rivalry_intensity_base     INT NOT NULL DEFAULT 0,
    financial_sensitivity       INT NOT NULL CHECK (financial_sensitivity BETWEEN 0 AND 100),
    current_sentiment            INT NOT NULL DEFAULT 50 CHECK (current_sentiment BETWEEN 0 AND 100)
);

CREATE TABLE club.club_history (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id              UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    season               INT NOT NULL,
    event_type           TEXT NOT NULL CHECK (event_type IN
                           ('trophy', 'promotion', 'relegation', 'record_transfer', 'biggest_win',
                            'biggest_defeat', 'academy_graduate', 'financial_crisis', 'ownership_change')),
    description          TEXT NOT NULL,
    related_event_id     UUID REFERENCES world.events(id),
    occurred_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_club_history_club ON club.club_history(club_id, season);

CREATE TABLE club.rivalries (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_a_id            UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    club_b_id            UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    intensity            INT NOT NULL DEFAULT 0 CHECK (intensity BETWEEN 0 AND 100),
    reason               TEXT,
    CHECK (club_a_id <> club_b_id),
    UNIQUE (club_a_id, club_b_id)
);

CREATE TABLE club.ownership_changes (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id                  UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    previous_owner_name      TEXT,
    new_owner_name           TEXT NOT NULL,
    new_owner_manager_id     UUID,                      -- set if a manager became owner (plan section 44)
    dna_changes              JSONB,                      -- which club_dna fields changed and how
    occurred_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

