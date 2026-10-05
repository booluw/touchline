# Chapter 26 — Dashboard, news, scouting and realtime

The "Observe" step of the core loop. All four surfaces are **read models** over
state owned elsewhere; none writes game state.

## 26.1 The home dashboard (S07-01)

`GET /api/dashboard` returns three sections, each capped at **12** items:

| Section | Items (stable id prefix) |
| --- | --- |
| **urgent** | `bids:` open bid threads (seller side) · `contracts:` contracts ending within **30 days** · `match:` fixtures within **48 h** · `board:` latest confidence ≤ **25** · `cash:` negative cash · `finance:` unresolved crisis |
| **important** | `board-drop:` confidence fell **15+** since the previous review · `morale:` players at ≤ **0.35** or with an open request · `standings:` league position |
| **interesting** | `rivals:` completed fixtures against a rival · `market:` newest **5** listing/withdrawal/completed-transfer events |

Each item: `id`, `priority`, `category`, `title`, `description`, `created_at`,
optional `action` deep link (`respond_bid`, `renew_contract`, `set_lineup`,
`view_board`, `view_finances`, `view_player`, `view_standings`,
`view_fixture`). Constants: `internal/dashboard/model.go`.

**Pushes** are best-effort `dashboard_update` envelopes carrying only items not
yet pushed (dedupe by item id); removals are not pushed — the next GET
reconciles. Triggers: `PushWorldDelta` once per world tick after the passes, and
`PushCategory` on bid events for the human managers on both sides
([Ch. 21](21-transfer-market.md)). A club-less manager gets empty sections.

## 26.2 News (`world.news_stories`)

| Category | Producer |
| --- | --- |
| `announcement` | season #1 start: fixtures released + kickoff day ([Ch. 7](07-seasons-and-rollover.md)) |
| `scheduling` | league re-pacing that moved fixtures; cup round materialisation / final-date edits ([Ch. 6](06-leagues-and-scheduling.md), [Ch. 8](08-cups.md)) |
| `general` | cup qualification cascades ([Ch. 8](08-cups.md)) |
| `fan_reaction` | after each match, human-managed clubs ([Ch. 23](23-board-and-job-security.md)) |

Stories are written **in the same transaction** as the facts they describe and
link `related_event_id`. `country_id` scopes a story (NULL = world-wide);
`GET /api/news` shows world-wide + own-country stories. All text is
deterministic templates — no LLM. A fuller news generator is S09-03.

## 26.3 Scouting (`internal/scout`)

`GET /api/clubs/:id/next-fixture` returns the club's next match (with
`scheduled_at` and game-week) and an opponent dossier from persisted state:

- identity, reputation, current manager (no account data);
- league position (nil off-season / no league / before the first result);
- form: trailing-5 string + EWMA rating ([Ch. 17](17-form.md));
- squad size and top 3 players by overall;
- matchup context: rivalry intensity and `golden_goal` for knockout ties.

`nil` view = no upcoming fixture.

## 26.4 Realtime (`pkg/realtime`, OPD-19)

- **One WebSocket per session** at `/ws`, behind `requireAuth` (the httpOnly
  access cookie) with an `Origin` check against `APP_ORIGIN`; the world is
  derived from the manager row.
- **Redis is ephemeral transport only** (pub/sub fan-out across API pods). If
  `REDIS_URL` is unset/unreachable the processes fall back to an in-process
  broker — fail-soft.
- Envelopes: `world_tick`, `match_tick`, `dashboard_update`,
  `relationship_change`, `social_message`, plus `ping`/`pong` and error codes
  (`invalid_message`, `missing_type`).
- Pushes never gate gameplay; a failure is logged.
- Frontend: a singleton `useSocket()` composable feeding Pinia stores.

## 26.5 Email & PWA

Email notifications (S07-02, provider OPD-07) and PWA offline shell / Web Push
(S07-03) are in the MVP plan; check `docs/tasks/` for their delivery status.

## Connections

- Every item's source system is linked above.
- Code: `internal/dashboard`, `internal/scout`, `pkg/realtime`, `internal/httpapi/ws.go`.
- Source: `docs/design/dashboard-numerics.md`, OPD-19, S07-01.

---
[← PolicyBot](25-policybot-and-absence.md) · [Contents](the-touchline-book.md) · [Next: Accounts & auth →](27-accounts-and-auth.md)
