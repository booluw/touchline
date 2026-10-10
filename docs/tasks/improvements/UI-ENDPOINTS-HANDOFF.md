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
| IM55 | Standings last-5 form | implemented 2026-10-09 |
| IM56 | League outlook: stakes + next-match swing | implemented 2026-10-09 |
| IM57 | Projected finish + Why factors | implemented 2026-10-09 |
| IM58 | Match engine xG | implemented 2026-10-09 |
| IM59 | Next 5 fixtures + difficulty | implemented 2026-10-09 |
| IM60 | Standings window ±3 around own club | implemented 2026-10-09 |
| IM61 | Team instructions + assistant tactical advice + tactics page | implemented 2026-10-10 |
| IM62 | Lineup picker (select/swap, desktop + mobile) + roster fitness/availability | implemented 2026-10-10, browser check pending |
| IM63 | Squad page + roster `recent_ratings` | implemented 2026-10-10, browser check pending |
| IM64 | Signed morale factors | implemented 2026-10-10, browser check pending |
| IM65 | Transfer-request preview + asking price + reassure route | implemented 2026-10-10, browser check pending |

## Status

- [x] Gap analysis (design vs router)
- [x] IM task files written (IM38–IM54)
- [x] Implementation IM38–IM46 (see each file's Delivery evidence); docs mirrored in `docs/product_manager.md` (OPD-61), Touchline Book ch. 9/10/12/16/20/21/23/26, `how-to/transfer-market.md`
- [x] Verification 2026-10-06: gofmt clean; build, vet, vet -tags integration, `go test ./...` pass; every touched integration test passes on embedded Postgres 16 (port 55432, launcher in the session scratchpad).
- Full serial integration run (`go test -p 1 -tags integration ./internal/... ./pkg/...`): 29 packages ok, 7 fail. `competition` fails 19 tests with the changes stashed too (scheduling/cup fixtures; existing). `match`, `matchday`, `player`, `policybot`, `social`, `scout` pass when run as a group, both before and after the change; they fail only after earlier packages leave state in the shared DB. `scout` `TestNextFixtureScout` fails on its own at an existing `league_position` assertion.
- [x] `/ui` component catalogue (`frontend/app/pages/ui.vue`, 2026-10-06): all 31 `components/ui` components with dummy data in the IM38–IM46 response shapes plus `toWhy`/`toFit`/`toStage` mappers; `nuxi typecheck` clean, dev server serves `/ui` 200. Not yet viewed in a browser.
- [x] `formatFixtureDateTimeSmart(iso, withTime = true)` (`frontend/app/utils/helpers.ts`, 2026-10-07): `false` returns date only ("Today"/"Tomorrow"/"Sat, Jun 14"). Not type-checked. Dashboard work in `components/dashboard/*` (NextFixture, CardCluster, AttentionPanel) is the user's own uncommitted edits.
- [ ] Follow-up: pitch token ratings on `/ui` are placeholders until the API exposes an effective slot rating (IM46 open question).
- [ ] Follow-up: `/ui` is public (outside `/play`); decide whether to gate or drop it before release.
- Not committed (per workflow).

## Next action

Squad screen (2026-10-10): IM63 **implemented, not committed** (OPD-65; Book §10 Squad screen). IM64/IM65 implemented, not committed. Gates 2026-10-10: backend gofmt/build/vet/`go test ./...` pass, integration PlayerSquad/Preview/Docs/Roster pass; frontend `nuxi typecheck` + eslint pass. Squad page layout: on desktop the page no longer scrolls, and the table and panel scroll independently (`pages/play/squad.vue`, eslint pass). The player header card (and the tabs on mobile) stays fixed while the cards below it scroll (`components/squad/PlayerPanel.vue`; typecheck + eslint pass). Table header now stays fixed while the rows scroll (`components/ui/DataTable.vue`: the table scrolls itself when its parent limits the height; squad table fills its column). Scrollbars app-wide (`assets/css/main.css`): faint and theme-aware (`--color-line2`, darkening to `--color-t3` when the bar is hovered), shown only while the scroll container is hovered. `UiWhyBreakdown` header: a long summary wraps instead of pushing "net" out of the card on mobile. Mobile player page: the header card and tabs stay fixed while the cards below scroll. `UiSegmentedControl` gains `fill`: full width, one equal column per option. Callers passed `class="grid grid-cols-N"`, but the root's own `inline-flex` won (computed display `flex`), so the tabs sized to their text and the highlight followed those uneven widths. PlayerPanel tabs and the tactics dials now use `fill`. Verified with headless Chromium on `/ui`: equal cells, highlight offset 0. `UiDataTable`: `fr` columns are `minmax(6rem, Nfr)`, so long text truncates and a too-wide table scrolls inside its card instead of widening the page. Density now defaults to simple below tablet width when no `density` is passed. LeagueTable uses that default, truncates club names, and has a fixed 76px form column with 12px dots. Headless check on `/ui` at 380px: the table scrolls within its card and the name column stays 96px. The competitions page itself was not browser-checked (needs login). Skeletons now match the loaded layouts. Squad desktop and the league table render the real `UiDataTable` (header, widths, density) with placeholder cells shaped like their content. New `components/squad/PlayerPanelSkeleton.vue` is used for the desktop panel and the mobile player page (with tabs). Squad mobile cards are card-shaped. typecheck + eslint pass; not browser-checked (needs login). The player page shows the panel skeleton for any player, including other clubs' players, whose loaded view is the old layout. Squad rows show the full name (first + last, falling back to the display name); search and name sort use it too. The player panel header and the old player view show the full name too. Squad mobile now matches the design: a fixed s1 header block (title + count/wages, position chips, "Sorted by … · Sort · Filter") over a scrolling card list (OVR, name + flag, pos·age·wage·expiry, coloured mood + 40×3 bar). Search, sort and status filters live in a bottom sheet with a Clear button and "Show N players". typecheck + eslint pass; not browser-checked (needs login). Competitions page: the league table's header stays fixed and only its rows scroll. On desktop the page fits the shell with each column scrolling on its own; on mobile the 68dvh box limits the table's height (fixtures still scroll that box). Headless check of the same wrapper chain: the table stays in its box, and after scrolling 60px the header is still at the top. New task: `/ui`'s many-option SegmentedControl overflows the page at 380px. New tasks: (b) `promise-playing-time` route still leaves a request pending — decide whether to keep it or restrict it to players without an open request. Follow-ups: browser check of `/play/squad` and the mobile `/play/players/:id`; 6 existing `internal/player` integration failures (listed in IM63 evidence).

Competitions screen (2026-10-09): IM55–IM60 **implemented, not committed** (OPD-63; Book §6.8–6.9, §15.5). Verification in each IM's Delivery evidence. Follow-ups: (a) `stakes.go` relegation safety counts only rivals that strictly overtake (ties ignored), looser than the outlook's rule; align if six-pointer/dead-rubber labels should match; (b) `TestListClubFixtures` is flaky (picks a club by name); (c) golangci-lint not run locally; (d) frontend wiring in progress (user's uncommitted `RaceCard.vue`, 2026-10-09).
- [x] Race card (2026-10-09): `types/manager/competition.ts` outlook types aligned to `LeagueOutlook`; `RaceCard.vue` renders stakes headline, races + attachments with status, guaranteed cup, W/D/L swing, finish range, projection why. `nuxi typecheck`: 0 errors in touched files (8 pre-existing elsewhere). Not viewed in browser; layout not checked against the design.


Product owner: answer the open questions in IM38–IM48, pick which deferred task (IM49–IM54) to plan first. Frontend wiring of the new fields is out of scope until asked.

Tactics (2026-10-10): IM61 **implemented, not committed** (OPD-64; Book §12.9; migration 0062). Next: browser check of `/play/tactics`; open question on crosses/familiarity data; pitch/lineup editor from the design not yet built.
