# S09-01 — Implement full player personality archetype system and hidden traits

**Status:** Not started  
**Sprint:** 09 — Relationship-driven football world  
**Source:** PRD §§11–12; technical plan §16; OPENCODE.md  
**Depends on:** S06-03, S08-02

## What to do

Implement complete player personality profiles (`player.player_personality`, `player.player_hidden_traits`). Model hidden variables (adaptability, ambition, loyalty, pressure handling, professionalism, sportsmanship, temperament, volatility). Personality archetypes drive how players react to transfer bids, benching, contract negotiations, manager team talks, and teammate transactions.

## Acceptance criteria

- `player.player_personality` and `player.player_hidden_traits` store full behavioral attributes per player.
- Personality traits dictate player reactions to management actions (e.g. volatile players react aggressively to missed promises; loyal players accept wage structures).
- Hidden traits are gradually revealed through manager interactions, scouting reports, and long-term performance observations.
- High professionalism and ambition traits positively influence training efficacy and recovery discipline.
- Interaction responses provide clear `Explanation` objects linking player reactions directly to underlying personality traits.

## Delivery evidence

- Pending.
