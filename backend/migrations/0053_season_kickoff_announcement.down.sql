-- =====================================================================
-- 0053 down: revert the announcement category and the squad-graph fingerprint.
-- =====================================================================
ALTER TABLE world.news_stories DROP CONSTRAINT IF EXISTS news_stories_category_check;
ALTER TABLE world.news_stories
    ADD CONSTRAINT news_stories_category_check CHECK (category IN
        ('transfer', 'sacking', 'dispute', 'wonderkid', 'finance',
         'tactics', 'rivalry', 'controversy', 'general', 'scheduling'));

DROP TABLE IF EXISTS social.squad_graph_state;