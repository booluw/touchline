-- ============================================================================
-- 0057_match_ratings — per-match board rating + supporter reaction (IM33)
--
-- One row per (fixture, club): the board's 0-100 rating of the manager for that
-- match and the supporter sentiment before/after it. The monthly confidence
-- review reads the recent rows as its Performance factor. Also admits the
-- 'fan_reaction' news category.
--
-- fixture_id carries no FK: rows are cleaned up through world_id, and the
-- (fixture_id, club_id) key is what makes the match hook idempotent.
-- ============================================================================
CREATE TABLE manager.match_ratings (
    fixture_id       UUID NOT NULL,
    club_id          UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    manager_id       UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE,
    world_id         UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    board_rating     INT NOT NULL CHECK (board_rating BETWEEN 0 AND 100),
    sentiment_before INT NOT NULL,
    sentiment_after  INT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (fixture_id, club_id)
);
CREATE INDEX match_ratings_manager_club_idx
    ON manager.match_ratings (manager_id, club_id, created_at DESC);

ALTER TABLE world.news_stories DROP CONSTRAINT IF EXISTS news_stories_category_check;
ALTER TABLE world.news_stories
    ADD CONSTRAINT news_stories_category_check CHECK (category IN
        ('transfer', 'sacking', 'dispute', 'wonderkid', 'finance',
         'tactics', 'rivalry', 'controversy', 'general', 'scheduling',
         'announcement', 'fan_reaction'));
