-- S06-05: manager delegation policies + absence mode.

-- Manager absence tracking.
ALTER TABLE manager.managers
    ADD COLUMN away_since           TIMESTAMPTZ,
    ADD COLUMN away_auto            BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN consecutive_missed   INT NOT NULL DEFAULT 0,
    ADD COLUMN last_activity_at     TIMESTAMPTZ;

-- Per-manager delegation policies.
CREATE TABLE manager.policies (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id              UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    manager_id            UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE,
    policy_type           TEXT NOT NULL CHECK (policy_type IN ('squad','transfer','training')),
    params                JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled               BOOLEAN NOT NULL DEFAULT TRUE,
    updated_by_actor_type TEXT CHECK (updated_by_actor_type IN ('manager','policy_bot')),
    updated_by_actor_id   UUID,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_policies_manager_type ON manager.policies(manager_id, policy_type);
CREATE INDEX idx_policies_world_manager ON manager.policies(world_id, manager_id) WHERE enabled;

-- One unemployed absence-PolicyBot manager per world (the delegated actor for
-- away managers). FiveAI per-club bots stay active + club-scoped; this row is
-- deliberately club-less so the uq_manager_one_active_club index never bites.
CREATE UNIQUE INDEX uq_managers_world_policy_bot
    ON manager.managers(world_id)
    WHERE is_policy_bot = TRUE AND current_club_id IS NULL;

-- Fixture-attendance anchor: club.club_lineups gains an updated_at so the
-- pre-match scan can tell "the manager set a lineup for this matchday" from
-- "left it to the automatic engine" (S06-05 missed-fixture counting).
ALTER TABLE club.club_lineups
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
