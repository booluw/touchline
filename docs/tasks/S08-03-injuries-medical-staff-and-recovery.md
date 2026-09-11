# S08-03 — Implement injury simulation, medical staff quality, and rehabilitation

**Status:** Not started  
**Sprint:** 08 — Academies and player development  
**Source:** PRD §18; technical plan §16; OPENCODE.md  
**Depends on:** S04-02, S05-01

## What to do

Build the match and training injury engine. Simulate realistic injury events (type, severity, duration) during match simulation and high-intensity training. Factor in player natural fitness, fatigue accumulated over congestion periods, pitch condition, and medical staff quality to determine injury probability and recovery time.

## Acceptance criteria

- `player.players` persists active injury status, injury type (e.g. hamstring pull, ACL tear), expected return date, and recovery progress.
- Match simulation deterministically triggers injury events based on match seed, player fatigue levels, and physical tackle interactions.
- Club medical staff quality level reduces recovery duration and lowers recurrence probability for previous injuries.
- Injured players are automatically blocked from selection in squad lineups; rushing players back early carries high recurrence risk.
- Every injury event emits `PLAYER_INJURED` and `PLAYER_RECOVERED` events with full `Explanation` context.

## Delivery evidence

- Pending.
