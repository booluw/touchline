# S01-04 — Deliver nationality-aware player generation and seed data

**Status:** Done  
**Owner:** opencode agent (S01-04)  
**Sprint:** 01 — World foundation and event spine  
**Source:** Technical plan §7, §16; OPENCODE.md  
**Depends on:** S01-01  
**Resolution:** OPD-13 (data home, coverage, and player-persistence timing) — see `docs/product_manager.md`.

## What to do

Complete the standalone player-generation subsystem with curated, versioned per-nationality name data and weighted nationality selection. Use it to seed believable players without licensing real players.

## Acceptance criteria

- Curated name datasets are added for the agreed nationality coverage; provenance/licensing for each source is documented before inclusion.
- Generator selection uses `world.nationality_pool` weights and the seeded random source.
- Generated names follow the supported nationality/culture rules and collision handling prevents duplicate first/last-name pairs within a world.
- Player generation remains independent of domain DB code and is unit-tested, including deterministic seeded output.
- World seeding can create a squad of generated players and persist their nationality/name data.

## Scope decision (product)

Per product decision (recorded as OPD-13), **players are not persisted in S01-04**. Generated players become `person.people` + `player.players` rows only when a team is created or the first season starts (S02-02 / S03-01), guaranteeing fresh-slate worlds. AC-5's persistence leg therefore lands in that later task; S01-04 delivers the complete generation subsystem and reference data it will call.

## Delivery evidence

1. **`pkg/playergen` rewritten** — DB-free subsystem with seeded RNG (injected `*rand.Rand`; global `rand.Seed` removed), real weighted selection (`NationalityPool.WeightedRandom`), rotation/collision handling (`NameRegistry`, `PlayerFactory` regenerate-on-collision with attempt cap), and a `GeneratedPlayer` output contract (first/last/display name, nationality code, seeded age, primary position). `LoadNameData` validates and loads `data/names/*.json` (codes, non-empty lists, provenance, positive weights); missing/malformed files are load errors — the old silent "Unknown" fallback is removed. Reference: `backend/pkg/playergen/{generator,nationality,poolgen,factory,registry,generated_player,load}.go` (`filebacked.go` deleted).
2. **21 curated nationality files** `backend/data/names/<code>.json` (br, ar, fr, ng, en, es, de, it, pt, nl, be, co, gh, uy, mx, us, hr, jp, kr, sco, ci) with `generation_weight`, provenance blocks, and first/last name pools; `backend/data/names/README.md` (schema, coverage/weights table, code convention incl. `eng`/`sco`) and `PROVENANCE.md` (curation status + authoritative open datasets to adopt before release).
3. **Reference-table seeding** `backend/cmd/ref-seed` (`main.go`) — idempotent ingestion into `ref.nationalities` (upsert) + `ref.name_pool` (delete+reinsert per code, one transaction). **Reference data only; zero game-entity writes.** Wired as compose service (`infra/docker/Dockerfile.ref-seed` + `docker-compose.yml` `ref-seed` after `migrations`) and a CI idempotency check in the `migrations` job (`.github/workflows/ci.yml`) running ref-seed twice and asserting identical output (`ref.nationalities=21 `).
4. **Unit tests** `backend/pkg/playergen/generator_test.go` — same-seed determinism, different-seed divergence, weighted distribution within tolerance (200k draws), zero-weight skipping, collision avoidance + exhaustion errors, registry reserve/release, and a full-data validation test over the shipped 21 files.
5. **Verification (local PG14 cluster, toolchain go 1.25):** `go vet ./...`, `go build ./...`, `go test ./...` and `go test -race ./...` all pass; `cmd/ref-seed` run twice produced identical `seeded ref.nationalities=21 ref.name_pool=2301 (first=1020 last=1281)`; DB confirmed — `ref.nationalities`=21, `ref.name_pool`=2301, `person.people`=0, `player.players`=0.