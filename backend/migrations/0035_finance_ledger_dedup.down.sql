DROP INDEX IF EXISTS finance.idx_ledger_account_dedup;
ALTER TABLE finance.ledger_entries DROP COLUMN IF EXISTS dedup_key;