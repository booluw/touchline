# S06-03 — Implement player morale, playing-time tracking, and transfer requests

**Status:** Not started  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §§13–14; technical plan §§6, 16; OPENCODE.md  
**Depends on:** S05-01

## What to do

Implement player morale dynamics, playing-time tracking against agreed contract roles (Key Player, Squad Player, Youth, etc.), and unhappiness triggers. When player expectations are unmet over a rolling period, transition player state to unhappy, trigger manager interaction options (reassure, promise playing time, reject demand), and allow players to submit formal transfer requests. Morale changes must feed into match performance modifiers and emit detailed `Explanation` objects.

## Acceptance criteria

- `player.players` tracks current morale, happiness factors, agreed squad role, and rolling playing-time percentage.
- Match completion ticks evaluate actual playing time against expected role contracts; unfulfilled roles trigger morale degradation.
- Unhappy players generate notifications for the manager with explicit `Explanation` objects detailing the root cause (e.g. "Played 1 of last 5 matches despite Key Player contract").
- Managers can initiate basic player interactions (promise playing time, explain tactical benching, offer revised contract).
- Unresolved unhappiness leads to formal `PLAYER_TRANSFER_REQUESTED` events, placing the player on the transfer list and informing squad dressing room state.
- Low player morale applies deterministic negative attribute modifiers during match simulation calculations.

## Delivery evidence

- Pending.
