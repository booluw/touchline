DELETE FROM world.news_stories WHERE category = 'fan_reaction';
ALTER TABLE world.news_stories DROP CONSTRAINT IF EXISTS news_stories_category_check;
ALTER TABLE world.news_stories
    ADD CONSTRAINT news_stories_category_check CHECK (category IN
        ('transfer', 'sacking', 'dispute', 'wonderkid', 'finance',
         'tactics', 'rivalry', 'controversy', 'general', 'scheduling',
         'announcement'));
DROP TABLE IF EXISTS manager.match_ratings;
