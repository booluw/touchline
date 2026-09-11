# S12-01 — Implement club creation flow, financial backing validation, and lower-tier placement

**Status:** Not started  
**Sprint:** 12 — Club creation and ownership path  
**Source:** PRD §43; technical plan §16; OPENCODE.md  
**Depends on:** S11-02

## What to do

Build the custom club creation workflow. Allow experienced managers with sufficient career reputation/funds to found new clubs, select crests, kit colors, home city, and stadium names, validate financial backing requirements, and enter lower-tier regional leagues at season boundaries. Generate initial starter squad rosters using `pkg/playergen`.

## Acceptance criteria

- Managers meeting career reputation thresholds can initiate custom club creation during seasonal transition windows.
- Creation wizard validates club naming, branding assets, stadium parameters, and initial financial capital injection.
- Newly created clubs are placed into the lowest active tier of the chosen region's league hierarchy without disrupting competition structures.
- Initial roster of starter players is generated via `pkg/playergen` parameterized by the club's regional location.
- Creation appends starting financial ledger entries and emits `CLUB_CREATED` events across the world event log.

## Delivery evidence

- Pending.
