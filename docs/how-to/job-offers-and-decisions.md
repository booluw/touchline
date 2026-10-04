# How to: job offers, accepting, and declining

The recruitment flow: how offers come into existence, what an offer contains,
and exactly what accepting or declining commits to — including the answer to
"what happens when a manager rejects a job offer?".

Relates to: [setup-and-launch.md](setup-and-launch.md) (registration
auto-offer, admin offers), [glossary.md](glossary.md)
("reputation", "career span", "offer status" entries),
[board-numerics](../design/board-numerics.md) (where the expectations come
from).

**The short answer to the rejection question:** declining an offer just marks
it `declined` with a timestamp. It is terminal for that offer, it is
**side-effect-free** (no career row, no reputation change, no event, no
punishment), and — because only one *pending* offer may exist per club+manager
— the club can later issue a **fresh** offer that the manager can weigh again.
There is no automatic re-offer; a new offer only comes from the admin endpoint.

---

## 1. How offers come into existence

Offers are issued to an **unemployed** manager by an **AI club**, two ways:

| Source | Endpoint / seam | Notes |
| --- | --- | --- |
| Registration auto-offer | `POST /api/auth/register` | When a new manager joins a playable world, a **random** AI club that can still offer (`OnboardingAIClubID`: AI-controlled, manager is nil or its own policy bot, **a league member**, and **not already proposing to another manager**) is offered via `OfferOnboardingJob`. `offer: null` if no such club exists. |
| Login | `POST /api/auth/login` | `EnsureOffer`: an unemployed human manager with **no pending offer** (never offered, declined with no club free, expired, resigned/sacked) gets the same random pick; returned as `offer` (else `null`). |
| Resign / sack re-offer | `POST /api/managers/me/resign`, board sack | Same random pick right after the exit, excluding the club just left. Best-effort; otherwise at next login. |
| Decline re-offer | `POST /api/offers/:id/decline` | Same random pick as registration, excluding the club just declined (see §4). |
| Admin | `POST /api/admin/offers` `{"club_id", "manager_id"}` | On behalf of an AI club at game start (or re-offer after a decline). |

**The league gate:** both paths refuse to issue an offer unless the club holds a
**domestic league membership** (`competition.club_competitions role='league'`).
An AI club outside a league returns `ErrClubNotInLeague` — the offer engine
only recruits into leagues that meaningfully matter.

### Offer creation guards (all `409`-family on violations)

- The candidate must be an unemployed, non-bot manager (`ErrManagerUnavailable`).
- The club must be AI-controlled (`ErrNotAIClub`) and, if it has a manager,
  that manager must be its policy bot (`ErrClubOccupied` — a human-managed
  club never issues offers).
- The club's world must be playable (`ErrClubNotPlayable`).
- The club must be in a league (`ErrClubNotInLeague`).
- Only one **pending** offer per **club** (`ErrClubHasOffer`): a club proposes
  to one manager at a time. Checked under the club row lock and backed by the
  partial unique index `uq_job_offer_pending_club` (migration 0058). The older
  per-(club, manager) index still exists; a duplicate there surfaces as
  `ErrOfferResolved`.

## 2. What an offer contains

`GET /api/offers` (your pending offers) and the create/accept/decline
responses return a `JobOffer` with the club identity plus **nested context
blocks** the candidate can read before choosing (writes nothing; blocks whose
data doesn't exist yet are omitted):

```jsonc
{
  "id": "…", "club_id": "…", "manager_id": "…", "status": "proposed",
  "created_at": "…",
  "club":      { "id": "…", "name": "…", "tier": 2, "reputation": 68 },
  "league":    { "id": "…", "name": "Tier 2 Division", "tier": 2,
                 "position": 5, "played": 12, "won": 7, "drawn": 2, "lost": 3, "points": 23 },
  "board":     { "season": 1, "persona": "demanding_owner",
                 "mandates": [ { "category": "primary", "target_type": "league_finish",
                                 "target_value": "4", "description": "finish … at or above position 4" }, … ] },
  "squad":     { "size": 24, "top_player": { "id": "…", "name": "…", "position": "ST", "overall": 82 } },
  "form":      { "rating": 1.12, "form_string": "W-W" },
  "supporters":{ "type": "working_class", "sentiment": 61 },
  "finance":   { "currency": "USD", "cash": 12345000, "operating_profit": …,
                 "transfer_budget": {…}, "wage_budget": {…}, "wage_commitments": {…},
                 "committed_spending": …, "projected_year_end_balance": …, "debt": 0 }
}
```

| Block | Meaning (how derived) |
| --- | --- |
| `club` | identity + `tier`/`reputation` |
| `league` | the club's domestic league; `position` + record computed from its best current-season standings line (points, id tiebreak). Omitted until the club has played. |
| `board` | the board persona + the **deterministic target set** the board would seed on assignment (`expectedFinish = clamp(round((110 − ambition)/8 [+1.5 if patience ≥ 70]), 1, 24)`, `expectedPoints = clamp(round(88 − 3.5·finish), 10, 95)`, plus break-even operating balance and wage structure). Computed on the fly — no mandate rows are written for a candidate. |
| `squad` | senior headcount (`active`/`injured`/`suspended`) + highest-rated player by `PositionalOverall` |
| `form` | the club's recent-form EWMA rating + `form_string`, when it has played |
| `supporters` | supporter archetype (`identity`) + current sentiment |
| `finance` | the full `FinanceSummary` read model (append-only ledger aggregates) |

## 3. Accepting — `POST /api/offers/:id/accept`

In one transaction: row-locks the offer, verifies it belongs to you
(`ErrNotOfferCandidate` → 409) and is still `proposed`
(`ErrOfferResolved` → 409), verifies the world is playable, that you are
still unemployed (`ErrManagerEmployed` → 409), and that the club is still
AI-run (`ErrClubOccupied` → 409 — a human who took the club first is never
displaced), then:

1. **The club's policy bot retires** (PRD §45 — an AI club hands over when a
   real manager accepts): `status = 'retired'`, no club. Not `unemployed` — a
   club-less unemployed bot is the world's single absence bot
   (`uq_managers_world_policy_bot`, narrowed in migration 0060).
