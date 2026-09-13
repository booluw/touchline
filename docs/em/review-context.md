# Engineering Review Context

**Review date:** 2026-09-13
**Scope:** Architecture and implementation review only. No application code was changed.

## Request interpreted

This review records maintainability, correctness, and expansion risks for the engineer who owns implementation. Each actionable concern is in a separate sibling file. Findings are evidence-based; absence of a finding is not a claim that the area is defect-free.

## Sources read

- `OPENCODE.md` — repository handoff, architecture decisions, invariants, and stated delivery status.
- `docs/Touchline — Persistent Multiplayer Football Manager PRD.md` — product requirements for the asynchronous world clock, event-driven consequences, and deterministic replay.
- `docs/Touchline_Technical_Implementation_Plan.md` — the buildable architecture and event/tick direction referenced by the handoff.
- `docs/product_manager.md` — product decisions, especially OPD-15, OPD-17, OPD-19, and OPD-21.
- `docs/tasks/S02-03-configurable-world-clock.md` and `docs/tasks/S04-02-deterministic-match-engine.md` — acceptance criteria and claimed delivery behavior for the affected systems.
- The implementation paths cited in each concern, plus their focused integration/unit tests where present.

## Invariants used to assess findings

- PostgreSQL is authoritative; Redis is only ephemeral fan-out.
- `world.events` is intended to be the event spine for history, audit, replay, notifications, and derived read models.
- Event processing is at-least-once and handlers must be idempotent.
- World operations are multi-world and deterministic where competition or money is involved.
- Live matches are driven by the daily world tick; hourly/weekly/monthly/seasonal ticks are distinct engine inputs.

## Deliberate non-findings / limits

- This was a static review. It does not assert a production incident or execute a fault-injection campaign.
- Authentication CSRF/rate-limit gaps are already explicitly open in OPD-15 rather than newly discovered defects; they are not duplicated here.
- This review does not prescribe a particular code-level solution. The owner should select one consistent event-publication and time-model contract, then add failure-mode coverage around it.

## Findings index

| Priority | Concern | File |
|---|---|---|
| P0 | Committed state changes can miss asynchronous event delivery permanently | `event-delivery-atomicity.md` |
| P0 | Aggregate ticks are incorrectly used as a day counter for match scheduling | `world-calendar-tick-semantics.md` |
| P1 | Competition lifecycle events are not consistently durable or dispatchable | `competition-event-spine.md` |
| P0 | Outbox repair mistakes normal queue retention for failed delivery | `outbox-repair-redelivery-retention.md` |
| P1 | API-originated state changes are configured as log-only events | `api-event-publisher-wiring.md` |

## Follow-up review (2026-09-13)

The original three findings were re-checked after S04-04. The transactional producer paths, calendar-day separation, and competition error propagation are now present with focused tests. The two remaining concerns above arise from the completed implementation's repair and process-wiring behavior.
