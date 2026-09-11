# Player name data files

Curated, versioned name-frequency data per nationality/culture. **This is a data engineering task** (per the technical implementation plan §7), not a code task — budget time to source open name-frequency datasets for each country.

## Format

One file per nationality: `data/names/<nationality>.json`

```json
{
  "first_names": ["Ade", "Chinedu", "Emeka", ...],
  "last_names": ["Adeyemi", "Okafor", "Balogun", ...]
}
```

The `pkg/playergen.FileBackedGenerator` loads these files at startup from the path passed to `NewFileBackedGenerator(dataDir)`.

## File naming

Use the same keys that `pkg/playergen/factory.go`'s `NationalityPool` references (e.g. `nigeria`, `brazil`, `argentina`, `france`, `england`, `spain`, `germany`, `italy`, `portugal`, `netherlands`, `belgium`, `japan`, `south_korea`, `usa`, `mexico`).

## Guidelines

- Prefer open, real-world name-frequency datasets over invented names — the world should "feel" like football.
- Enforce the `(first_name, last_name)` collision check at generation time (regenerate on collision within a nationality pool).
- Equipment: some cultures use patronymic/single-name styles; include a `shirt_name` convention where relevant (e.g. Brazilian single-name shirt names) in a later version.