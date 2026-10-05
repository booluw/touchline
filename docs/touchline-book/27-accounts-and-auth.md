# Chapter 27 — Accounts, authentication and sessions

## 27.1 Accounts vs. managers (OPD-15)

- `auth.users` is the **account** (email, bcrypt password, `is_admin`).
- Membership in a world is a `manager.managers` row — **one per (user,
  world)**.
- **One job per user globally**, not per world.
- JWTs carry **no `world_id`**. The world is always derived from the manager
  row.

## 27.2 Tokens and cookies

| Token | Claims | Cookie | TTL (`JWT_ACCESS_TTL` / `JWT_REFRESH_TTL`) |
| --- | --- | --- | --- |
| access | `sub`=manager, `user_id`, `jti`, `exp`, `iat`, `type=access` | `access_token`, Path `/`, httpOnly | 15 m |
| refresh | `sub`=manager, `jti`, `exp`, `iat`, `type=refresh` | `refresh_token`, Path `/api/auth`, httpOnly | 720 h |

`Secure` is set whenever `ENV ≠ development`. CORS allows exactly
`APP_ORIGIN`. Refresh tokens are stored **hashed** in `auth.sessions`; the
`jti` guarantees uniqueness on rotation. `requireAuth` is DB-free (it validates
the JWT only).

## 27.3 Flows

| Route | Behaviour |
| --- | --- |
| `POST /api/auth/register` | creates a non-admin account; if exactly **one** playable world exists, joins it as an unemployed manager and auto-offers an AI club (`world`/`offer` may be `null`). No session minted; duplicate email → 409. Passwords: non-empty, ≤ 72 bytes (bcrypt); no other rules (OPD-54) |
| `POST /api/auth/login` | admin → world-less console session. Non-admin: active-job world wins; a jobless multi-world account gets `{"status":"worlds","worlds":[…]}` and no cookies (re-post with `world_id`); exactly one joined world auto-resolves; none → `403 "no world joined"` |
| `POST /api/auth/refresh` | rotates the session, new cookie pair |
| `POST /api/auth/logout` | deletes the session row for the refresh cookie, clears cookies; idempotent; works with an expired access token |

### One live session per account (IM13, OPD-39)

`uq_sessions_one_live_per_user` (partial unique on live rows). Login
hard-deletes the previous live session in the same transaction, so the other
device's refresh token dies immediately; its already-issued access JWT keeps
working until expiry (a documented ≤ 15-minute grace).

## 27.4 Admins

`is_admin` gates `/api/admin/*` (`requireAuth` + `requireAdmin`). Admin
sessions are world-less, so manager-scoped routes return `403 "no world
context"` unless the admin also has a manager row in that world
([Ch. 28](28-admin-console.md)).

`cmd/user-create` is the dev/admin bootstrap (`-admin`, `-world-id`,
`-password-env`), not the product signup.

## 27.5 Open items

OPD-02 (registration policy: age/privacy consent, verification, password
reset), OPD-54 (password rules — deferred, do not add), CSRF and rate limiting
(OPD-08/S11), anti-multi-accounting (S10-04).

## Connections

- World membership and job offers: [Chapter 22](22-managers-and-job-offers.md).
- Heartbeat used for absence detection: [Chapter 25](25-policybot-and-absence.md).
- Code: `internal/auth/{auth,register,session}.go`, `pkg/jwt`, `internal/httpapi/{auth_handlers,middleware}.go`.
- Source: `docs/development.md` (auth session flow), OPD-15, OPD-39, OPD-54, IM13, IM28.

---
[← Dashboard & surfaces](26-dashboard-news-scouting-realtime.md) · [Contents](the-touchline-book.md) · [Next: Admin console →](28-admin-console.md)
