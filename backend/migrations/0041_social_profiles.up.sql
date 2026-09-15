-- =====================================================================
-- SCHEMA: social — profile reads + trust seeding (S06-04a)
-- =====================================================================

-- Rate-limit counting for the direct messaging surface (S06-04b): the inbox
-- index created in 0010 is (recipient_id, sent_at DESC); sending needs the
-- sender's recent-sends view, so add the paired index now.
CREATE INDEX IF NOT EXISTS idx_messages_sender
    ON social.messages(sender_id, sent_at DESC);

-- Seed trust from the S06-03 player↔manager journal so every manager has a
-- baseline trust score the day profiles launch. The journal already carries the
-- signed sentiment delta; trust mirrors it (approvals positive, denials and
-- broken promises negative). Rows written by the app afterwards carry a
-- non-prefixed reason, so the down migration can distinguish seeding rows from
-- live trust events and delete only the backfill.
INSERT INTO social.trust_events (manager_id, delta, reason, related_event_id, occurred_at)
SELECT re.manager_id,
       re.sentiment_delta,
       'backfilled:' || CASE re.event_type
           WHEN 'transfer_approved'           THEN 'transfer approved'
           WHEN 'transfer_denied'             THEN 'transfer denied'
           WHEN 'reassured'                   THEN 'reassured'
           WHEN 'playing_time_promise_kept'   THEN 'playing-time promise kept'
           WHEN 'playing_time_promise_broken' THEN 'playing-time promise broken'
       END,
       re.related_event_id,
       re.created_at
FROM social.relationship_events re
WHERE re.sentiment_delta <> 0;