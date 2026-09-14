-- =====================================================================
-- SCHEMA: finance — idempotency key on the append-only ledger (S05-02)
-- =====================================================================

-- River redelivers world.events at-least-once, so a tick-scoped posting (e.g.
-- the monthly wage run) must be idempotent at the ledger row itself rather than
-- trusted to the event handler. dedup_key carries the posting's natural key
-- (e.g. 'wage:<world_tick>') and the UNIQUE index turns a redelivered posting
-- into a no-op via ON CONFLICT DO NOTHING. Non-deduped rows carry a NULL
-- dedup_key, and NULLs never conflict in a unique index, so the free-form
-- ledger still allows many rows per account. Adding a constraint — never a
-- derived/balance column — keeps the append-only invariant intact.
ALTER TABLE finance.ledger_entries ADD COLUMN dedup_key TEXT;

CREATE UNIQUE INDEX idx_ledger_account_dedup
    ON finance.ledger_entries(account_id, dedup_key);