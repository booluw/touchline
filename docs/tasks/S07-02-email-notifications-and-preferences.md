# S07-02 — Implement notification preferences and transactional email worker

**Status:** Not started  
**Sprint:** 07 — MVP experience, operations, and validation  
**Source:** PRD §18; technical plan §18; OPENCODE.md  
**Depends on:** S01-02, S02-01

## What to do

Implement the `notification_preferences` table in the `manager` schema and build a dedicated Notification worker. The worker subscribes to `world.events` (e.g. `TRANSFER_BID_SUBMITTED`, `FINANCIAL_WARNING`, `MATCH_COMPLETED`, `MANAGER_SACKED`) and checks the manager's opted preferences (category opt-in/opt-out and delivery cadence: instant vs. daily digest) before dispatching emails. Wrap the transactional email client behind a clean interface (`pkg/notifier`) to support SES/Postmark/SendGrid interchangeably.

## Acceptance criteria

- `manager.notification_preferences` persists user choices per event category (Transfers, Finances, Match Reports, Board Updates) and delivery channel.
- Notification worker listens to event bus asynchronously without blocking engine event handlers.
- Transactional email service abstraction formats responsive HTML/text emails with direct deep links into the Nuxt application.
- Daily digest mode aggregates events over a 24-hour window and sends a single summary email per manager.
- Managers can update notification preferences via `/api/manager/preferences` API and UI settings screen.

## Delivery evidence

- Pending.
