# S10-01 — Implement player agents and semi-autonomous contract negotiations

**Status:** Not started  
**Sprint:** 10 — Agents, competitions, identity, and ownership  
**Source:** PRD §28; technical plan §16; OPENCODE.md  
**Depends on:** S06-01, S09-01

## What to do

Implement player agents as semi-autonomous entities represented in `social.relationships`. Agents represent players during contract renewals and transfer negotiations. Model agent personalities (greedy, patient, feeder-club oriented, aggressive) and agent commission demands. Agents leak transfer rumors to the news engine, demand release clauses, and drive hard bargains based on player performance and manager trust scores.

## Acceptance criteria

- Agents exist as entities in `social.relationships` representing cohorts of players across clubs.
- Contract and transfer negotiations require negotiating directly with the player's agent.
- Agent personality traits dictate negotiation style, patience thresholds, and agent fee expectations.
- High agent trust scores unlock favorable contract terms; low trust leads to public transfer demands and leaks to the news engine.
- All agent interactions append contract terms and agent fee debits to `finance.ledger_entries` with complete `Explanation` logs.

## Delivery evidence

- Pending.
