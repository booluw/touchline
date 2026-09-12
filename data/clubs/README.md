# Club name data

Curated, versioned pools for deterministic AI-club naming during S04-01 league
seeding. A generated club gets a `stem` + `suffix` name (e.g. "Imperial Rovers")
drawn from `ref.club_name_parts`; the pools live at the file level here as a
**bulk-ingest channel**, and the database is the **runtime source of truth**.

## Governance (important)

- `cmd/ref-seed` **upserts** into `ref.club_name_parts` — it never deletes
  rows. Administrator additions made through the admin dashboard (which write
  the same table) therefore survive any re-ingest.
- Adding an entry is a pure **data change** in either direction: append it to
  the JSON below and re-run ref-seed for a bulk load, or create it at runtime
  via `POST /api/admin/club-name-parts`. Removing an entry is an admin API
  call (`DELETE /api/admin/club-name-parts/:kind/:value`) or manual SQL —
  removing it from this JSON alone will **not** remove it from the DB.
- This intentionally diverges from `data/names` (player names), where the JSON
  files are authoritative and re-ingest is delete+reinsert. Club-name parts
  are user-extensible at runtime, so rows must survive re-ingest.

## Format

`data/clubs/clubnames.json`:

```json
{
  "version": 1,
  "provenance": {
    "source": "…where the list came from…",
    "license": "…",
    "curated_by": "…",
    "date": "2026-09-12",
    "verified": false
  },
  "stems": ["Athletic", "Olympique", "…"],
  "suffixes": ["FC", "United", "…"]
}
```

- `stems` and `suffixes` are the name-building pools. Lists are deduplicated on
  load; an empty list is a load error so seeding can never silently run dry.
- `provenance` records where each list came from; `verified: false` marks
  lists still awaiting cross-checking.
- Loader contract is enforced by `cmd/ref-seed`'s `LoadClubNames` (reads only
  `clubnames.json`; malformed files, missing provenance, or empty lists fail
  loudly).

## Determinism note

`nextClubName` draws with `Intn(len(pool))`, so **extending the pool shifts
the draw distribution for future seeds** — that is the point of making these
options data-driven. An already-seeded world is immutable, and identical
data + seed reproduce an identical competition (same guarantee as the player
name pools).