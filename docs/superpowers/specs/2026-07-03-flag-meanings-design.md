# Translate CF flag values to flag_meanings via Grafana value mappings

**Date:** 2026-07-03
**Status:** Approved

## Problem

QARTOD quality-flag variables (e.g. `navd88_meters_qartod_gross_range_test`,
`qartod_qc_rollup` on `Hohonu_tide_Boothbay_Harbor_ME`) come back from ERDDAP
tabledap as bare integers (1, 2, 3, 4, 9). Their meanings live in the dataset
metadata as the CF-convention attribute pair:

- `flag_values` — comma-separated numeric list, e.g. `1, 2, 3, 4, 9`
- `flag_meanings` — space-separated token list, e.g. `GOOD UNKNOWN SUSPECT FAIL MISSING`

Panels currently show raw numbers, which are meaningless without the lookup.

## Decision

For every queried variable that declares **both** `flag_values` and
`flag_meanings`, the Go backend attaches Grafana **value mappings** to that
field's `FieldConfig`. Detection is attribute-driven (the CF convention pair),
not name-based — no QARTOD variable names are hardcoded.

Data stays numeric, so time-series panels still plot; tables, stat panels,
state timelines, and tooltips display the mapped text and color. Users can
still override mappings in panel config (backend mappings are field-config
defaults).

Rejected alternatives:

- **Convert to string columns** — breaks numeric plotting; loses information.
- **Parallel `<name>_meaning` string fields** — doubles columns, clutters panels.

## Metadata fetch & cache

- `handleQuery` fetches `{base}/info/{datasetID}/index.json`. Rows have the
  shape `[rowType, variableName, attributeName, dataType, value]`; parse into
  `map[variableName]data.ValueMappings` keeping only variables with both flag
  attributes.
- Cache per `Datasource` instance, keyed by dataset ID, TTL 1 hour, mutex
  guarded. Metadata changes rarely; the cache dies with the instance when
  datasource settings change. Failures are not cached (retried on the next
  query).

## Mapping construction

- Zip `flag_values` (split on commas, trimmed) with `flag_meanings` (split on
  whitespace) into a `data.ValueMapper` (`value string → {Text, Color, Index}`).
  `Index` preserves list order for UI display.
- If the two lists have different lengths, skip that variable (malformed
  metadata — better no mapping than a wrong one).
- Colors by case-insensitive meaning token; unrecognized tokens get text-only
  mappings:
  - `GOOD`, `PASS` → green
  - `SUSPECT` (and `SUSPECT_OR_OF_HIGH_INTEREST`) → orange
  - `FAIL` → red
  - `UNKNOWN`, `NOT_EVALUATED` → grey
  - `MISSING` → dark grey

## Error handling — fail soft

Any metadata problem (HTTP error, timeout, parse failure, absent attributes)
logs a warning and the query proceeds exactly as today with no mappings. A
metadata hiccup never fails a working dashboard query.

## Wiring

- `parseTableJSON` gains a mappings parameter; mappings merge into the
  existing per-field `Config` alongside the `suffix:` unit (a field may have
  both).
- No frontend, query model, or `plugin.json` changes — no Grafana restart
  needed beyond the normal backend rebuild.

## Testing

- Unit tests: info-response parsing (happy path, mismatched list lengths,
  absent attributes, non-200), mapping/color construction, cache TTL, and
  fail-soft on metadata errors.
- Extend the existing fake-server tests to also serve the `/info/` endpoint
  and assert mappings land on the right fields.
