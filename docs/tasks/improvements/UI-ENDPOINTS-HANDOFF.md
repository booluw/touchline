# UI redesign → backend endpoints: working handoff

Working memory for the "new UI design" endpoint programme (started 2026-10-06,
branch `feat/ui`). Read this first when resuming; update the Status section at
each milestone.

## Source

- Design: claude.ai/design project `244e00dd-271d-47ca-ad99-af4f1d67b9c9`
  (read via the `DesignSync` tool, `get_file`). Screen files:
  `Touchline.dc.html` (shell + Home), `Touchline Squad.dc.html`,
  `Touchline Transfers.dc.html`, `Touchline Matchday.dc.html`,
  `Touchline Finances Board.dc.html`, `Touchline Landing Auth.dc.html`,
  `Touchline Screens.dc.html` (tactics, training, scouting, academy, club,
  competitions, world, career, social).
- Data in the designs lives in the `<script type="text/x-dc">` block of each file.

## Scope rules from the product owner

1. Every change gets an IM task file (template: Status / Sprint / Source /
   Depends / What to do / Recorded decisions / Delivery evidence).
2. Implement the **improved (existing) endpoints** only.
3. Write tasks but **do not implement**: scouting, shortlist, social,
   new auth (password reset, handle availability, OAuth), preferences.
4. Mirror every route/field change in `backend/internal/apidocs/openapi.yaml`.
5. **Do not assume.** A field is added only when the engine already stores or
   computes it. If the design needs data the engine does not model, record it
   as an open question in the task, do not invent it.
6. No commit unless asked.

## Known constraints found while checking

- IM37/OPD-60: potential is never exposed. The Squad design shows potential
  in Advanced mode. That conflicts, so potential stays hidden (open question).
- Dashboard `Item` (`internal/dashboard/model.go`) has id, priority,
  category, title, description, created_at, action{kind, ids}. It has no factors.
- `finance` responses already carry `factors`; `board` carries scores + `explanation`.

## Task map

| IM | Area | Implement now? |
|----|------|----------------|
| IM38 | Dashboard counts, board factors, summary panel | yes |
| IM39 | Next fixture: own club position/form | yes |
| IM40 | Roster: nationality, age, contract | yes |
| IM41 | Club profile fields | yes |
| IM42 | Finance health, breakdown, cash history, ledger filter | yes |
| IM43 | Board members/history + negotiation preview | yes |
| IM44 | Bid negotiation rounds | yes |
| IM45 | Match event stats | yes |
| IM46 | Lineup position fit | yes |
| IM47 | Training view | no API change (data on roster) |
| IM48 | Onboarding club choice | no API change (needs preference model) |
| IM49–IM53 | Scouting, shortlist, social, auth extras, preferences | deferred |
| IM54 | Career/world/landing/press (new endpoints) | deferred, confirm scope |

## Status

- [x] Gap analysis (design vs router)
- [x] IM task files written (IM38–IM54)
- [x] Implementation IM38–IM46 (see each file's Delivery evidence); docs mirrored in `docs/product_manager.md` (OPD-61), Touchline Book ch. 9/10/12/16/20/21/23/26, `how-to/transfer-market.md`
- [x] Verification 2026-10-06: gofmt clean; build, vet, vet -tags integration, `go test ./...` pass; every touched integration test passes on embedded Postgres 16 (port 55432, launcher in the session scratchpad).
- Full serial integration run (`go test -p 1 -tags integration ./internal/... ./pkg/...`): 29 packages ok, 7 fail. `competition` fails 19 tests with the changes stashed too (scheduling/cup fixtures; existing). `match`, `matchday`, `player`, `policybot`, `social`, `scout` pass when run as a group, both before and after the change; they fail only after earlier packages leave state in the shared DB. `scout` `TestNextFixtureScout` fails on its own at an existing `league_position` assertion.
- Not committed (per workflow).

## Next action

Product owner: answer the open questions in IM38–IM48, pick which deferred task (IM49–IM54) to plan first. Frontend wiring of the new fields is out of scope until asked.
