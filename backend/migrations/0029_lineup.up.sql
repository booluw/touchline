-- =====================================================================
-- SCHEMA: club — managers' preferred starting XIs
-- =====================================================================
-- Per-club preferred lineups (club.club_lineups): the manager's set-and-forget
-- starting XI for user-managed clubs (is_ai_controlled = FALSE). Slots follow
-- the shared squad.DefaultFormation slot order (0 = GK ... 10 = left wing), so
-- a filled table always maps to a legal 11. AI-managed clubs do not write this
-- table — their XIs are drawn seeded per fixture (internal/squad/lineup.go).
-- Missing or unavailable slots fall back to the deterministic selector at
-- matchday.

CREATE TABLE club.club_lineups (
    slot       INT  NOT NULL CHECK (slot BETWEEN 0 AND 10),
    club_id    UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    player_id  UUID NOT NULL REFERENCES player.players(id),
    PRIMARY KEY (club_id, slot),
    UNIQUE (club_id, player_id)   -- a player can only own one preferred slot
);
CREATE INDEX idx_club_lineups_player ON club.club_lineups(player_id);