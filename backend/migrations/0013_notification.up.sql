-- =====================================================================
-- SCHEMA: notification
-- Email-first, per-category, user-configurable (resolved open question #4).
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS notification;

CREATE TABLE notification.preferences (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id    UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE,
    category      TEXT NOT NULL CHECK (category IN
                   ('transfer_bid', 'club_finance', 'match_report', 'board_warning',
                    'player_request', 'contract_expiry', 'injury', 'news_digest')),
    channel       TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email', 'web_push')),
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    frequency     TEXT NOT NULL DEFAULT 'instant' CHECK (frequency IN
                   ('instant', 'daily_digest', 'weekly_digest')),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (manager_id, category, channel)
);

CREATE TABLE notification.dispatch_log (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id          UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE,
    category            TEXT NOT NULL,
    channel             TEXT NOT NULL,
    related_event_id    UUID REFERENCES world.events(id),
    status              TEXT NOT NULL CHECK (status IN ('sent', 'failed', 'skipped_by_preference')),
    sent_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_dispatch_log_manager ON notification.dispatch_log(manager_id, sent_at DESC);

