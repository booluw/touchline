-- =====================================================================
-- SCHEMA: world
-- World lifecycle, the event log (the backbone of section 63/68), tick
-- configuration, and news generated as a read-model over events.
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS world;

CREATE TABLE world.worlds (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('provisioning', 'open_beta', 'active', 'paused', 'archived')),
    current_tick        BIGINT NOT NULL DEFAULT 0,
    current_season      INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    launched_at         TIMESTAMPTZ
);

-- Per-world runtime configuration: tick cadences, feature flags, etc.
-- Keeps cadence configurable at runtime (plan section 5) without redeploys.
CREATE TABLE world.world_config (
    world_id            UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    config_key          TEXT NOT NULL,             -- e.g. 'tick.match_minutes', 'tick.daily_hour'
    config_value        JSONB NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (world_id, config_key)
);

-- The event log. Every meaningful state change in the simulation is
-- appended here (plan section 4). This table is the source for history,
-- news, audit, replay, and anti-abuse read-models.
CREATE TABLE world.events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id            UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    world_tick          BIGINT NOT NULL,
    event_type          TEXT NOT NULL,             -- e.g. 'PLAYER_SOLD', 'MANAGER_SACKED'
    actor_type          TEXT CHECK (actor_type IN ('manager', 'policy_bot', 'board', 'system', 'ai_club')),
    actor_id            UUID,
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    explanation         JSONB,                     -- structured reasoning breakdown (plan section 8)
    caused_by_event_id  UUID REFERENCES world.events(id),
    random_seed         BIGINT,                    -- set when this event involved RNG (determinism, plan section 8/68)
    occurred_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_world_tick ON world.events(world_id, world_tick);
CREATE INDEX idx_events_type ON world.events(world_id, event_type);
CREATE INDEX idx_events_actor ON world.events(actor_type, actor_id);
CREATE INDEX idx_events_caused_by ON world.events(caused_by_event_id);

CREATE TABLE world.news_stories (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id            UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    headline            TEXT NOT NULL,
    body                TEXT NOT NULL,
    category            TEXT NOT NULL CHECK (category IN
                          ('transfer', 'sacking', 'dispute', 'wonderkid', 'finance', 'tactics', 'rivalry', 'controversy', 'general')),
    related_event_id    UUID REFERENCES world.events(id),
    published_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_news_world_published ON world.news_stories(world_id, published_at DESC);

