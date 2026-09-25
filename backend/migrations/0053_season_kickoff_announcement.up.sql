-- =====================================================================
-- 0053: season kickoff press releases + squad-graph fingerprint (IM next)
--
-- * world.news_stories gains the 'announcement' category: the country-scoped
--   press releases a league season start publishes (fixture list + official
--   kickoff day). Both stories share the SEASON_CREATED event as their
--   related_event_id, and both are written transactionally with the season
--   they describe.
-- * social.squad_graph_state fingerprints the player↔player edges a club's
--   squad generates (team-dynamics workstream). GetDynamics skips rewriting
--   the graph when the fingerprint is unchanged, turning the ~thousands of
--   per-edge INSERT round-trips a steady-state read used to pay into two
--   lookups (fingerprint + tiny hash compare).
-- =====================================================================
ALTER TABLE world.news_stories DROP CONSTRAINT IF EXISTS news_stories_category_check;
ALTER TABLE world.news_stories
    ADD CONSTRAINT news_stories_category_check CHECK (category IN
        ('transfer', 'sacking', 'dispute', 'wonderkid', 'finance',
         'tactics', 'rivalry', 'controversy', 'general', 'scheduling',
         'announcement'));

CREATE TABLE social.squad_graph_state (
    world_id    UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    club_id     UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    member_hash BIGINT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (world_id, club_id)
);