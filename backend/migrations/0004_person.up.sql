-- =====================================================================
-- SCHEMA: person
-- A shared identity for any human-like entity that can hold one or more
-- roles over time (player, manager, coach, scout, agent, pundit — plan
-- section 16 / V2 "retired player careers"). Building this now avoids a
-- painful migration when retirement-into-new-roles ships in V2.
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS person;

CREATE TABLE person.people (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id            UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    first_name          TEXT NOT NULL,
    last_name           TEXT,                      -- nullable: some cultures/generation rules use single names
    display_name        TEXT NOT NULL,              -- shirt name / commonly used name
    date_of_birth       DATE NOT NULL,
    nationality_code    TEXT NOT NULL REFERENCES ref.nationalities(code),
    second_nationality_code TEXT REFERENCES ref.nationalities(code),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_people_world ON person.people(world_id);
CREATE INDEX idx_people_nationality ON person.people(nationality_code);

