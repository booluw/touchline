-- =====================================================================
-- SCHEMA: manager
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS manager;

CREATE TABLE manager.managers (
    id                            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id                      UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    person_id                     UUID REFERENCES person.people(id),   -- null for AI-only managers with no person record
    user_id                       UUID REFERENCES auth.users(id),      -- null for pure AI-controlled managers
    current_club_id               UUID REFERENCES club.clubs(id),      -- enforced: one active club at a time (see below)
    status                        TEXT NOT NULL DEFAULT 'unemployed' CHECK (status IN
                                   ('active', 'unemployed', 'sabbatical', 'retired')),
    coaching_ability               INT NOT NULL DEFAULT 50 CHECK (coaching_ability BETWEEN 1 AND 100),
    preferred_tactical_identity    TEXT,
    risk_tolerance                 INT DEFAULT 50 CHECK (risk_tolerance BETWEEN 0 AND 100),
    is_policy_bot                  BOOLEAN NOT NULL DEFAULT FALSE,   -- TRUE for AI-club decision actor (plan section 10)
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_managers_world ON manager.managers(world_id);
CREATE INDEX idx_managers_user ON manager.managers(user_id);

-- Enforce "one manager, one club, at a time" (resolved open question #2):
-- a manager can only be `current_club_id IS NOT NULL` while status = 'active',
-- and no two active managers can point at the same club.
CREATE UNIQUE INDEX uq_manager_one_active_club
    ON manager.managers(current_club_id)
    WHERE status = 'active' AND current_club_id IS NOT NULL;

ALTER TABLE club.clubs
    ADD CONSTRAINT fk_clubs_current_manager
    FOREIGN KEY (current_manager_id) REFERENCES manager.managers(id);

ALTER TABLE club.board_mandates
    ADD CONSTRAINT fk_mandates_manager
    FOREIGN KEY (manager_id) REFERENCES manager.managers(id);

ALTER TABLE player.players
    ADD CONSTRAINT fk_players_developed_by
    FOREIGN KEY (developed_by_manager_id) REFERENCES manager.managers(id);

ALTER TABLE club.ownership_changes
    ADD CONSTRAINT fk_ownership_new_owner_manager
    FOREIGN KEY (new_owner_manager_id) REFERENCES manager.managers(id);

-- Append-only reputation log — never a mutable score column (resolved open
-- question #2). `world_id IS NULL` rows are global/display-only career
-- history; `world_id IS NOT NULL` rows are what hiring logic in that
-- specific world actually reads.
CREATE TABLE manager.manager_reputation_events (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id             UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE,
    world_id               UUID REFERENCES world.worlds(id),   -- NULL = global career-history entry
    category               TEXT NOT NULL CHECK (category IN
                            ('trophies', 'promotions', 'player_development', 'finances',
                             'tactical_innovation', 'academy_success', 'media_presence')),
    delta                  INT NOT NULL,
    reason                 TEXT NOT NULL,
    related_event_id       UUID REFERENCES world.events(id),
    occurred_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_manager_rep_manager ON manager.manager_reputation_events(manager_id, world_id);

CREATE TABLE manager.manager_history (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id            UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE,
    world_id              UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    club_id               UUID NOT NULL REFERENCES club.clubs(id),
    role                  TEXT NOT NULL DEFAULT 'manager' CHECK (role IN ('manager', 'assistant', 'owner', 'chairman')),
    start_date            DATE NOT NULL,
    end_date              DATE,
    outcome_summary       TEXT,               -- "won three league titles, produced six academy graduates" (sec. 61)
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_manager_history_club ON manager.manager_history(club_id, start_date);
CREATE INDEX idx_manager_history_manager ON manager.manager_history(manager_id);

CREATE TABLE manager.job_security_snapshots (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id                 UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE,
    club_id                    UUID NOT NULL REFERENCES club.clubs(id),
    world_tick                 BIGINT NOT NULL,
    performance_score          INT NOT NULL,
    expectations_score         INT NOT NULL,
    financial_score             INT NOT NULL,
    board_relationship_score    INT NOT NULL,
    club_dna_alignment_score    INT NOT NULL,
    supporter_sentiment_score   INT NOT NULL,
    alternatives_score          INT NOT NULL,
    total_score                 INT NOT NULL,
    explanation                 JSONB NOT NULL,  -- Explanation/ExplanationFactor breakdown (plan section 8)
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_job_security_manager ON manager.job_security_snapshots(manager_id, world_tick DESC);

