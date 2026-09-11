# S14-02 — Implement multi-world matchmaking, cross-world reputation, and world scaling

**Status:** Not started  
**Sprint:** 14 — V2 infrastructure and world expansion  
**Source:** PRD §42; technical plan §18; OPENCODE.md  
**Depends on:** S02-02, S14-01

## What to do

Build the multi-world lobby and matchmaking UI, enabling new managers to browse available parallel worlds (differing in speed cadences, regional themes, or league formats). Enforce the single active club rule platform-wide (`one manager, one club at a time`). Maintain manager global career history across worlds while ensuring world hiring logic evaluates managers strictly using world-scoped reputation history.

## Acceptance criteria

- World lobby API `GET /api/worlds` lists active worlds, current tick speeds, open club vacancies, and manager populations.
- Platform enforces that a manager user account holds at most one active club assignment across all active worlds simultaneously.
- When moving between worlds (after resignation or sacking), a manager's global career history log is preserved for display.
- Hiring algorithms and board confidence evaluation in world B evaluate manager eligibility using world B scoped history only.
- System supports running hundreds of concurrent isolated worlds on the shared backend architecture.

## Delivery evidence

- Pending.
