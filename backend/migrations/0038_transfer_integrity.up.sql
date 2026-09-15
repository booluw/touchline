-- =====================================================================
-- S06-01: transfer market integrity constraints.
-- The transfer schema ships unused since 0008; the market engine now
-- relies on these guards (documented in docs/design/transfer-numerics.md).
-- =====================================================================

-- A player can only be listed once while a listing is live.
CREATE UNIQUE INDEX idx_listings_active_player
    ON transfer.listings(player_id)
    WHERE status = 'active';

-- The expiry sweep walks open bids in status order; partial index keeps it
-- small while the market holds many resolved rows.
CREATE INDEX idx_bids_open
    ON transfer.bids(status, created_at)
    WHERE status IN ('pending', 'countered');

-- A bid thread completes a transfer at most once. The ID is nullable only
-- for the future free-agent signing path (from_club_id IS NULL), which is
-- not exercised in S06-01.
CREATE UNIQUE INDEX idx_completed_bid
    ON transfer.completed_transfers(bid_id);