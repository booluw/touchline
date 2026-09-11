# S12-02 — Implement manager-to-owner transition and chairman authority mechanics

**Status:** Not started  
**Sprint:** 12 — Club creation and ownership path  
**Source:** PRD §44; technical plan §16; OPENCODE.md  
**Depends on:** S12-01

## What to do

Build the manager ownership path allowing successful managers to buy equity in clubs or transition to chairman status. Model chairman governance powers: setting board mandates, appointing or sacking head coaches (human or AI), approving major capital expenditure, and determining long-term strategic direction.

## Acceptance criteria

- Managers can accumulate personal wealth through contract earnings and invest equity into club ownership.
- Transitioning to Chairman shifts the manager's UI view from daily tactical management to executive governance.
- Chairmen can hire, evaluate, and fire head coaches (human managers or AI PolicyBots) while delegating matchday tactics.
- Financial dividends and capital gains are tracked in manager financial accounts.
- Chairman actions append auditable events and emit `CHAIRMAN_APPOINTED` / `COACH_HIRED` events across the world log.

## Delivery evidence

- Pending.
