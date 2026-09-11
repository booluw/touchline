# S09-02 — Implement graph-derived squad dynamics and dressing room factions

**Status:** Not started  
**Sprint:** 09 — Relationship-driven football world  
**Source:** PRD §15; technical plan §§6, 16; OPENCODE.md  
**Depends on:** S06-04, S09-01

## What to do

Build the squad dynamics engine using graph CTE queries over `social.relationships`. Group players into dressing room hierarchy tiers (Team Leaders, Highly Influential, Influential, Other) and social factions (e.g. core veterans, foreign cohort, youth alliance). Model morale contagion across connected player graph edges, so selling a team leader's best friend or mistreating an influential player triggers dressing room unrest.

## Acceptance criteria

- Squad hierarchy and social groups are dynamically computed from `social.relationships` graph edges without redundant denormalization.
- Team leaders exert strong morale influence over their social cluster; mistreating a leader degrades morale across their connected faction.
- Managers can inspect squad hierarchy diagrams in the Nuxt UI showing social groups, dressing room cohesion, and overall manager support.
- Severe squad unrest (e.g. selling a team leader) can trigger a delegation of players demanding a board meeting or requesting transfer en masse.
- Morale contagion events emit auditable `SQUAD_UNREST_TRIGGERED` events with `Explanation` objects tracing causal relationship chains.

## Delivery evidence

- Pending.
