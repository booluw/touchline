# The 2D match simulation (IM34)

How the picture of a match is produced, switched on, and kept in step with the
result. Engine contract and track format:
[`backend/pkg/pitchsim/README.md`](../../backend/pkg/pitchsim/README.md).

## 1. Two engines, one result

`matchsim` decides every match exactly as before. `pitchsim` is a second,
separate engine that takes matchsim's events and produces the movement that
leads to them, plus extra events (shots off target, saves, tackles, fouls,
corners, offsides). The extra events never change a score, a rating or an
injury. With the simulation off, nothing about a match changes.

## 2. Switching it on

Per world, with the world config key `match.visual_engine`:

| Value | Effect |
| --- | --- |
| `off` (default, or key absent) | matches play exactly as before |
| `2d` | matches that kick off afterwards run the positional engine |

Set it with the admin world-config endpoint (the same one used for
`tick.match_cadence`). The value is read **once at kickoff** and frozen into the
match, so a match is simulated for its whole life or not at all. Worlds launched
before IM34 have no key and are off until it is set. A live match paced faster
than 2 seconds per minute (lab worlds) is never simulated.

## 3. What is stored

- **Positions are not stored.** They are regenerated on request from the match
  seed, the kickoff snapshot and the event feed.
- **Extra events are stored** in `match.match_events` with `source = 'pitchsim'`
  and sequence numbers from 100000. Matchsim rows keep their numbers and carry
  `source = 'matchsim'`.
- Every row of a simulated match has `offset_millis`, its position inside the
  minute. The feed is ordered by minute, offset, then sequence.
- Passes are not rows; they are counted from the track.
- A quick-played match in a simulated world keeps its kickoff snapshot so it
  can be replayed.

## 4. Watching

- `GET /api/matches/:id/track?from=&to=` returns the movement for a range of
  minutes. It answers 404 for a match played without the simulation.
- The fixture header reports `visual` and `pacing_millis`.
- **Any signed-in user can read any match** (fixture header, feed, track) —
  access goes through one check in the API (`mayViewMatch`), where the planned
  shareable link will be added.
- Live: each `match_tick` carries that minute's movement. The picture therefore
  runs **one step behind** the server (20 seconds at the default cadence), and
  the match page reveals the feed, scoreline and clock from the picture's
  position, so nothing is announced before it is shown.
- Golden-goal extra time arrives in one block and is played back minute by
  minute; the page fetches the block from the track endpoint.
- Live ticks are sent per world. A viewer from another world is kept current by
  the page polling the API once per match minute.
- A finished match is a replay with play/pause, speed and seek.

Pages other than the match page (fixtures, dashboard, news) are not delayed and
can show a result before the picture reaches it.
