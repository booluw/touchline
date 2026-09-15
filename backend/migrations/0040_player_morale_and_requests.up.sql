-- =====================================================================
-- SCHEMA: player — morale, appearances, transfer requests (S06-03)
-- =====================================================================

-- Per-player morale on the existing condition table (the home of per-player
-- match-day state, migration 0034): [0,1] with 0.5 neutral. playing_time_pct
-- is the player's whole-season share of the club's minutes (0..1).
-- transfer_request_cooldown_until silences new requests after a denial or a
-- fresh reassure until it passes.
ALTER TABLE player.player_condition
    ADD COLUMN morale                      NUMERIC(5,4) NOT NULL DEFAULT 0.5 CHECK (morale BETWEEN 0 AND 1),
    ADD COLUMN playing_time_pct            NUMERIC(5,4) NOT NULL DEFAULT 0   CHECK (playing_time_pct BETWEEN 0 AND 1),
    ADD COLUMN transfer_request_cooldown_until TIMESTAMPTZ;

-- Agreed squad role on the active contract (S06-03). Nullable so existing
-- contracts can be backfilled; new contracts get it resolved lazily by the
-- weekly player pass from player_preferences.playing_time_expectation.
ALTER TABLE player.contracts
    ADD COLUMN squad_role TEXT CHECK (squad_role IN
        ('key_player', 'rotation', 'squad_player', 'development'));

-- Backfill from the free-text playing-time promise recorded at contract
-- signing (migration 0006). Best-effort keyword mapping; anything unparseable
-- falls to the neutral 'squad_player'.
UPDATE player.contracts
SET squad_role = CASE
    WHEN playing_time_promise ILIKE '%key player%' OR playing_time_promise ILIKE '%first team%'
      OR playing_time_promise ILIKE '%star%' OR playing_time_promise ILIKE '%regular starter%'
    THEN 'key_player'
    WHEN playing_time_promise ILIKE '%rotation%' OR playing_time_promise ILIKE '%impact sub%'
      OR playing_time_promise ILIKE '%regular%'
    THEN 'rotation'
    WHEN playing_time_promise ILIKE '%development%' OR playing_time_promise ILIKE '%youth%'
      OR playing_time_promise ILIKE '%prospect%'
    THEN 'development'
    ELSE 'squad_player'
END
WHERE squad_role IS NULL AND playing_time_promise IS NOT NULL AND playing_time_promise <> '';

-- One appearance row per player per completed match: started (in the starting
-- XI), minutes on the pitch (90 minus sub-off minute for starters, 90 minus
-- sub-on minute for bench players; 0..90).
CREATE TABLE player.player_appearances (
    player_id  UUID     NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    match_id   UUID     NOT NULL REFERENCES match.matches(id)  ON DELETE CASCADE,
    started    BOOLEAN  NOT NULL,
    minutes    SMALLINT NOT NULL CHECK (minutes BETWEEN 0 AND 90),
    PRIMARY KEY (player_id, match_id)
);
CREATE INDEX idx_player_appearances_player ON player.player_appearances(player_id);

-- Formal player transfer requests (S06-03). One open 'pending' request per
-- player is enforced by the partial unique index; approve/auto_list close it
-- by creating a transfer listing, deny/reassure pause further requests via
-- transfer_request_cooldown_until on player_condition.
CREATE TABLE player.player_transfer_requests (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id      UUID NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    club_id        UUID NOT NULL REFERENCES club.clubs(id),
    manager_id     UUID NOT NULL REFERENCES manager.managers(id),
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN
                   ('pending', 'approved', 'denied', 'reassured', 'auto_listed', 'withdrawn')),
    reason         TEXT NOT NULL DEFAULT 'playing_time' CHECK (reason IN
                   ('playing_time', 'wage', 'ambition', 'homesickness')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at    TIMESTAMPTZ,
    reassured_until TIMESTAMPTZ,
    related_event_id UUID REFERENCES world.events(id)
);
CREATE UNIQUE INDEX uq_player_transfer_request_open
    ON player.player_transfer_requests(player_id)
    WHERE status = 'pending';
CREATE INDEX idx_player_transfer_requests_status
    ON player.player_transfer_requests(status, created_at);

-- =====================================================================
-- SCHEMA: social — player↔manager relationship journal (S06-03)
-- =====================================================================
-- Append-only memory that survives club changes: a denied transfer, an
-- approved one, a reassure, a kept/broken playing-time promise. This is the
-- durable "history" surfaced by S06-04 profiles; the aggregated signed
-- sentiment lives on social.relationships (upserted from these deltas). It is
-- deliberately NOT coupled to the playing-time mechanic.
CREATE TABLE social.relationship_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id        UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    player_id       UUID NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    manager_id      UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL CHECK (event_type IN
                    ('transfer_approved', 'transfer_denied', 'reassured',
                     'playing_time_promise_kept', 'playing_time_promise_broken')),
    sentiment_delta INT NOT NULL CHECK (sentiment_delta BETWEEN -100 AND 100),
    related_event_id UUID REFERENCES world.events(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_relationship_events_pair
    ON social.relationship_events(player_id, manager_id, created_at DESC);

-- One canonical player↔manager sentiment memory: the signed row is
-- professional_respect when sentiment ≥ 0 and dislike when < 0 (flipped by
-- upsert), so approve/deny/reassure accumulate on a single relationship.
CREATE UNIQUE INDEX uq_relationship_player_manager
    ON social.relationships(entity_a_id, entity_b_id)
    WHERE entity_a_type = 'player' AND entity_b_type = 'manager';