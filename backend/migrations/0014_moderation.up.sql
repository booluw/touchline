-- =====================================================================
-- SCHEMA: moderation
-- Anti-abuse / competitive-integrity support (plan section 13).
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS moderation;

CREATE TABLE moderation.flags (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id                UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    flag_type               TEXT NOT NULL CHECK (flag_type IN
                             ('suspicious_transfer', 'collusion', 'market_manipulation',
                              'multi_account', 'bot_behavior', 'other')),
    related_entity_id       UUID NOT NULL,
    related_entity_type     TEXT NOT NULL CHECK (related_entity_type IN
                             ('transfer', 'manager', 'club', 'competition')),
    severity                INT NOT NULL DEFAULT 1 CHECK (severity BETWEEN 1 AND 5),
    status                  TEXT NOT NULL DEFAULT 'open' CHECK (status IN
                             ('open', 'reviewing', 'resolved', 'dismissed')),
    details                 JSONB,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at             TIMESTAMPTZ,
    resolved_by_user_id     UUID REFERENCES auth.users(id)
);
CREATE INDEX idx_flags_world_status ON moderation.flags(world_id, status);

CREATE TABLE moderation.device_fingerprints (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    fingerprint_hash     TEXT NOT NULL,
    ip_address           INET,
    first_seen_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_fingerprints_hash ON moderation.device_fingerprints(fingerprint_hash);
CREATE INDEX idx_fingerprints_user ON moderation.device_fingerprints(user_id);

