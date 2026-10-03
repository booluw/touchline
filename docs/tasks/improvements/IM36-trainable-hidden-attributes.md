# IM36 — Make hidden attributes trainable

**Status:** Not started
**Owner:** —
**Sprint:** Improvements (player development)
**Source:** Product request (2026-10-02), follow-up to IM35. The hidden
attributes exposed in IM35 should be trainable.
**Depends on:** IM35 (exposure, OPD-59), the weekly development pass
(`backend/internal/training/dev_pass.go`), the personality model
(`backend/internal/personality`).

## What to do

Today the only code that writes to `player.player_hidden_traits` is the dev
pass, and it only touches `potential` / `potential_ceiling_locked`. Add a
mechanism that moves `professionalism`, `temperament` and `adaptability`.

Open questions to settle before implementation:

1. **What drives the change?** FM moves these traits mainly through mentoring
   (a senior player with a stronger trait pulls a younger one), not drills.
   Options: a mentoring pairing, a training focus, or both. Adaptability in FM
   moves with time spent at a foreign club rather than with training.
2. **Cadence and size:** weekly in the dev pass, capped per season? Age-gated
   (FM: mostly under 24)?
3. **Visible duplicates:** `player.player_personality` also has
   `professionalism` and `adaptability`. Should they stay independent, or
   should one feed the other?
4. **Engine effects:** `squad.go` already reads hidden professionalism and
   temperament. Confirm that a change mid-season is acceptable to match and
   morale numerics.
5. **Observability:** record changes in `player.player_attribute_changes` (with
   a `hidden:` key prefix?) so the development read can show them.

## Recorded decisions

None yet.
