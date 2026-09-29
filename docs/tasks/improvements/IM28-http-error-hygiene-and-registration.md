# IM28 — HTTP error hygiene and registration hardening

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (API correctness / security)
**Source:** Doc-vs-code audit against the OpenAPI spec and A13:
1. `GET /api/fixtures/:id` with an unknown id returned 500; the spec and the
   handler's own doc say an unknown fixture is a 404, indistinguishable from a
   foreign one.
2. `internalError` returned `err.Error()` to clients, leaking SQL and schema
   detail on every 500.
3. A13 says a failed auto-offer is logged; the handler discarded the error.
4. A password over 72 bytes made bcrypt fail and registration return 500.
5. `ErrNotAuthorized` still read "login is currently limited to
   administrators" (A12 removed that gate); it is only produced when a revoked
   admin refreshes.
**Depends on:** A12, A13, S04-03.

## What to do

Map the not-found case to 404, return a generic 500 body, log onboarding offer
failures, reject over-long passwords with 400, and reword the sentinel.

## Delivery evidence

### Backend

- `backend/internal/httpapi/match_handlers.go` — `pgx.ErrNoRows` → 404
  `fixture not found`.
- `backend/internal/httpapi/auth_handlers.go` — `internalError` logs the cause
  and returns `{"error":"internal server error"}`; `handleRegister` logs
  `OnboardingAIClubID` / `CreateJobOffer` failures and maps
  `ErrPasswordTooLong` to 400.
- `backend/internal/auth/auth.go` — `ErrPasswordTooLong`; `ErrNotAuthorized`
  reworded ("administrator access is no longer granted to this account").
- `backend/internal/auth/register.go` — rejects passwords over 72 bytes.
- `backend/internal/apidocs/openapi.yaml` — register `password.maxLength: 72`
  and the 400 description; `Error.error` documents the generic 500 body.

### Tests

- `internal/httpapi/match_feed_integration_test.go` —
  `TestMatchFeedScopedToCallerWorld` also asserts an unknown fixture is 404.

### Verification

See [IM29](IM29-single-backend-image-deploy.md#verification).

## Recorded decisions

- **500 bodies are generic**; causes live in the server log only.
- **No minimum password length was added**: no document defines one, so it is
  recorded as an open product decision (OPD-54) rather than invented. The
  72-byte maximum is a technical limit of bcrypt, not a product rule.
