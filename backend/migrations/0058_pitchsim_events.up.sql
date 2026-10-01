-- ============================================================================
-- 0058_pitchsim_events — positional engine events in the match feed (IM34)
--
-- pkg/pitchsim adds outcome-neutral events (shot, save, tackle, foul, corner,
-- offside) on top of matchsim's. They share match.match_events:
--   source         which engine wrote the row. matchsim rows are the result;
--                  pitchsim rows never change a score.
--   offset_millis  where in its minute the event happens (match time), so the
--                  feed can be revealed in step with the 2D picture. NULL for
--                  matches played without the positional engine.
-- UNIQUE (match_id, sequence) is unchanged: matchsim rows keep their numbers and
-- pitchsim rows are numbered from 100000 (pitchsim.ExtraSequenceBase).
-- ============================================================================
ALTER TABLE match.match_events
    ADD COLUMN source TEXT NOT NULL DEFAULT 'matchsim' CHECK (source IN ('matchsim', 'pitchsim')),
    ADD COLUMN offset_millis INT CHECK (offset_millis >= 0 AND offset_millis < 60000);

ALTER TABLE match.match_events DROP CONSTRAINT IF EXISTS match_events_event_type_check;
ALTER TABLE match.match_events
    ADD CONSTRAINT match_events_event_type_check CHECK (event_type IN
        ('kickoff', 'goal', 'assist', 'yellow_card', 'red_card', 'substitution',
         'injury', 'chance_created', 'penalty_awarded', 'penalty_scored',
         'penalty_missed', 'half_time', 'full_time',
         'shot', 'save', 'tackle', 'foul', 'corner', 'offside'));
