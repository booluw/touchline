# S08-01 — Implement academy investment tiers and procedural youth intake

**Status:** Not started  
**Sprint:** 08 — Academies and player development  
**Source:** PRD §23; technical plan §16; OPENCODE.md  
**Depends on:** S01-04, S05-02, S07-04

## What to do

Build the youth academy investment model (`club.academies`, `club.facilities`). Implement configurable annual investment tiers, maintenance overhead costs appended to `finance.ledger_entries`, and annual procedural youth intake generation. Parameterize `pkg/playergen` by region to produce nationally plausible youth prospects matching club academy location. Support tactical academy management options including shutting down academies for emergency cash relief or expanding investment for higher prospect potential distribution.

## Acceptance criteria

- `club.academies` tracks investment tier, facility level, regional location, and annual operational costs.
- Annual youth intake events trigger `pkg/playergen` parameterized by club region, instantiating a cohort of 15-18 year-old youth players in `player.players`.
- Academy maintenance expenses are debited as recurring append-only entries in `finance.ledger_entries`.
- Managers can upgrade, downgrade, or shut down academy operations; shutting down yields immediate budget savings but emits negative board/supporter explanation events.
- Higher investment tiers probabilistically skew initial attribute baselines and ceiling potential for generated prospects.

## Delivery evidence

- Pending.
