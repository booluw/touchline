-- =====================================================================
-- SCHEMA: club — manager tactical setups and weekly training plans (S05-01)
-- =====================================================================

-- Per-club Simple-Mode tactical setup (club.club_tactics). The control is one
-- of the five product-approved styles (S05-01 / OPD-25); formation is an
-- optional refinement within the style's allowed set and is service-validated
-- (the allowed set depends on the style, so no DB CHECK on formation). AI
-- clubs may write a row too (a PolicyBot actor runs the same command layer),
-- but otherwise play styles.with balanced defaults. Changes to a club's tactics
-- only affect fixtures kicked off afterwards — kickoff freezes the row into the
-- match snapshot (sim_inputs).
CREATE TABLE club.club_tactics (
    club_id                UUID PRIMARY KEY REFERENCES club.clubs(id) ON DELETE CASCADE,
    style                  TEXT NOT NULL CHECK (style IN
                           ('balanced', 'possession', 'gegenpress', 'low_block', 'direct')),
    formation              TEXT NOT NULL DEFAULT '4-3-3',
    updated_by_actor_type  TEXT CHECK (updated_by_actor_type IN ('manager', 'policy_bot')),
    updated_by_actor_id    UUID,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-club weekly Simple-Mode training plan (club.club_training_plans). One
-- active archetype per club. effective_from_tick is the world's current_tick
-- at submission (audit anchor); the weekly WORLD_TICK handler applies the plan
-- on the first weekly emission after submission and records last_applied_week
-- (the weekly event's world_tick) in the same transaction, so at-least-once
-- river redelivery can never double-apply a week. Replacing the plan resets
-- its week stamp, so a swap mid-week applies from the next weekly tick.
CREATE TABLE club.club_training_plans (
    club_id                UUID PRIMARY KEY REFERENCES club.clubs(id) ON DELETE CASCADE,
    archetype              TEXT NOT NULL CHECK (archetype IN
                           ('technical', 'physical', 'defensive', 'attacking', 'recovery')),
    effective_from_tick    BIGINT NOT NULL,
    last_applied_week      BIGINT,
    updated_by_actor_type  TEXT CHECK (updated_by_actor_type IN ('manager', 'policy_bot')),
    updated_by_actor_id    UUID,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The S05-01 training matrix targets granular attribute keys. Three keys used
-- by the matrix are not in the pkg/playergen generation catalogue:
-- 'tackling' (technical), 'marking' (positional), 'teamwork' (mental). Backfill
-- them for pre-existing players at each player's category mean so today's
-- ratings are unchanged (the player_attributes snapshot IS the category mean).
-- New squads get the keys from the generation catalogue going forward.
INSERT INTO player.player_attributes (player_id, attribute_category, attribute_key, value)
SELECT player_id, 'technical', 'tackling', technical_mean FROM (
    SELECT player_id,
           round(avg(value) FILTER (WHERE attribute_category = 'technical')::numeric)::int AS technical_mean,
           round(avg(value) FILTER (WHERE attribute_category = 'positional')::numeric)::int AS positional_mean,
           round(avg(value) FILTER (WHERE attribute_category = 'mental')::numeric)::int AS mental_mean
    FROM player.player_attributes GROUP BY player_id
) m
UNION ALL
SELECT player_id, 'positional', 'marking', positional_mean FROM (
    SELECT player_id,
           round(avg(value)::numeric)::int AS technical_mean,
           round(avg(value) FILTER (WHERE attribute_category = 'positional')::numeric)::int AS positional_mean,
           round(avg(value) FILTER (WHERE attribute_category = 'mental')::numeric)::int AS mental_mean
    FROM player.player_attributes GROUP BY player_id
) m
UNION ALL
SELECT player_id, 'mental', 'teamwork', mental_mean FROM (
    SELECT player_id,
           round(avg(value)::numeric)::int AS technical_mean,
           round(avg(value) FILTER (WHERE attribute_category = 'positional')::numeric)::int AS positional_mean,
           round(avg(value) FILTER (WHERE attribute_category = 'mental')::numeric)::int AS mental_mean
    FROM player.player_attributes GROUP BY player_id
) m
ON CONFLICT (player_id, attribute_category, attribute_key) DO NOTHING;