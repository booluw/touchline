# S15-01 — Implement multi-role actor framework for inhabitable universe roles

**Status:** Not started  
**Sprint:** 15 — Multi-role universe foundation  
**Source:** PRD §77; technical plan §16; OPENCODE.md  
**Depends on:** S13-02, S14-02

## What to do

Extend the command handler and actor authorization layer to support multiple inhabitable non-manager role types (Sporting Director, Player Agent, Journalist, Scout, Player). Refactor `ActorID` authorization checks to evaluate command execution against permissions assigned to the active role type. All actions across all inhabitable roles append to the single shared `world.events` event log.

## Acceptance criteria

- `ActorID` context supports role types (`manager`, `director`, `agent`, `journalist`, `scout`, `player`).
- Unified command dispatcher validates commands against specific role permission schemas.
- User accounts can select and inhabit active non-manager roles within a world.
- Actions taken by all role types publish typed events into `world.events` maintaining full causality tracking (`CausedBy`).
- Interface framework supports pluggable role-specific UI control panels.

## Delivery evidence

- Pending.
