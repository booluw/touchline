-- =====================================================================
-- SCHEMA: finance
-- Ledger-based, never a bare mutable balance column (plan section 36/67).
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS finance;

CREATE TABLE finance.accounts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id      UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    club_id       UUID NOT NULL UNIQUE REFERENCES club.clubs(id) ON DELETE CASCADE,
    currency      TEXT NOT NULL DEFAULT 'USD'
);

-- Append-only. `cash balance` = SUM(amount) WHERE account_id = X.
-- Never UPDATE a running total here.
CREATE TABLE finance.ledger_entries (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id          UUID NOT NULL REFERENCES finance.accounts(id) ON DELETE CASCADE,
    entry_type          TEXT NOT NULL CHECK (entry_type IN ('credit', 'debit')),
    category            TEXT NOT NULL CHECK (category IN
                         ('ticket_sales', 'season_tickets', 'sponsorship', 'broadcasting', 'prize_money',
                          'merchandise', 'player_sale', 'loan_income', 'commercial_partnership',
                          'wages', 'transfer_fee', 'staff_cost', 'facilities', 'academy',
                          'stadium', 'travel', 'medical', 'scouting', 'debt_interest', 'bonus', 'other')),
    amount              NUMERIC(14,2) NOT NULL CHECK (amount > 0), -- sign is carried by entry_type, not the number
    description         TEXT,
    related_event_id    UUID REFERENCES world.events(id),
    occurred_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ledger_account_occurred ON finance.ledger_entries(account_id, occurred_at DESC);
CREATE INDEX idx_ledger_category ON finance.ledger_entries(account_id, category);

-- Budgets are planned capacity, explicitly separate from cash (plan section 36).
CREATE TABLE finance.budgets (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id             UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    season              INT NOT NULL,
    budget_type         TEXT NOT NULL CHECK (budget_type IN ('transfer', 'wage')),
    allocated_amount    NUMERIC(14,2) NOT NULL,
    committed_amount    NUMERIC(14,2) NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_budgets_club_season_type ON finance.budgets(club_id, season, budget_type);

CREATE TABLE finance.wage_commitments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_id    UUID NOT NULL REFERENCES player.contracts(id) ON DELETE CASCADE,
    club_id        UUID NOT NULL REFERENCES club.clubs(id),
    weekly_wage    NUMERIC(14,2) NOT NULL,
    start_date     DATE NOT NULL,
    end_date       DATE NOT NULL
);
CREATE INDEX idx_wage_commitments_club ON finance.wage_commitments(club_id, end_date);

CREATE TABLE finance.financial_crisis_states (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id       UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    stage         TEXT NOT NULL CHECK (stage IN
                   ('warning', 'restriction', 'emergency', 'administration_risk',
                    'ownership_intervention', 'bankruptcy')),
    details       JSONB,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at   TIMESTAMPTZ
);
CREATE INDEX idx_crisis_club_active ON finance.financial_crisis_states(club_id) WHERE resolved_at IS NULL;

