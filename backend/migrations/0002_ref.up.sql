-- =====================================================================
-- SCHEMA: ref
-- Static, non-world-scoped reference data — the same across every world.
-- Powers procedural player generation (implementation plan section 7).
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS ref;

CREATE TABLE ref.nationalities (
    code                TEXT PRIMARY KEY,         -- ISO 3166-1 alpha-2, e.g. 'NG', 'BR'
    name                TEXT NOT NULL,
    generation_weight   NUMERIC NOT NULL DEFAULT 1.0, -- relative weight in the global player pool
    attribute_bias      JSONB NOT NULL DEFAULT '{}'::jsonb -- optional light bias, e.g. {"physical": 1.05}
);

CREATE TABLE ref.name_pool (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nationality_code    TEXT NOT NULL REFERENCES ref.nationalities(code),
    name_type           TEXT NOT NULL CHECK (name_type IN ('first', 'last', 'single')),
    name                TEXT NOT NULL,
    frequency_weight    NUMERIC NOT NULL DEFAULT 1.0
);
CREATE INDEX idx_name_pool_nat_type ON ref.name_pool(nationality_code, name_type);

