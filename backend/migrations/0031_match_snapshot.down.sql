ALTER TABLE match.matches
    DROP COLUMN IF EXISTS sim_inputs,
    DROP COLUMN IF EXISTS pacing_millis,
    DROP COLUMN IF EXISTS current_minute;