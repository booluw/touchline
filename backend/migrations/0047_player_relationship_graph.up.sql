-- =====================================================================
-- SCHEMA: social — player relationship graph traversal (S09-02)
--
--   Squad dynamics derive dressing-room hierarchy and factions from the
--   player↔player subgraph of social.relationships at read time. That read
--   is an undirected recursive CTE over endpoints filtered by world and
--   player entity types.
--
--   There is deliberately NO new table and NO denormalized faction column:
--   the graph is the single source of truth (PRD §15, technical plan §6).
--   These partial indexes keep the forward and reverse traversals cheap
--   without touching the existing club/manager relationship indexes.
-- =====================================================================

CREATE INDEX IF NOT EXISTS idx_relationships_player_edges
    ON social.relationships (world_id, entity_a_id)
    WHERE entity_a_type = 'player' AND entity_b_type = 'player';

CREATE INDEX IF NOT EXISTS idx_relationships_player_edges_rev
    ON social.relationships (world_id, entity_b_id)
    WHERE entity_a_type = 'player' AND entity_b_type = 'player';
