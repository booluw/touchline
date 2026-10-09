# IM56 — League outlook: what is at stake + next-match swing

**Status:** Planned
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html` competitions screen), product-owner review 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM07 (`qualify.go` qualification bands)

## What to do

New `GET /api/me/competitions/:id/outlook` (manager's own club in that league). Pure read; new `internal/competition/outlook.go`.

1. `stakes`: the single most relevant race, chosen in this order (first that applies):
   1. **Title race** — possible while leader_pts − our_pts ≤ 3 × our_games_left. Returns gap, games_left, max_reachable_points.
   2. **Promotion / relegation line** — gap to the nearest zone boundary (`promotions`, relegation spots from league config).
   3. **Cup qualification** — every competition the league is attached to: all `competition.cup_qualification` rows for this `league_id` (domestic, regional, world — every scope). Nearest band above and below our position, with points gap.
2. `attachments`: the full list behind (1.3), always returned — `[{cup, scope, from_position, to_position, status}]` where status is `in` / `outside` + points gap — so the card can show every place the league feeds, not only the nearest.
3. `next_match`: for our next league fixture, our table position after a win / draw / loss, holding every other club's points constant (re-sort of current table; tiebreaks = existing standings order).
4. **Clinch status** on every race (title, promotion, relegation, each attachment): `clinched` | `alive` | `eliminated`, all derived from one guaranteed **finish range** `[best, worst]` for our club (`pts`, `max = pts + 3 × games_left`):
   - `best  = 1 + count(clubs with pts_j > max)`: clubs strictly out of our reach.
   - `worst = 1 + count(clubs with max_j >= pts)`: clubs that can still finish level or above. **Equal points count against us** because GD/goals tiebreaks can still change. This is the "lose it by a point" guard.
   - Once every league fixture is played, `best = worst = ` the actual final position (tiebreaks applied).
   - Any race covering positions [a, b]: **clinched** iff a ≤ best and worst ≤ b; **eliminated** iff worst < a or best > b; otherwise **alive**.
     Champion = [1, 1]; promoted = [1, promotions]; relegated = [N − relegations + 1, N]; each attachment = its band.
   - Also return `guaranteed_at_least`: set only when **every** position in [best, worst] falls inside some attachment band. It names the band that contains `worst` (our floor), e.g. "Qualified for at least the Regional Cup" while a higher cup is still possible. If any position in the range has no band, it is null.
   - Copy: clinched → "Champions", "Promoted", "Relegated", "Qualified for <cup>"; alive → "on course for" / gap text. Never a claim the maths has not proved.
5. **Leagues without movement**: races with no slots (`promotions = 0`, no relegation, no `promotes_to`/`relegates_to`, no attachments) are skipped, never shown as "0 pts off". A league whose only race is the title (no relegation, nothing above, no attachments) gets just the title race: alive → gap, clinched → "Champions".
6. Mirror in `openapi.yaml`.

## Recorded decisions

- Attachments cover **all** competitions the league qualifies for, every scope (product owner, 2026-10-09). Source is the `cup_qualification` table, so a new regional/world cup appears automatically.
- Say "Qualified/Promoted/Relegated/Champions" when it is mathematically certain (product owner, 2026-10-09). Before that, "on course for".
- The clinch check is deliberately conservative: it assumes every rival can win all remaining games even when two rivals still play each other. It can be late to declare, but it can never declare wrongly. An exact check (one that accounts for head-to-head fixtures) is a max-flow problem; only add it if late declarations prove noticeable.
- Equal points never clinch before the final whistle; tiebreaks are only trusted on the final table.
- "Qualified" means the league position is secured. Actual cup entry still runs through IM07/IM09 (double-booking sweep, manager cup choices); if a club is later displaced there, that is reported by the cup flow, not here.

- **No play-offs** — the engine does not model them and will not (product owner, 2026-10-09). The design's "play-off race" card becomes the stakes card above.
- Priority order title → promotion/relegation → cup qualification confirmed by product owner.
- Next-match swing ignores other same-day results (simple, explainable); documented in the Touchline Book, no extra response field.

## Tests

Unit (table-driven): title race on/off at the exact 3×games boundary; zone gap at top/bottom edges; no applicable band → stakes falls through; league attached to a domestic + a regional + a world cup returns all three; clinch boundaries: leader 1 pt clear of a rival who can still win every game → `alive`, not champion; level on points with 0 games left but fixtures unplayed elsewhere → `alive`; strictly out of reach → `clinched`; all fixtures played → final table decides including tiebreaks; band [3, 6] is not clinched while we can still reach 2nd (best = 2), but with bands [1, 2] and [3, 6] and worst ≤ 6, `guaranteed_at_least` names the [3, 6] cup; with a gap in coverage it is null; a league with no promotion, no relegation and no attachments returns only the title race; win/draw/loss positions with ties.
