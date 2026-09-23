-- Reverse IM05: drop the country-scheduling column, the news country linkage,
-- and restore the pre-IM05 news category check (no 'scheduling').
DROP INDEX IF EXISTS idx_news_country_published;

ALTER TABLE world.news_stories DROP CONSTRAINT IF EXISTS news_stories_category_check;
ALTER TABLE world.news_stories
    ADD CONSTRAINT news_stories_category_check CHECK (category IN
        ('transfer', 'sacking', 'dispute', 'wonderkid', 'finance',
         'tactics', 'rivalry', 'controversy', 'general'));

ALTER TABLE world.news_stories DROP COLUMN IF EXISTS country_id;

ALTER TABLE world.countries DROP COLUMN IF EXISTS default_scheduling_rules;