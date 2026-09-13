-- =====================================================================
-- 0031: live match snapshot (S04-02, OPD-21)
--
-- A live match (fixtures.status='live', matches.status='in_progress') runs
-- on its own real-time goroutine, independent of the world clock. When it
-- kicks off we persist the full deterministic simulation input — the built
-- home/away teams, XIs, benches, form states, fixture context and engine
-- seed — as a JSONB snapshot on the matches row. The pacing goroutine and
-- any rehydrated worker both derive the stream from (seed + snapshot +
-- ordered match.match_inputs), so the identical match resumes after a
-- restart.
--
-- pacing_millis records the world-config cadence resolved at kickoff
-- (tick.match_cadence as a Go duration; absent/malformed falls back to
-- matchsim.DefaultTuning().LivePacingSecondsPerMinute); the pacing loop
-- sleeps per simulated minute. Stored in millis so 10ms test cadences and
-- sub-second configurations survive a worker restart.
--
-- current_minute is the engine minute the pacing loop has produced so far
-- (0 at kickoff, 90 at full time). It is the authoritative "match clock" for
-- input validation and rehydration: minutes are not all event-bearing, so
-- MAX(match_events.minute) would under-count silent minutes.
-- =====================================================================

ALTER TABLE match.matches
    ADD COLUMN sim_inputs     JSONB,
    ADD COLUMN pacing_millis  INTEGER,
    ADD COLUMN current_minute INTEGER NOT NULL DEFAULT 0;