# A02 — PlayerFactory parameterization

**Status:** Done
**Sprint:** Ad-hoc (player lifecycle)
**Source:** PRD §23, §25; user design session
**Depends on:** S01-04

## What to do

Extend `pkg/playergen` to accept per-call creation options so callers can request age-range, quality band, origin, and academy skew without touching package-level constants.

## Changes

### CreatePlayerOptions struct

```go
type CreatePlayerOptions struct {
    MinAge         int       // default 17
    MaxAge         int       // default 33
    QualityOffset  int       // added to all category means (-20..+20); 0 = baseline
    Nationality    string    // if non-empty, forces this code instead of weighted draw
    AcademyProduct bool      // sets is_academy_product-like flag in output
    Origin         string    // 'generated'|'club_academy'|'street'; propagated by caller
}
```

### CreatePlayerWithOptions(opts CreatePlayerOptions) (*GeneratedPlayer, error)

Delegates to existing internal generation but applies:
- `opts.MinAge`/`opts.MaxAge` (clamped 13..38) in place of fixed `minAge`/`maxAge`.
- `opts.QualityOffset` nudges `categoryMeans` lookup by the given integer.
- `opts.Nationality` overrides `natPool.WeightedRandom`.

### GeneratedPlayer additions

```go
type GeneratedPlayer struct {
    // ... existing fields ...
    Origin         string // 'generated' | 'club_academy' | 'street'
    AcademyProduct bool
}
```

### CreatePlayer backward compat

`CreatePlayer()` unchanged (delegates to `CreatePlayerWithOptions(CreatePlayerOptions{})`). All existing callers unaffected.

### Unit tests

- Test age range 13–15 (street intake).
- Test age range 15–17 (club academy).
- Test quality offset ±10 shifts attribute means.
- Test forced nationality override.
- Confirm `CreatePlayer()` still produces default 17–33 range.

## Acceptance criteria

- ✅ `go test ./pkg/playergen/...` passes (incl. `factory_test.go`).
- ✅ `CreatePlayer()` backward compatible (no existing test changes required beyond the `generateAttributes` arity bump — offset defaults to 0).
- ✅ New options test the full parameter space.

## Delivery evidence

- `factory.go`: `CreatePlayerOptions` + `CreatePlayerWithOptions` added; `CreatePlayer()` delegates to it.
- `generated_player.go`: `GeneratedPlayer` gains `Origin` + `AcademyProduct`.
- `attributes.go`: `generateAttributes` takes `qualityOffset` (clamped to [-20,+20]); means clamped to [10,95]; offset 0 = byte-identical baseline output.
- `attrs := generateAttributes(rng, pos, offset)` applies the offset to every category mean before standard `attrJitterOffset`.
- Tests: determinism parity (`zero-options ≡ CreatePlayer`), street range 13–15, club range 15–17, invalid-range fallback to [17,33], quality offset +15/-15 shift direction, forced nationality, origin/academy propagation, nil-dependency errors. All green.
