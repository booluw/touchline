-- 0058: a club proposes to at most one manager at a time.
-- Onboarding used to pick the same first AI club for every registrant, so one
-- club held pending offers to many managers and the second acceptance clashed
-- with the first. Existing duplicates were cleared manually before this ran.
CREATE UNIQUE INDEX uq_job_offer_pending_club
    ON manager.job_offers(club_id) WHERE status = 'proposed';
