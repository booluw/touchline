# pitchsim — positional match engine (IM34)

`pkg/pitchsim` turns a match that `pkg/matchsim` has already decided into
movement: where the ball and the 22 players are, every five match seconds. It is
pure and deterministic (no database, network or clock) and it **never decides
anything**.

## Contract

1. **matchsim is the result.** Score, events, player ratings and injuries come
   from `matchsim` alone. `pitchsim` takes those events as input and stages
   them: a goal by player X in minute 37 is a move that ends with X scoring in
   minute 37.
2. **Superset.** `pitchsim` adds positions and extra events (`shot`, `save`,
   `tackle`, `foul`, `corner`, `offside`). None of them changes a score. Passes
   are counted per minute in the track, not emitted as events.
3. **Every matchsim event is staged exactly once** (one cue in its minute), in
   matchsim's order. Nothing is dropped or invented among matchsim's types.
4. **A minute never changes.** The output for minute *m* depends only on the
   seed, the two line-ups and the events up to *m*. Generating a longer match,
   or regenerating later, gives the same minute. Live pacing relies on this: the
   extra events stored during the match equal a later regeneration.
5. **Own RNG stream**, seeded from the match seed and a tag, so matchsim's draw
   order is untouched.

`Version` must be bumped whenever generated output changes; stored extra events
belong to one version.

## Track format

`Generate(Input) Track` returns minutes `1..Input.Minutes`. Per minute:

| Field | Meaning |
| --- | --- |
| `lineup` | player id per slot at the start of the minute (home 0-10, away 11-21); empty = nobody |
| `frames` | 12 keyframes, `t` = 0, 5000 … 55000 ms; `b` = ball `[x, y, height]`; `p` = `[x, y]` per slot, `[-1,-1]` = off the pitch |
| `cues` | `{seq, t}` for every event in the minute, both engines, by feed sequence |
| `passes` | completed passes, home then away |

Coordinates are integers `0..1000`: x from goal line to goal line, y from
touchline to touchline. Home attacks towards x = 1000 in the first half; frames
after minute 45 (including extra time) are mirrored because the teams change
ends. Renderers interpolate linearly between keyframes.

Extra events carry sequences from `ExtraSequenceBase` (100000), so matchsim's
sequence numbers are never disturbed.

## Model

- Each starter stands at a base position for their role, mirrored for the away
  side; a substitute takes over the slot of the player replaced.
- The whole block slides with the ball; the side in possession pushes up, the
  other drops off, and the nearest opponent closes the ball down.
- Open play is a chain of passes, preferring team-mates ahead. Each frame the
  side on the ball may lose it; the home side's share of the ball is fixed for
  the match from the kickoff ratings (not from the result).
- An attack that reaches the final third may end in a shot wide, a save, a
  corner or an offside.
- The two frames before a goal, chance or penalty belong to the attacking side,
  and the last pass goes to the recorded assister.
- A card follows a foul by the booked player. A sent-off player leaves from the
  next minute; a substitute appears from the next minute.

## Known limits

- Ball height is always 0 (the field exists for the 3D view).
- The ball share is a kickoff estimate, so pass share will not match matchsim's
  possession figure.
- A live tactic change does not reshape the team.
- Second-half mirroring also applies to extra time (no further change of ends).