2. You are assigned: `manager.managers` → `status='active'`,
   `current_club_id = club`; club → `current_manager_id = you`,
   `is_ai_controlled = FALSE`.
3. The offer is marked `accepted` (`responded_at` stamped); every other
   pending offer **from this club or to you** is marked `expired`, so the
   club's one pending slot is never held by a moot offer.
4. A **career span** is opened (`manager.manager_history`, role `manager`,
   `start_date = today`).
5. `JOB_OFFER_ACCEPTED` is emitted.
6. **Reputation +5** is appended (`careerDeltaAcceptedJob`, reason "took
   over").

The one-job-per-manager invariant is enforced by partial unique indexes plus a
database advisory lock; accepting while employed is rejected (`409`).

## 4. Declining — `POST /api/offers/:id/decline`

The current behavior, exactly:

1. Row-locks the offer; owner ≠ you → `409 ErrNotOfferCandidate`; status ≠
   `proposed` → `409 ErrOfferResolved`.
2. `UPDATE … SET status = 'declined', responded_at = now()`.
3. **Immediately re-offers** (`OfferOnboardingJob`): a random league club
   with no pending offer, never the club just declined (it may be offered
   again later). Best-effort — a failure is logged, the decline still stands.
4. Returns the declined offer plus `next_offer` (the replacement `JobOffer`,
   or `null` when no club is free).

No career history row, **no reputation delta** (accept `+5`, sack `−10`,
decline `0`), no world event, no explanation.

Consequences that *are* real:

- The offer is **terminal** — re-declining or accepting it returns
  `409 ErrOfferResolved`.
- The partial unique index on `(club_id, manager_id) WHERE status='proposed'`
  and the one-per-club index free the club, so it can propose to the next
  manager who registers or declines elsewhere.
- `expired` is produced by an accept (above) and by the **daily world tick**:
  an offer unanswered for **7 in-game days** (`offerTTLDays`, measured from
  `offered_on`, the world date at offer time — migration 0059) expires,
  freeing its club. The manager is not re-offered then; they get a fresh offer
  at their next login.

## 5. Ending a job you hold

- **Resign** (`POST /api/managers/me/resign`): career span closed, club back
  to AI control, `MANAGER_RESIGNED`-style event, reputation **unchanged** (0).
- Both resign and sack then immediately offer the manager another club (never
  the one they left; see §1).
- **Sack**: same mechanics, but actor is the board and the event is
  `MANAGER_SACKED` carrying a `board_confidence` explanation, and reputation
  is **−10**.
- In both paths the history row is **never deleted** — the career log is
  append-only (`manager.manager_history`).

## 6. Career reads

- `GET /api/managers/me/career` (or history seam) → `manager.manager_history`
  entries (jobs + notes, append-only).
- Reputation log: `manager.manager_reputation_events` append-only;
  `WorldReputationTotal` is `SUM(delta)` for a world.
- `GET /api/offers` lists your **pending** offers in a world only.

## 7. Routes and status codes

| Route | Success | Errors |
| --- | --- | --- |
| `GET /api/offers` | `200` list of pending offers (decorated) | — |
| `POST /api/offers/:id/accept` | `200` offer (`status: accepted`) | `404` not found · `409` not your offer / already resolved / employer conflict / club occupied |
| `POST /api/offers/:id/decline` | `200` offer (`status: declined`) | `404` not found · `409` not your offer / already resolved |
| `POST /api/managers/me/resign` | `200` | `409` not employed |
| `POST /api/admin/offers` | `201` offer (decorated) | `400` missing ids · `409` league gate / not AI / occupied / world / candidate unavailable · `422`-family world issues |

## 8. Common questions

**If I decline, can I change my mind?** Not on the same offer — it's terminal.
The club can issue a fresh one and you'd weigh that instead.

**Does declining hurt my reputation?** No. Career reputation only moves on
accept (+5) and sack (−10); resign and decline leave it untouched.

**Why is 'expired' in the enum if nothing produces it?** Forward-compat:
offers *can* expire in the schema (`proposed → accepted | declined | expired`),
but no scheduler currently drives it. Treat declined offers as the only
"closed by no" outcome today.

**Why must the offering club be in a league?** A talent contract exists to win
league football; a club outside one has no meaningful mandate board, form, or
league context to show a candidate — so the offer engine refuses it.