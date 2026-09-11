# S13-02 — Refactor player/manager identity into Person entity for retired player careers

**Status:** Not started  
**Sprint:** 13 — International world and careers  
**Source:** PRD §61; technical plan §16; OPENCODE.md  
**Depends on:** S08-02, S12-02

## What to do

Refactor player and manager schemas to share a unified `Person` entity identity. When players retire from active competition, transition their `Person` record into secondary football careers (Assistant Manager, Scout, Agent, Pundit, or Club Manager) preserving their complete playing history, relationships, and personality attributes.

## Acceptance criteria

- `world.persons` (or shared schema) unifies identity records across players, managers, scouts, and agents.
- Retiring players undergo career conversion evaluation based on mental attributes, leadership, and staff preferences.
- Former players can be hired by human managers as assistant coaches, scouts, or youth directors.
- Relationship edges (`social.relationships`) built during a player's active playing career carry over seamlessly into their staff career.
- Complete life career view renders full timeline from academy debut through player retirement and staff milestones.

## Delivery evidence

- Pending.
