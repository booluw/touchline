-- =====================================================================
-- SCHEMA: player — weekly attribute-change deltas (S08-01)
-- =====================================================================

-- Append-only weekly record of how each player's attributes moved, written by
-- the weekly training pass (docs/design/tactics-training-numerics.md §2). The
-- latest week per player+key is the "since last week" delta read models show;
-- cumulative history supports future trend charts.
--
-- attribute_key carries the stable player.player_attributes key plus one
-- pseudo-key, 'morale', whose delta is the weekly morale swing (S06-05) so the
-- read model's delta block can surface motivation as a single signed number
-- alongside the six attribute categories.
CREATE TABLE player.player_attribute_changes (
    player_id          UUID   NOT NULL REFERENCES player.players(id) ON DELETE CASCADE,
    applied_week       INTEGER NOT NULL,
    attribute_key      TEXT   NOT NULL,
    delta              INTEGER NOT NULL, -- signed; total movement across the week
    recorded_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (player_id, applied_week, attribute_key)
);

CREATE INDEX idx_player_attribute_changes_player_applied
    ON player.player_attribute_changes (player_id, applied_week);