# S06-04b — Direct manager messaging

**Status:** Implemented  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §66; technical plan §12; OPENCODE.md  
**Depends on:** S06-04a

## What to do

Implement the direct manager-to-manager messaging surface on `social.messages`: send, inbox, mark-read, rate limiting, basic sanitization, and realtime delivery of inbound messages to the recipient's active socket.

## Delivery evidence

- **`internal/social` messaging**: `SendMessage`, `ListInbox`, `MarkRead`, `UnreadCount`, `checkMessageRateLimit`, `sanitizeMessage` (HTML-tag strip + trim + ≤ `MaxMessageRunes` = 2000, over-length rejected not truncated), `MessagePush` (shared outbox/realtime payload). `EventMessageSent = "MESSAGE_SENT"` outbox event recorded in the same tx as the insert via `eventbus.WriteTx`.
- **Validation**: recipient must be a human manager (`is_policy_bot = false`) in the sender's world → `ErrBotRecipient`/`ErrManagerNotInWorld`; self-message → `ErrSelfMessage`; unknown → `ErrManagerNotFound`; empty → `ErrEmptyMessage`; over-length → `ErrMessageTooLong`.
- **Rate limits** (rolling, via `idx_messages_sender` from migration 0041): 30 messages/minute + 200/day per sender → `ErrRateLimited` (HTTP 429 + `Retry-After`).
- **Realtime**: `pkg/realtime` gains `EventSocialMessage = "social_message"`; `Service.WithRealtime(broker)` (optional, mirrors the matchday runner) publishes a best-effort world-scoped push after commit — the inbox read stays authoritative.
- **HTTP surface** (documented in `internal/apidocs/openapi.yaml`, docs-coverage green, 61 routes): `GET /api/messages` (inbox + unread, sender names resolved), `POST /api/messages` (201/400/404/413/429), `POST /api/messages/:id/read` (idempotent 200, foreign message → 404).
- **Wiring**: `internal/app` calls `socialSvc.WithRealtime(broker)`; the integration harness wires the same broker so WS tests reach the delivered envelope.
- **Tests** (integration): `internal/social/message_integration_test.go` (lifecycle, invalid recipients, sanitization/length, per-minute + per-day rate limits, realtime publish via a `LocalBroker` sink); `cmd/api/social_messages_integration_test.go` (HTTP round-trip incl. 401/400/404/413/429/`Retry-After`, cross-world 404, and `TestWSMessageDelivery` proving a sent message reaches the recipient's socket).

## Notes

- Delivery is **world-scoped, not per-recipient**: the socket fan-out follows the OPD-19 model (a world socket sees its world's stream), so a message appears to every socket in the recipient's world; the client routes it to the inbox. Postgres remains authoritative.
- Moderation/blocking/commissioner tooling is OPD-06 and lands in S10-04, not here.
- `subject` remains schema-nullable and unused in S06-04b (threaded/structured messaging is future work).