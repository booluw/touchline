DELETE FROM match.match_events WHERE source = 'pitchsim';
ALTER TABLE match.match_events DROP CONSTRAINT IF EXISTS match_events_event_type_check;
ALTER TABLE match.match_events
    ADD CONSTRAINT match_events_event_type_check CHECK (event_type IN
        ('kickoff', 'goal', 'assist', 'yellow_card', 'red_card', 'substitution',
         'injury', 'chance_created', 'penalty_awarded', 'penalty_scored',
         'penalty_missed', 'half_time', 'full_time'));
ALTER TABLE match.match_events
    DROP COLUMN IF EXISTS offset_millis,
    DROP COLUMN IF EXISTS source;
