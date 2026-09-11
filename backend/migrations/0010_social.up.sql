-- =====================================================================
-- SCHEMA: social
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS social;

CREATE TABLE social.relationships (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id               UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    entity_a_id            UUID NOT NULL,
    entity_a_type          TEXT NOT NULL CHECK (entity_a_type IN ('player', 'manager', 'club')),
    entity_b_id            UUID NOT NULL,
    entity_b_type          TEXT NOT NULL CHECK (entity_b_type IN ('player', 'manager', 'club')),
    relationship_type      TEXT NOT NULL CHECK (relationship_type IN
                            ('friendship', 'rivalry', 'mentorship', 'professional_respect',
                             'dislike', 'family', 'national_team', 'academy', 'former_teammate',
                             'former_manager', 'agent')),
    strength                INT NOT NULL CHECK (strength BETWEEN -100 AND 100),
    trust                    INT NOT NULL DEFAULT 0 CHECK (trust BETWEEN -100 AND 100),
    sentiment                  INT NOT NULL DEFAULT 0 CHECK (sentiment BETWEEN -100 AND 100),
    last_interaction_at          TIMESTAMPTZ,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entity_a_id, entity_b_id, relationship_type)
);
CREATE INDEX idx_relationships_a ON social.relationships(entity_a_id, entity_a_type);
CREATE INDEX idx_relationships_b ON social.relationships(entity_b_id, entity_b_type);
CREATE INDEX idx_relationships_world_type ON social.relationships(world_id, relationship_type);

CREATE TABLE social.messages (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id         UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    sender_id        UUID NOT NULL,
    sender_type      TEXT NOT NULL CHECK (sender_type IN ('manager', 'system')),
    recipient_id     UUID NOT NULL,
    recipient_type   TEXT NOT NULL CHECK (recipient_type IN ('manager', 'system')),
    subject          TEXT,
    body             TEXT NOT NULL,
    sent_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at          TIMESTAMPTZ
);
CREATE INDEX idx_messages_recipient ON social.messages(recipient_id, sent_at DESC);

CREATE TABLE social.promises (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id        UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    player_id       UUID NOT NULL REFERENCES player.players(id),
    manager_id      UUID NOT NULL REFERENCES manager.managers(id),
    promise_type    TEXT NOT NULL CHECK (promise_type IN
                     ('increase_playing_time', 'sign_better_players', 'improve_facilities',
                      'allow_transfer', 'preferred_position', 'loan_player', 'promote_academy_player',
                      'improve_salary', 'challenge_for_trophy')),
    explicitness    TEXT NOT NULL DEFAULT 'explicit' CHECK (explicitness IN ('explicit', 'implied')),
    deadline        DATE,
    confidence      INT NOT NULL DEFAULT 50 CHECK (confidence BETWEEN 0 AND 100),
    importance      INT NOT NULL DEFAULT 50 CHECK (importance BETWEEN 0 AND 100),
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN
                     ('pending', 'fulfilled', 'broken', 'adapted')),
    context         JSONB,           -- e.g. adaptation reason (player got injured, etc.)
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ
);
CREATE INDEX idx_promises_player ON social.promises(player_id, status);
CREATE INDEX idx_promises_manager ON social.promises(manager_id, status);

-- Append-only, mirrors the reputation-log pattern used for managers.
CREATE TABLE social.trust_events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id          UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE,
    delta               INT NOT NULL,
    reason               TEXT NOT NULL,
    related_event_id       UUID REFERENCES world.events(id),
    occurred_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_trust_events_manager ON social.trust_events(manager_id);

