-- 0063: crowd attendance per match (IM66). Set at kickoff from the home
-- club's stadium capacity and supporter state. NULL = played before IM66;
-- never backfilled (the pre-match supporter state is gone).
ALTER TABLE match.matches
    ADD COLUMN attendance INT CHECK (attendance >= 0);
