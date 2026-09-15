# A13 — Manager registration and onboarding (self-service signup + auto-offer)

**Status:** In progress
**Sprint:** Ad-hoc (manager onboarding)
**Source:** Product decision (manual session); OPD-15(4)/(6); OPD-16(2)
**Depends on:** A12; S02-01 (auth); S02-02 (job offers); S03-01 (seeded AI clubs)

## What to do

Add a self-service registration endpoint that creates a plain (non-admin) account,
automatically joins it to the **single playable world** as an unemployed manager,
and — when that world has an unmanaged AI club — automatically issues a first job
offer so a registered player can accept and be immediately playable. `cmd/user-create`
remains the admin/dev bootstrap and is NOT replaced by this endpoint.

## New endpoint

`POST /api/auth/register` (public)

Body: `{email, password, display_name?}`.
Returns **201** `{id, email, display_name, is_admin, world: {...}|null, offer: {...}|null}`.

Behaviour:
- Unique email → 409 `ErrEmailTaken`.
- bcrypt-hash and insert `auth.users` (`is_admin=FALSE`, `display_name` defaults
  to the email local part).
- **Exactly one playable world** (`world.worlds.status IN ('active','open_beta')`)
  → also insert an unemployed `manager.managers` row for that world. **Zero or
  two or more** playable worlds → account only (`world=null`; recorded decision).
- After the join, if a world was joined: find the onboarding AI club and issue a
  job offer via the existing `manager.Service.CreateJobOffer`. Offer issuance is
  **non-fatal** — any failure logs and yields `offer:null` (an admin can offer
  later). The offer is created in its own transaction after the account tx
  commits.

## Changes

### internal/auth

- `Service.Register(ctx, RegisterParams{Email, Password, DisplayName, IP,
  DeviceFingerprint}) (*RegisterResult, error)` — one transaction for
  user insert + world join; `RegisterResult{UserID, ManagerID *uuid.UUID,
  JoinedWorld *WorldInfo, CreateAccount}`.
- New sentinel `ErrEmailTaken` (unique-violation, HTTP 409).
- Factor the insert logic out of `cmd/user-create` where practical (optional
  cleanup; the CLI keeps its `-admin`/`-world-id` surface).

### internal/manager

- `Service.OnboardingAIClubID(ctx, worldID) (uuid.UUID, error)` — deterministic
  selection (`ORDER BY id LIMIT 1`) of the first club in the world that is
  `is_ai_controlled` and whose current manager (if any) is a policy bot — i.e.
  an AI club that can still issue offers (CreateJobOffer's validation). Returns
  `ErrNoOnboardingClub` when none.

### internal/httpapi

- New `handleRegister` + route `POST /api/auth/register` (public) in
  `router.go`, next to login/refresh.
- Validation: non-empty email (contains `@`), non-empty password; 400 on bad
  body.

### Tests

- `internal/auth`: `Register` unit/integration — success with join, duplicate
  email → ErrEmailTaken, no playable world → JoinedWorld nil, two worlds → nil.
- `cmd/api` integration: end-to-end happy path — register → offer present →
  login as the real (non-minted) manager → `GET /api/clubs` (200) → accept the
  auto-offer → club flips `is_ai_controlled FALSE` → manager endpoints work off
  that real session. Variance: no AI club → `offer:null`.

### Docs

- `internal/apidocs/openapi.yaml`: document the register endpoint (request,
  201/400/409 responses, `World`/`JobOffer` refs). Route-coverage test keeps
  spec ≈ router.
- `docs/development.md`, `docs/how-to/setup-and-launch.md`: Step 3 becomes the
  product registration flow; `cmd/user-create` documented as the admin/dev
  bootstrap; troubleshooting rows for "no playable world" and "no auto-offer".

## Recorded decisions

- Registration auto-joins only when there is **exactly one** playable world;
  with zero or several the new account is created world-less.
- Auto-offer target is deterministic (first AI club by id) and non-fatal.
- No email verification/recovery (remains open under OPD-02); registration rate
  limiting deferred (S11); device fingerprint captured for future
  anti-multi-accounting hooks (field already exists on `auth.sessions`).