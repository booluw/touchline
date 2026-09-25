# IM11 — Season #1 kickoff date (optional pin) + launch announcements

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (season start / launch experience)
**Source:** Product decision (manual session)
**Depends on:** IM03 (fixture pacing — the day formula stamped at season
creation), IM05 (`allowed_weekdays` resolution + weekday pacing),
S09-03 (`world.news_stories` read-model and the country-scoped story contract).
Feeds the launch playbook in `docs/how-to/setup-and-launch.md`.

## Delivery evidence

- `internal/competition/seeding.go` — `StartSeason` is now a thin wrapper on a
  shared `startSeason(ctx, worldID, competitionID, kickoff *time.Time)`; new
  `StartSeasonKickoff(ctx, worldID, competitionID, kickoffDate *time.Time)`
  pins the league's first matchday. The anchor is resolved from the world's
  **current date** (`daysTruncate(worldRef).AddDate(0,0,current_day)`): with no
  date matchday 1 lands on current date **+ 1**; with a date matchday 1 lands
  on exactly that date (anchor = kickoff − 1). Validation is a pure helper
  `resolveKickoffAnchor(today, kickoff, allowedWeekdays)` (scheduling.go):
  a kickoff at/before the world's current date → `ErrKickoffDateInPast`; a
  kickoff off the league's resolved `allowed_weekdays` set (when one exists) →
  `ErrKickoffNotAllowedWeekday`. Both reject before any write (single tx,
  rollback leaves no season).
- `internal/competition/scheduling.go` — `publishAnnouncementNews`: two
  country-wide `announcement` stories — fixtures released + official kickoff
  day — written in the same transaction, each linked to the `SEASON_CREATED`
  event and reading `MIN(scheduled_at)::date` from the generated matchday-1
  fixtures.
- `internal/competition/service.go` — sentinels `ErrKickoffDateInPast`,
  `ErrKickoffNotAllowedWeekday`.
- `backend/migrations/0053_season_kickoff_announcement.up.sql` / `.down.sql` —
  adds `'announcement'` to the `news_stories` category CHECK. `migrations/README.md`
  row added.
- `internal/httpapi/competition_handlers.go` — `handleStartSeason` accepts an
  optional JSON body `{"kickoff_date":"YYYY-MM-DD"}` (empty body back-compat);
  malformed date → 400, past/off-weekday → 422.
- Tests — unit `kickoff_test.go` (anchor selection + both pacing invariants
  `scheduledAtFromDay` / `paceWeekdayMatchday`); integration
  `kickoff_integration_test.go` (pinned matchday 1, default next day, past /
  off-weekday rejection + rollback, weekday-set success, 2 announcements
  linked to `SEASON_CREATED`); HTTP `TestHTTPStartSeasonEndpoint` extended
  (201 pinned, 422 past, 400 malformed).
- Docs: `docs/how-to/seasons.md` (kickoff date + press-release flow, error
  mapping), `docs/product_manager.md` OPD-36.
- Verify: `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` all
  green; touched files `gofmt`-clean. Integration tests are compile-gated
  locally (live Postgres required).

## Recorded decisions

- **Default behavior is 'kick off as soon as the world can play'.** An empty
  body anchors the fixture calendar to the world's **current date**, matchday 1
  = the next day — replacing the old reference-date anchor
  (`launched_at`/`created_at`), which could schedule a pinned matchday far in
  the past or future. Existing `StartSeason` call sites are unchanged (they all
  run before the clock advances, so current-day == reference-day there).
- **A pinned date is authoritative for matchday 1.** `kickoff_date` shifts the
  whole calendar deterministically off anchor = kickoff − 1 day — the IM03 day
  formula and any IM05 weekday re-pacing reproduce around it; matchday 1 is
  exactly the requested day. Never read the wall clock in the computation.
- **Validation is write-free and symmetric.** Both rejections happen before
  anything persists; a failed call leaves no season, no `SEASON_CREATED`, no
  announcements, and no fixtures.
- **The weekday guard applies only when a set resolves.** A league/country
  without `allowed_weekdays` accepts any pinned date except the past; a pinned
  date still skips weekday snapping (the pinned day stands even if a weekday
  set exists — only a genuinely non-allowed weekday is rejected, and a date
  outside the set when the set is non-empty is the admin sending a date the
  calendar cannot reproduce).
- **Launch is announced world-wide in the same transaction.** Two country-scoped
  `announcement` stories (fixtures + kickoff day) ride the season tx and link
  to the `SEASON_CREATED` event — feeds never advertise a calendar the engine
  didn't keep.
- **Migration numbering:** IM11 consumed `0053`. IM10 (cup final-date policy,
  still Not started) planned `0053` — it must pick the next free number at
  implementation time.