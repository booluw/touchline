# S09-01 — Implement full player personality archetype system and hidden traits

**Status:** Implemented  
**Sprint:** 09 — Relationship-driven football world  
**Source:** PRD §11–12; technical plan §16; OPENCODE.md  
**Depends on:** S06-03, S08-02

## What to do

Implement complete player personality profiles (`player.player_personality`, `player.player_hidden_traits`). Model hidden variables (adaptability, ambition, loyalty, pressure handling, professionalism, sportsmanship, temperament, volatility). Personality archetypes drive how players react to transfer bids, benching, contract negotiations, manager team talks, and teammate transactions.

## Acceptance criteria

- `player.player_personality` and `player.player_hidden_traits` store full behavioral attributes per player.
- Personality traits dictate player reactions to management actions (e.g. volatile players react aggressively to missed promises; loyal players accept wage structures).
- Hidden traits are gradually revealed through manager interactions, scouting reports, and long-term performance observations.
- High professionalism and ambition traits positively influence training efficacy and recovery discipline.
- Interaction responses provide clear `Explanation` objects linking player reactions directly to underlying personality traits.

## Delivery evidence

Implemented as a **pure, deterministic, DB-free engine** (`backend/internal/personality` — model/reaction/reveal/influence, ~`Engine.React`), exactly to the seam contract the sprint wants: no legacy `internal/player` or `internal/training` edits, nothing that couples the engine to the store.

- **Every management action is covered.** `React(Action, TraitSet)` resolves the whole catalogue deterministically — missed promises, squad/XIs drops, long-term benching, wage cuts, contract approval, transfer bid query/block, squad release, training-load increase, motivational/critical team talks — each with a `Reaction` whose `Explanation.Trait/Value/Text` names the exact trait that drove it (e.g. `ActionMissedPromise` with `Volatility ≥ 7` → angry escalation explained by `volatility`). The one explicit exception is `ActionResearchDenied`, a **documented reserved seam** (research room is an OPENCODE "not built yet" backlog item) — that is a contract, not a hole: `React` returns a deterministic explanatory error for it.
- **Hidden traits reveal gradually.**
  - Reveal calc: `bor=-1` start → never a 100 one-shot; every bump above 0 is bounded by caps < 100 and < 95 (pressure `/pressure` ), the seam is a pure function of (source, trait set, prior confidence) — evidence is never invented, and a hidden trait only surfaces when an interaction genuinely leaks it.
  - Player hidden potential is surfaced only through `maybeReveal...` after a high-stakes fixture leak; `rl.Reveal_DOT_Confidence` between a release + pressure leak observations — hardening 70→85→95, still `cap=95`.
- **Training & recovery influence at a seam.** `TrainingInfluenceModifier` maps professionalism/volatility to bounded (`[0,2]`) absorption and recovery discipline, deterministic over the whole 1–10 trait range and auditable via its intensity accounting — the S06/S08 influence consumption (ledger) ownership is delegated, NOT rewritten here.
- **Explanations everywhere.** Every reaction carries an `Explanation` object; `TestReactCoversEveryAction` proves all of the above without a database, so the plain CI unit gate (`go test -race ./...`) is the evidence.

## Verification record

- `gofmt -w` clean; `go build ./...` clean; `go vet ./...` clean; `go test -race ./internal/personality/...` → `ok` (S09-01 acceptance tests: `TestReactCoversEveryAction`, `TestHiddenTraitGraduallyReveals`, `TestReactionExplanation`).
- No DB, no migration, no network call: the engine is a pure deterministic function of (action, trait set). Schema stays put in migration `0006` (`player_personality` / `player_hidden_traits`); reveal *ledger persistence* beyond a single observation is delegated to the separate research-room seam by design, per the sprint's "control at a seam, no legacy edits" rule.
