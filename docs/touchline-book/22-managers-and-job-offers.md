# Chapter 22 — Managers, careers and job offers

## 22.1 Managers

`manager.managers` has one row per (user, world) (`uq_managers_user_world`).

| Field | Meaning |
| --- | --- |
| `status` | `unemployed`, `active`, `retired` (retired = a policy bot replaced by a human) |
| `current_club_id` | the club held, if any |
| `is_policy_bot` | AI manager row ([Ch. 25](25-policybot-and-absence.md)) |
| `last_activity_at`, `away_since`, `away_auto`, `consecutive_missed` | absence detection |

Invariants:

- **One job per user, globally** (`uq_manager_one_job_per_user`, partial unique
  on active + club) plus an advisory lock (OPD-15).
- One active club per manager; a human-managed club never issues offers.
- One club-less unemployed **absence bot** per world
  (`uq_managers_world_policy_bot`, narrowed to unemployed rows in migration
  0060).

## 22.2 How a human gets a job

**The only sanctioned path is a job offer from an AI club** (OPD-16). There is
no direct assignment.

| Source | Trigger |
| --- | --- |
| Registration | `POST /api/auth/register` auto-offers a random eligible AI club |
| Login | `EnsureOffers`: an unemployed human is topped up to 5 pending offers (registration also issues 5, spread across countries then leagues) |
| After resign / sack | immediate re-offer, excluding the club just left |
| After decline | immediate re-offer, excluding the club just declined |
| Admin | `POST /api/admin/offers {club_id, manager_id}` |

An eligible offering club (`OnboardingAIClubIDs`) is AI-controlled, managed by
nobody or its own policy bot, **a league member**, and **not already proposing
to someone else**.

### Creation guards (409-family)

`ErrManagerUnavailable` (candidate not an unemployed human), `ErrNotAIClub`,
`ErrClubOccupied`, `ErrClubNotPlayable`, `ErrClubNotInLeague` (the **league
gate**), `ErrClubHasOffer` — **one pending offer per club**
(`uq_job_offer_pending_club`, migration 0058) — and the older per-(club,
manager) index (`ErrOfferResolved`).

## 22.3 What an offer shows

`GET /api/offers` returns pending offers decorated with context blocks
(omitted if the data doesn't exist yet):

| Block | Contents |
| --- | --- |
| `club` | identity, tier, reputation |
| `league` | position + record (points with an id tie-break) |
| `board` | persona + the **target set the board would seed** — expected finish/points, break-even operating balance, wage structure — computed on the fly, nothing written |
| `squad` | senior headcount, top player by overall |
| `form` | EWMA rating + form string |
| `supporters` | identity + current sentiment |
| `finance` | the full finance summary |

## 22.4 Accepting — `POST /api/offers/:id/accept`

One transaction, after locking and validating (owner, `proposed`, world
playable, you unemployed, club still AI-run):

1. the club's policy bot **retires** (`status = 'retired'`, no club);
2. you're assigned (`active`, `current_club_id`; club `is_ai_controlled =
   FALSE`);
3. the offer → `accepted`; every other pending offer **from this club or to
   you** → `expired`;
4. a **career span** opens (`manager.manager_history`, append-only);
5. `JOB_OFFER_ACCEPTED`;
6. **reputation +5**.

## 22.5 Declining — `POST /api/offers/:id/decline`

Marks the offer `declined` (terminal, `responded_at` set) and immediately
re-offers another club (best-effort). **No reputation change, no career row, no
event, no penalty.** The declined club is free to propose again later.

### Expiry

The daily tick expires offers unanswered for **7 in-game days**
(`offerTTLDays`, measured from `offered_on`, the world date at offer time —
migration 0059). The manager gets a fresh offer at next login.

## 22.6 Ending a job

| Exit | Mechanics | Reputation |
| --- | --- | --- |
| **Resign** `POST /api/managers/me/resign` | career span closed, club back to AI | 0 |
| **Sacked** by the board ([Ch. 23](23-board-and-job-security.md)) | same; event `MANAGER_SACKED` with the `board_confidence` explanation | **−10** |

Both re-offer immediately. History rows are never deleted.

## 22.7 Career reputation

`manager.manager_reputation_events` is append-only; a manager's world total is
`SUM(delta)` (`WorldReputationTotal`). Deltas: accept **+5**, sack **−10**,
resign 0, decline 0. The board's `alternatives_score` reads it
([Ch. 23](23-board-and-job-security.md)).

Reads: `GET /api/managers/me/career`, the reputation log, public profiles
([Ch. 24](24-social-and-rivalries.md)).

## 22.8 Routes

| Route | Success | Errors |
| --- | --- | --- |
| `GET /api/offers` | 200 pending offers + `taken_clubs` (clubs you let go that another human has since taken) | — |
| `POST /api/offers/:id/accept` | 200 | 404 · 409 not yours / resolved / employed / occupied |
| `POST /api/offers/:id/decline` | 200 (+ `next_offer`) | 404 · 409 |
| `POST /api/managers/me/resign` | 200 | 409 not employed |
| `POST /api/admin/offers` | 201 | 400 · 409 · 422 |

## 22.9 Planned

Ownership/chairman transition (S12-02), club creation (S12-01), richer hiring
markets where clubs compete for managers (OPD-09).

## Connections

- Board expectations shown in offers: [Chapter 23](23-board-and-job-security.md).
- Accounts and world membership: [Chapter 27](27-accounts-and-auth.md).
- Code: `internal/manager/{service,offercontext}.go`.
- Source: `docs/how-to/job-offers-and-decisions.md`, OPD-15, OPD-16, migrations 0058–0060.

---
[← Transfer market](21-transfer-market.md) · [Contents](the-touchline-book.md) · [Next: The board →](23-board-and-job-security.md)
