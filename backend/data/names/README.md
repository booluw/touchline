# Player name data files

Curated, versioned name-frequency data per nationality. **This is a data engineering task** (per the technical implementation plan §7). The JSON files are the record-of-record for the global player pool: `pkg/playergen` consumes them directly (unit tests, and the future team-creation flow via an in-memory loader), and `cmd/ref-seed` ingests them into the database reference tables (`ref.nationalities`, `ref.name_pool`).

## Format

One file per nationality: `data/names/<code>.json`, where `<code>` is a lowercase 2–3 letter slug (`br`, `ng`, `en`, …).

```json
{
  "nationality": {"code": "ng", "name": "Nigeria", "generation_weight": 8},
  "provenance": {
    "source": "…dataset or reference list used…",
    "license": "…",
    "curated_by": "…",
    "date": "2026-09-11",
    "verified": false
  },
  "first_names": ["Chinedu", "Emeka", ...],
  "last_names": ["Okafor", "Balogun", ...]
}
```

- `nationality.code` must be a unique lowercase 2–3 letter slug. Royal/Home-nation codes (`eng`, `sco`) are not ISO 3166-1 alpha-2 values — they are project-reserved slugs (see `ref.nationalities.code` comment and OPD-13).
- `generation_weight` is the relative share of that nationality in the global player pool (positive number; tuning is a data change, not code).
- `provenance` records where each list came from. `verified: false` marks lists that still need cross-checking against an authoritative open dataset.
- `first_names` / `last_names` are the given-name and surname pools. Lists are deduplicated on load; empty lists are a load error so generation can never silently return "Unknown".

## Coverage (21 nationalities)

| code | nationality   | weight | culture rule |
|------|---------------|--------|--------------|
| br   | Brazil        | 10.0   | first + last (single-name shirt names deferred) |
| ar   | Argentina     |  8.5   | first + last |
| fr   | France        |  8.0   | first + last |
| ng   | Nigeria       |  8.0   | first + last |
| en   | England       |  7.5   | first + last |
| es   | Spain         |  7.0   | first + last |
| de   | Germany       |  6.5   | first + last |
| it   | Italy         |  6.0   | first + last |
| pt   | Portugal      |  6.0   | first + last |
| nl   | Netherlands   |  5.5   | first + last |
| be   | Belgium       |  5.0   | first + last (mixed Flemish/Walloon) |
| co   | Colombia      |  5.0   | first + last |
| gh   | Ghana         |  4.5   | first + last |
| uy   | Uruguay       |  4.0   | first + last |
| mx   | Mexico        |  4.0   | first + last |
| us   | United States |  3.5   | first + last |
| hr   | Croatia       |  3.5   | first + last |
| jp   | Japan         |  3.0   | first + last (given-name first for our model; cultural ordering deferred) |
| kr   | South Korea   |  3.0   | first + last (given-name first for our model; cultural ordering deferred) |
| sco  | Scotland      |  2.5   | first + last |
| ci   | Ivory Coast   |  3.0   | first + last |

## File naming / loader contract

- `pkg/playergen.LoadNameData(dataDir)` reads only files matching `<code>.json` (lowercase 2–3 letters). Any other file (documentation, `*.example.json`) is ignored, so stray placeholders can never be loaded.
- A malformed file, an invalid code, an empty list, or a missing provenance block fails the load — coverage gaps fail loudly.

## Guidelines

- Prefer open, real-world name-frequency datasets over invented names — the world should "feel" like football. See `PROVENANCE.md` for what is verified today and what still needs cross-checking.
- Enforce the `(first_name, last_name)` collision check at generation time (regenerate on collision within a nationality pool). This is done by `playergen.NameRegistry` in the team-creation flow.
- Deferred (later version): patronymic/single-name cultures (`shirt_name` conventions such as Brazilian single-name shirts), and family-name-first ordering for East-Asian cultures.