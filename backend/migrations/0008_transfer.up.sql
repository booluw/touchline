-- =====================================================================
-- SCHEMA: transfer
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS transfer;

CREATE TABLE transfer.listings (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id            UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    player_id           UUID NOT NULL REFERENCES player.players(id),
    listing_club_id     UUID NOT NULL REFERENCES club.clubs(id),
    asking_price        NUMERIC(14,2),
    listing_type        TEXT NOT NULL DEFAULT 'open_to_offers' CHECK (listing_type IN
                         ('open_to_offers', 'actively_shopped', 'loan_available')),
    status               TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'withdrawn', 'completed')),
    listed_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_listings_world_status ON transfer.listings(world_id, status);
CREATE INDEX idx_listings_player ON transfer.listings(player_id);

CREATE TABLE transfer.bids (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id              UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    listing_id            UUID REFERENCES transfer.listings(id),
    player_id             UUID NOT NULL REFERENCES player.players(id),
    bidding_club_id       UUID NOT NULL REFERENCES club.clubs(id),
    bidding_manager_id    UUID REFERENCES manager.managers(id),  -- null if AI club
    selling_club_id       UUID NOT NULL REFERENCES club.clubs(id),
    fee                   NUMERIC(14,2) NOT NULL,
    status                TEXT NOT NULL DEFAULT 'pending' CHECK (status IN
                           ('pending', 'accepted', 'rejected', 'countered', 'withdrawn', 'expired')),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    responded_at          TIMESTAMPTZ
);
CREATE INDEX idx_bids_player_status ON transfer.bids(player_id, status);
CREATE INDEX idx_bids_clubs ON transfer.bids(bidding_club_id, selling_club_id);

CREATE TABLE transfer.negotiations (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bid_id                UUID NOT NULL REFERENCES transfer.bids(id) ON DELETE CASCADE,
    round                 INT NOT NULL,
    proposed_by           TEXT NOT NULL CHECK (proposed_by IN ('buying_club', 'selling_club')),
    terms                 JSONB NOT NULL,   -- fee, installments, bonuses, clauses, wage, etc.
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_negotiations_bid ON transfer.negotiations(bid_id, round);

CREATE TABLE transfer.completed_transfers (
    id                                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id                           UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    bid_id                             UUID REFERENCES transfer.bids(id),
    player_id                          UUID NOT NULL REFERENCES player.players(id),
    from_club_id                       UUID REFERENCES club.clubs(id),        -- null if signing a free agent
    to_club_id                         UUID NOT NULL REFERENCES club.clubs(id),
    fee                                NUMERIC(14,2) NOT NULL DEFAULT 0,
    installments                       JSONB,
    is_record_transfer_for_buyer       BOOLEAN NOT NULL DEFAULT FALSE,
    is_record_transfer_for_seller      BOOLEAN NOT NULL DEFAULT FALSE,
    related_event_id                   UUID REFERENCES world.events(id),
    completed_at                       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_completed_transfers_player ON transfer.completed_transfers(player_id);
CREATE INDEX idx_completed_transfers_world ON transfer.completed_transfers(world_id, completed_at DESC);

CREATE TABLE transfer.clauses (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    completed_transfer_id    UUID NOT NULL REFERENCES transfer.completed_transfers(id) ON DELETE CASCADE,
    clause_type              TEXT NOT NULL CHECK (clause_type IN ('sell_on', 'buy_back', 'release')),
    percentage                NUMERIC,          -- for sell_on
    amount                     NUMERIC,         -- for buy_back / release
    beneficiary_club_id          UUID REFERENCES club.clubs(id),
    conditions                     JSONB,
    expires_at                       DATE,
    triggered_at                       TIMESTAMPTZ
);
CREATE INDEX idx_clauses_transfer ON transfer.clauses(completed_transfer_id);

CREATE TABLE transfer.loans (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id                 UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    player_id                UUID NOT NULL REFERENCES player.players(id),
    from_club_id              UUID NOT NULL REFERENCES club.clubs(id),
    to_club_id                  UUID NOT NULL REFERENCES club.clubs(id),
    start_date                    DATE NOT NULL,
    end_date                        DATE NOT NULL,
    wage_contribution_pct             NUMERIC CHECK (wage_contribution_pct BETWEEN 0 AND 100),
    loan_to_buy_amount                  NUMERIC,
    status                                 TEXT NOT NULL DEFAULT 'active' CHECK (status IN
                                           ('active', 'recalled', 'completed', 'converted_to_permanent'))
);
CREATE INDEX idx_loans_player ON transfer.loans(player_id, status);

