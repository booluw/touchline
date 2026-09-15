# A12 — Manager login (open the login gate, OPD-15(4) world resolution)

**Status:** In progress
**Sprint:** Ad-hoc (manager onboarding)
**Source:** Product decision (manual session); OPD-15(4) in `docs/product_manager.md`; PRD auth/session
**Depends on:** S02-01 (auth + session flow)

## What to do

Open the Phase-1 admin-only login gate so non-admin accounts with manager rows can
log in and obtain a **manager-scoped session**, adopting the OPD-15(4) world
resolution rules. Admin accounts keep their world-less console session (recorded
decision: admins always get the console; playing requires a non-admin account).

## Login world resolution (OPD-15(4), concrete)

For a non-admin account, load every `manager.managers` row for the account
(excluding memberships of `archived` worlds — terminal per OPD-16). Then:

1. **(a) Job wins.** Exactly one manager row with `status='active'` and
   `current_club_id IS NOT NULL` → mint a session pinned to that manager/world
   (enforced unique by `uq_manager_one_active_club` + one-job-per-user).
2. **(b) Explicit pick.** Login body carries `world_id` → mint a session pinned to
   that world's manager row; `ErrNotMember` (403) if the account has no row there.
3. **(c) One world.** Non-admin has manager rows in exactly one non-archived
   world → mint that session.
4. **(d) World picker.** Non-admin has rows in **two or more** non-archived
   worlds → return `{"status":"worlds","worlds":[{world_id,name,status}]}`, set **no
   cookies**; the client re-posts login with a chosen `world_id` (rule b).
5. **(g) No manager rows.** → `ErrNoManager` (403, "account exists, no world joined").

## Changes

### internal/auth

- Remove the `!isAdmin → ErrNotAuthorized` gate from `Service.Login`.
- New sentinels: `ErrNoManager`, `ErrNotMember`. `ErrNotAuthorized` is no longer
  produced by the login path.
- `LoginResult` gains `Worlds []WorldInfo` (`nil` when a session is minted);
  `WorldInfo{ID uuid.UUID, Name string, Status string}`.
- `LoginParams` gains `WorldID *uuid.UUID` (optional explicit pick, rule b).
- `Service.Refresh`: fix the revoked-admin sentinel — return `ErrNotAuthorized`
  (matching the existing test) instead of `ErrInvalidRefresh`. Manager-session
  refresh is unchanged (world re-derived from the manager row — no world in JWT).

### internal/httpapi

- `loginRequest` gains optional `world_id`.
- `handleLogin`:
  - session outcome → set cookies + return `{display_name, created_at, is_admin,
    id}` (unchanged).
  - worlds outcome → 200 `{"status":"worlds","worlds":[...]}`, no cookies.
  - `ErrNoManager` / `ErrNotMember` → 403 with the sentinel message.
- No new routes in A12.

### Tests

- `internal/auth/auth_integration_test.go`: replace the non-admin-rejected-login
  test with resolution cases (single-world, multi-world picker, explicit
  `world_id`, wrong-world `world_id` → ErrNotMember, no manager rows →
  ErrNoManager). Admin console behavior tests unchanged; update the revoked-admin
  refresh test if the sentinel assertion already agrees (it expects
  `ErrNotAuthorized`).
- `cmd/api`: integration test asserting a plain account cannot log in becomes a
  manager-session test; a no-manager-row account → 403 "no world joined".
  `managerCookies` minted-session helper stays for isolated tests.

### Docs

- `internal/apidocs/openapi.yaml`: `POST /api/auth/login` — optional `world_id`
  request field, `worlds` picker response shape, `ErrNoManager`/`ErrNotMember`
  errors. Route-coverage test keeps spec ≈ router.
- `docs/development.md`, `docs/how-to/setup-and-launch.md`: the "plain account
  can't log in" gate text/troubleshooting rows are replaced by the resolution
  rules.

## Recorded decisions

- Admins always receive a world-less console session; an admin who wants to play
  uses a separate non-admin account.
- "Active world" for resolution = any membership except `archived` worlds.
- No email verification/recovery (still open under OPD-02); no login rate
  limiting (deferred to S11).