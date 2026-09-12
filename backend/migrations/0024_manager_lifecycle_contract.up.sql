-- =====================================================================
-- 0024: manager-club assignment contract + job offers (S02-02)
--
-- Establishes the offer-driven assignment boundary approved in OPD-16:
--   * world lifecycle is admin-managed (see internal/world lifecycle ops);
--   * a manager gets their first club ONLY via a job offer from an
--     AI-managed club (manager.job_offers), accepted by the manager;
--   * one active club assignment per manager, platform-wide.
-- =====================================================================

-- Job offers from AI clubs. status lifecycle: proposed -> accepted | declined | expired.
CREATE TABLE manager.job_offers (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id                UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    club_id                 UUID NOT NULL REFERENCES club.clubs(id),
    manager_id              UUID NOT NULL REFERENCES manager.managers(id), -- the candidate
    offered_by_manager_id   UUID REFERENCES manager.managers(id),           -- club's AI/policy-bot manager
    status                  TEXT NOT NULL DEFAULT 'proposed' CHECK (status IN
                              ('proposed', 'accepted', 'declined', 'expired')),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    responded_at            TIMESTAMPTZ
);
CREATE INDEX idx_job_offers_candidate ON manager.job_offers(manager_id, status);
-- One pending offer per (club, candidate): a club cannot spam offers.
CREATE UNIQUE INDEX uq_job_offer_pending_club_manager
    ON manager.job_offers(club_id, manager_id) WHERE status = 'proposed';

-- A manager only holds a club while employed: unemployed/sabbatical/retired
-- rows must have no current_club_id, so resignations never leave dangling clubs.
ALTER TABLE manager.managers
    ADD CONSTRAINT chk_managers_club_only_when_active
    CHECK (status = 'active' OR current_club_id IS NULL);

-- The one-active-assignment invariant extends to AI managers that carry a
-- person record (a real identity that can recur across worlds). Pure
-- policy-bot rows (is_policy_bot, no person) are per-world entities and stay
-- covered by uq_manager_one_active_club within each world.
CREATE UNIQUE INDEX uq_manager_one_active_person
    ON manager.managers(person_id)
    WHERE status = 'active' AND current_club_id IS NOT NULL AND person_id IS NOT NULL;

-- World names are unique (case-insensitively), surfaced by the world service
-- as ErrNameCollision when an admin creates a duplicate.
CREATE UNIQUE INDEX uq_worlds_name ON world.worlds (lower(name));

-- Assignment-border reputation deltas (S02-02) belong to a 'career' category,
-- so the append-only reputation log can express signs of employment events.
ALTER TABLE manager.manager_reputation_events
    DROP CONSTRAINT manager_reputation_events_category_check,
    ADD CONSTRAINT manager_reputation_events_category_check
    CHECK (category IN ('trophies', 'promotions', 'player_development', 'finances',
                        'tactical_innovation', 'academy_success', 'media_presence', 'career'));