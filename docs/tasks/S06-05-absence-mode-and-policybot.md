# S06-05 — Implement absence mode delegation and PolicyBot fallback execution

**Status:** Not started  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §§45–47, 71–73; technical plan §10, §16; OPENCODE.md  
**Depends on:** S05-01, S06-01

## What to do

Implement manager delegation policies (`Policy` rows in `manager` schema) for absence mode and deadline resolution. Unify human commands and automated actions through single command handlers (`SelectSquad`, `RespondToBid`, `SetTrainingFocus`). When a manager misses an action deadline (e.g. pre-match line-up submission or bid expiry window), execute the handler via `PolicyBot` as the `ActorID`. Extend this mechanism to drive unmanaged AI-controlled clubs using personality-weighted policy parameters.

## Acceptance criteria

- Managers can configure standing delegation policies for squad selection (e.g. "Pick best fitness", "Rotate for cup"), bid responses (e.g. "Reject under 120% valuation"), and training routines.
- Scheduled pre-match or event deadlines check for human input; if absent, `PolicyBot` evaluates the manager's saved `Policy` and invokes the exact same command handler a human would call.
- All PolicyBot actions emit auditable events explicitly tagged with `ActorID = PolicyBot` for transparency and auditability.
- AI-controlled clubs use the PolicyBot architecture parameterized by club DNA and board expectations, eliminating duplicate simulation code.
- Managers receive clear summary logs upon returning from absence mode detailing all automated actions taken on their behalf.

## Delivery evidence

- Pending.
