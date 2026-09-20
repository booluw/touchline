# Club-name data files

Curated, versioned club-name pools for deterministic AI-club naming (S04-01,
OPD-13 analogue). `cmd/ref-seed` ingests this file into the `ref.club_name_parts`
database table; the database is the **runtime source of truth**, and the file
is the bulk-ingest channel.

There is one global file — `clubnames.json` — a single fallback pool of name
fragments usable by any country, plus one optional **country-scoped pool per
region** in `regional/{code}.json` that makes seeded clubs feel at home in
their country.

## Format

`data/clubs/clubnames.json` (global fallback):

```json
{
  "version": 1,
  "provenance": {"source": "...", "license": "...", "curated_by": "...", "date": "2026-09-21", "verified": false},
  "stems": ["Athletic", "Harbour", "Dynamo", "..."],
  "suffixes": ["FC", "United", "Rangers", "..."]
}
```

- `stems` are the lead words ("Athletic", "Harbour", "Dynamo", ...).
- `suffixes` are the appended words/acronyms ("FC", "United", "Rangers", ...).
  A club name is formed by joining one stem and one suffix ("Harbour United").
- `provenance` mirrors the `data/names` contract; `verified: false` marks the
  list as hand-curated and not yet cross-checked against a published dataset.

### Regional pools (`regional/{code}.json`)

`regional/` holds the same `{version, provenance, stems, suffixes}` schema, one
file per country code (the code is the file name, lowercase letters/digits,
matching `ref.nationalities` codes). A country with a regional pool draws its
AI-club names from it; countries without one fall back to the global file. The
pools deliberately use regional running-club *conventions* ("Villa", "CF",
"Grêmio", "SC", "Kickers", ...) and fictional identity words rather than real
commercial clubs.

Codes present today: `ar, be, br, ci, co, de, es, fr, gb, gh, hr, it, jp, kr,
mx, ng, nl, pt, sco, us, uy`. Add a country's flavor by dropping in a new
`regional/{code}.json` and re-running `cmd/ref-seed`.

## Load contract

- `cmd/ref-seed` loads `clubnames.json` (global) plus every `regional/{code}.json`
  from `-clubdata` (default `data/clubs`).
- A missing file, malformed JSON, missing `provenance.source`/`provenance.license`,
  an empty `stems`/`suffixes` list, or a duplicate entry fails loudly — seeding
  must never run with a silent "Unknown" fallback.
- Stems and suffixes are each deduplicated on load and must be non-empty
  strings. A missing `regional/` directory (or an empty region set) is fine:
  the global pool is the fallback.

## Runtime vs. file authority

The `ref.club_name_parts` table IS the runtime authority. `country_code` on
each row scopes the pool: `''` is the global pool, a 2–4 letter code is a
per-country pool. `cmd/ref-seed`:
- **UPSERTs** the file's stems/suffixes into the table, per code (existing
  rows are kept);
- **never deletes** rows, so admin-added fragments from the dashboard
  (`POST/DELETE /api/admin/club-name-parts`) always survive a re-ingest.

This decor is deliberately a couple of running-club conventions reused across
all worlds rather than real commercial club identities.