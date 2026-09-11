# Name data provenance

Each `data/names/<code>.json` file carries a `provenance` block. This document
summarizes how the current (S01-04) curation was produced and what still needs
verification before the lists are treated as final.

## Status of the current lists

- All 21 lists were **hand-curated by the Touchline data-engineering task
  (S01-04)** from the curator's general knowledge of common given names and
  surnames in each country/region, aiming for the names an actual football fan
  would recognize as typical first-choice names in that culture.
- Every file has `provenance.verified: false`. Name lists are **facts** (names
  are not copyrightable in the main territories), so inclusion in this repo is
  safe today, but the *selection* has not been cross-checked against a
  published top-N frequency list.
- **Before the game is exposed to real users (release/phase-2), each list must
  be replaced or reconciled with an authoritative open dataset** (examples
  below), and each file's `provenance` block updated with the real source,
  license, and `verified: true`.

## Realistic open sources to adopt at that point

- Brazil (IBGE census names), France (INSEE first-name data), Spain/Argentina/
  Colombia/Uruguay/Mexico (RENIEC/registro-civil/INE family-name lists),
  England/Scotland/United States (Office for National Statistics + US Census
  surname data), Germany (Gesamtverzeichnis der Vornamen/typical surname deplpy),
  Netherlands (Meertens Instituut), Belgium (Familles de Belgique), Japan/Korea
  (ranked given-name + surname lists), Ghana/Nigeria/Ivory Coast (registry/
  newspaper byline frequency studies), Italy (ISTAT/cognomi italiani), Portugal
  (SPIE name survey), Croatia (MUP/Croatian Bureau of Statistics surname lists).
- Licensing must be checked per dataset before inclusion; prefer public-domain
  census/civil-registry aggregates.

## Rules

- No celebrity-only names; lists are everyday common names (a handful may
  coincide with famous people — that is unavoidable and fine).
- No fictional/invented names.
- Diacritics are preserved where the source culture writes them (e.g. French,
  Spanish, Portuguese); transliteration is used for Turkish-influenced and
  East-Asian cultures (e.g. `ă` handled via plain ASCII in this curation —
  flagged for the verification pass).
- `generation_weight` values are relative, tunable data — not facts — and can
  be adjusted in the JSON without code changes.