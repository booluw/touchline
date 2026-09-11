# S10-02 — Implement manager-created competitions and competition reputation growth

**Status:** Not started  
**Sprint:** 10 — Agents, competitions, identity, and ownership  
**Source:** PRD §33; technical plan §16; OPENCODE.md  
**Depends on:** S04-01, S06-04

## What to do

Build the manager-created competition creation flow (`competition.competitions`, `competition.competition_rules`). Allow qualified managers to create custom tournaments (knockout cups, invitational leagues, pre-season trophies), set eligibility criteria, seed participants, and allocate prize money from club/entry funds. Track competition prestige and dynamic reputation growth over seasons as high-reputation clubs participate.

## Acceptance criteria

- `competition.competitions` stores custom competition formats, participant entry rules, scheduling windows, and prize funds.
- Managers can invite human or AI clubs to custom tournaments; entry fees are processed via append-only `finance.ledger_entries`.
- Match scheduler schedules custom tournament fixtures seamlessly alongside default domestic league schedules without fixture collision.
- Competition reputation updates dynamically each season based on participating clubs' average reputation scores.
- Winners and historic statistics are recorded permanently in competition history read-models.

## Delivery evidence

- Pending.
