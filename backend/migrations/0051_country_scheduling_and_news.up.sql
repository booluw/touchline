-- =====================================================================
-- 0051: country scheduling + country-scoped scheduling news (IM05)
--
-- IM05 weekday-aware fixture scheduling:
--   * world.countries.default_scheduling_rules is the optional country-wide
--     fixture-calendar default (a JSONB document carrying the same keys as
--     competition_rules.scheduling_rules — IM05 uses "allowed_weekdays", the
--     ISO-8601 weekday numbers 1..7 a competition may play on). A league/cup
--     falls back to its country's default when its own scheduling_rules
--     declare no weekday set; when neither is set the legacy day-formula
--     pacing (IM03) applies unchanged.
--   * world.news_stories.country_id lets scheduling news land only in the
--     country's feeds (world-wide stories keep NULL). NULL stories appear in
--     every feed, so existing news is unaffected.
-- =====================================================================
ALTER TABLE world.countries
    ADD COLUMN default_scheduling_rules JSONB;

ALTER TABLE world.news_stories
    ADD COLUMN country_id UUID REFERENCES world.countries(id) ON DELETE CASCADE;

CREATE INDEX idx_news_country_published
    ON world.news_stories(world_id, country_id, published_at DESC);

-- Allow the 'scheduling' category the IM05 news publisher writes.
ALTER TABLE world.news_stories DROP CONSTRAINT IF EXISTS news_stories_category_check;
ALTER TABLE world.news_stories
    ADD CONSTRAINT news_stories_category_check CHECK (category IN
        ('transfer', 'sacking', 'dispute', 'wonderkid', 'finance',
         'tactics', 'rivalry', 'controversy', 'general', 'scheduling'));