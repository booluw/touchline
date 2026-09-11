# S08-02 — Implement dynamic potential growth and age-curve player development

**Status:** Not started  
**Sprint:** 08 — Academies and player development  
**Source:** PRD §10; technical plan §16; OPENCODE.md  
**Depends on:** S04-02, S08-01

## What to do

Build the dynamic player potential and progression engine. Rather than static fixed potential ceilings, calculate player growth dynamically based on competitive playing time, match performance ratings, facility quality, training intensity, and age curves. Handle physical and mental attribute decline for aging veteran players (30+ years old).

## Acceptance criteria

- `player.player_attributes` updates dynamically following periodic development ticks.
- Young players receiving regular first-team match minutes experience accelerated attribute growth and potential ceiling expansion.
- Lack of playing time or poor training discipline causes young player development to stagnate.
- Veteran players follow realistic physical attribute decay curves while preserving or increasing tactical/mental attributes.
- All attribute adjustments produce auditable event records accompanied by `Explanation` objects detailing development drivers (e.g., "+2 Stamina from high match minutes and quality training").

## Delivery evidence

- Pending.
