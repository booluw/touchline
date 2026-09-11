# S10-03 — Implement club DNA archetypes and supporter group mechanics

**Status:** Not started  
**Sprint:** 10 — Agents, competitions, identity, and ownership  
**Source:** PRD §§6, 37; technical plan §§6, 16; OPENCODE.md  
**Depends on:** S03-01, S06-02

## What to do

Implement club DNA profiles (`club.club_dna`) and supporter group dynamics (`club.supporter_groups`). Club DNA archetypes (e.g., Youth Developer, Moneyball, Heavy Metal Football, Local Pride) govern AI-club transfer/tactical decisions and board expectations. Supporter groups track happiness, tradition demands, match attendance, and matchday ticket revenue.

## Acceptance criteria

- `club.club_dna` stores core identity weights (Youth Bias, Financial Conservatism, Tactical Style, Local Sourcing).
- AI clubs evaluate squad building, manager hiring, and transfer targets strictly through their assigned Club DNA rules.
- `club.supporter_groups` models fan confidence, matchday atmosphere influence, and ticket revenue scaling based on results and style.
- Deviating from core club DNA (e.g. selling academy stars at a Youth Developer club) causes rapid fan sentiment collapse and board pressure.
- Matchday attendance and gate receipts fluctuate dynamically based on supporter happiness and fixture importance.

## Delivery evidence

- Pending.
