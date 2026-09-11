# S06-02 — Implement board confidence, structured mandates, and manager sackings

**Status:** Not started  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §§9, 54; technical plan §§6, 8, 16; OPENCODE.md  
**Depends on:** S04-01, S05-02

## What to do

Build the board confidence scoring engine and `board_mandates` processing in the `club` and `manager` schemas. Model mandates as structured negotiable rows (primary/secondary/strategic/financial targets) with status tracking. Calculate job security scores based on mandate fulfillment, league standing, financial performance, and fan sentiment. Generate `Explanation` objects for all confidence score changes. Trigger automatic sacking events when confidence falls below the sacking threshold, terminating the manager's assignment and resetting the club to hiring mode.

## Acceptance criteria

- `club.boards` and `club.board_mandates` tables store structured targets (category, target_value, weight, status, deadline) per club.
- Periodic world ticks evaluate board confidence across league performance, financial health, and strategic mandate progress.
- Job security score is updated world-scoped and stored in `manager.job_security_snapshots` with full `Explanation` factor breakdowns.
- Managers can view their board confidence level (0-100%) alongside clear explanation breakdowns detailing exactly why confidence gained or lost points.
- If board confidence breaches the sacking threshold, a `MANAGER_SACKED` event is emitted, the manager's club assignment is removed, and the club enters PolicyBot/interim management.
- Fired managers retain their global career history log in `manager.manager_history` while becoming available for new job assignments.

## Delivery evidence

- Pending.
