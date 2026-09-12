DROP INDEX IF EXISTS manager.uq_manager_one_active_person;
ALTER TABLE manager.manager_reputation_events
    DROP CONSTRAINT IF EXISTS manager_reputation_events_category_check,
    ADD CONSTRAINT manager_reputation_events_category_check
    CHECK (category IN ('trophies', 'promotions', 'player_development', 'finances',
                        'tactical_innovation', 'academy_success', 'media_presence'));
DROP INDEX IF EXISTS world.uq_worlds_name;
ALTER TABLE manager.managers DROP CONSTRAINT IF EXISTS chk_managers_club_only_when_active;
DROP INDEX IF EXISTS manager.uq_job_offer_pending_club_manager;
DROP INDEX IF EXISTS manager.idx_job_offers_candidate;
DROP TABLE IF EXISTS manager.job_offers;