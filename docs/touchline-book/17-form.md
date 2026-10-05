# Chapter 17 — Club form

**Form** is a club's rolling memory of recent performance *relative to
expectation*. It produces the `FormFactor` multiplier the match engine applies
to a team ([Ch. 15](15-match-engine.md)), and a familiar `W-D-L` string for
read models.

## 17.1 The model (matchsim addendum v1.2 §2.2, approved)

One row per club in `club.form_state`:

```
quality  = clamp(1 + (actualGD − expectedGD) / ExpectedGDDivisor, 0.5, 1.5)
current  = clamp((1 − α) · previous + α · quality, 0.85, 1.15)
```

| Constant | Value | Status |
| --- | --- | --- |
| `AlphaDefault` (α) | 0.2 | approved |
| `MinRating` / `MaxRating` | 0.85 / 1.15 | approved |
| `ExpectedGDDivisor` | 4.0 | proposal |

- **Expected GD** mirrors the engine's own expectation (goal weights incl. home
  advantage). It is naturally fractional, so `internal/match` uses
  `ResultQualityDelta` (continuous) — rounding to an integer would erase most
  matches' signal.
- Quality ≈ 1.0 means "performed exactly as expected", so form **drifts back
  toward 1.0** whenever results normalise: runs and slumps emerge over weeks
  without ever dominating squad quality.
- A new club (or missing row) is **neutral**: rating 1.0.
- `LastUpdatedTick` mirrors `world.worlds.current_tick` at write time so the
  same save replays the same form timeline.

## 17.2 The form string

`FormStringFromResults` renders up to the last 5 results oldest-first joined by
`-`, padded with `-` for missing slots (e.g. `W-W-D-L-W`, or `-` placeholders
early on). Stored on the same row.

## 17.3 Where form appears

| Consumer | Uses |
| --- | --- |
| match engine | `Team.FormFactor` |
| job offers | `form` block `{rating, form_string}`, omitted until the club has played ([Ch. 22](22-managers-and-job-offers.md)) |
| scouting dossiers | trailing-5 string + EWMA rating ([Ch. 26](26-dashboard-news-scouting-realtime.md)) |

Form is distinct from:

- **morale** (players, [Ch. 18](18-morale-and-transfer-requests.md)),
- **supporter sentiment** (fans, [Ch. 9](09-clubs-dna-supporters.md)),
- **board match ratings** (manager, [Ch. 23](23-board-and-job-security.md)).

## Connections

- Updated in the match-completion transaction: [Chapter 16](16-matchday-and-live-matches.md).
- Code: `internal/form/{form,store}.go`.
- Source: matchsim addendum v1.2 §2.2, glossary §4.

---
[← Matchday](16-matchday-and-live-matches.md) · [Contents](the-touchline-book.md) · [Next: Morale →](18-morale-and-transfer-requests.md)
