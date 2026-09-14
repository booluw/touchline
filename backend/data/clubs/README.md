# Club-name data files

Curated, versioned club-name pools for deterministic AI-club naming (S04-01,
OPD-13 analogue). `cmd/ref-seed` ingests this file into the `ref.club_name_parts`
database table; the database is the **runtime source of truth**, and the file
is the bulk-ingest channel.

Unlike the player-name files there is exactly one file — `clubnames.json` — a
single global pool of name fragments, not one file per region.

## Format

`data/clubs/clubnames.json`:

```json
{
  "version": 1,
  "provenance": {"source": "...", "license": "...", "curated_by": "...", "date": "2026-09-14", "verified": false},
  "stems": ["Athletic", "Olympic", "..."],
  "suffixes": ["FC", "United", "..."]
}
```

- `stems` are the lead words ("Athletic", "Harbour", "Dynamo", ...).
- `suffixes` are the appended words/acronyms ("FC", "United", "Rangers", ...).
  A club name is formed by joining one stem and one suffix ("Harbour United").
- `provenance` mirrors the `data/names` contract; `verified: false` marks the
  list as hand-curated and not yet cross-checked against a published dataset.

## Load contract

- `cmd/ref-seed` loads this file at `-clubdata` (default `data/clubs`), which
  must contain `clubnames.json`.
- A missing file, malformed JSON, missing `provenance.source`/`provenance.license`,
  an empty `stems`/`suffixes` list, or a duplicate entry fails loudly — seeding
  must never run with a silent "Unknown" fallback.
- Stems and suffixes are each deduplicated on load and must be non-empty
  strings.

## Runtime vs. file authority

The `ref.club_name_parts` table IS the runtime authority. `cmd/ref-seed`:
- **UPSERTs** the file's stems/suffixes into the table (existing rows are kept);
- **never deletes** rows, so admin-added fragments from the dashboard
  (`POST/DELETE /api/admin/club-name-parts`) always survive a re-ingest.

This decor is deliberately a couple of running-club conventions reused across
all worlds rather than real commercial club identities.