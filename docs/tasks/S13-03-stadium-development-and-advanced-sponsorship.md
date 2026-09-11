# S13-03 — Implement stadium expansion projects and commercial sponsorship deals

**Status:** Not started  
**Sprint:** 13 — International world and careers  
**Source:** PRD §39; technical plan §16; OPENCODE.md  
**Depends on:** S05-02, S10-03

## What to do

Implement stadium expansion and infrastructure upgrade projects alongside commercial sponsorship deal negotiations. Board and managers can commission stadium expansion (seating capacity, corporate boxes, pitch quality) requiring multi-year capital expenditure debited via `finance.ledger_entries`. Negotiate shirt and stadium naming sponsorship packages driven by club reputation and league tier.

## Acceptance criteria

- `club.facilities` persists stadium seating capacity, corporate hospitality suites, and pitch condition.
- Stadium expansion projects require capital commitment, board approval, and multi-month construction timelines.
- During construction, matchday seating capacity is temporarily restricted; upon completion, matchday revenue capacity scales up.
- Commercial sponsorship deals (front-of-shirt, sleeve, stadium naming rights) present multi-year guaranteed income streams debited to `finance.ledger_entries`.
- Sponsorship offer values scale dynamically based on club division tier, fan base size, and global reputation.

## Delivery evidence

- Pending.
