# TGN REL Ingest (Matching-Focused)

This package loads the selected Getty TGN REL files into PostgreSQL for place-name to TGN ID matching.

## Included source files

- `SUBJECT.out`
- `TERM.out`
- `LANGUAGE_RELS.out`
- `SUBJECT_RELS.out`
- `SUBJECT_MERGE.out`
- `PTYPE_ROLE.out`
- `PTYPE_ROLE_RELS.out`
- `COORDINATES.out`

## What gets created

- Stage schema: `tgn_stage`
- Final schema: `tgn`
- Run tracking table: `tgn.import_run`
- Matching function: `tgn.match_place_name(name, context_parent_id, context_country_id, limit_n)`

## Requirements

- PostgreSQL with permissions to create schemas, tables, functions, and extensions.
- `psql` available in `PATH`.
- Python 3.10+ (standard library only).

## Run

```bash
export DATABASE_URL='postgresql://user:pass@localhost:5432/mydb'
./scripts/tgn_ingest.py --input-dir ./tgn_rel_0126 --release-id tgn_rel_0126
```

Optional flags:

- `--db-url` to pass DB URL directly
- `--psql-bin` for non-default `psql` path
- `--keep-temp` to persist canonicalized TSV files

## Example query

```sql
SELECT *
FROM tgn.match_place_name('Alexandria', NULL, NULL, 10);
```

With context boost:

```sql
SELECT *
FROM tgn.match_place_name('Alexandria', 7007567, NULL, 10);
```

## Notes

- Full reload behavior: each run truncates stage and final tables before load.
- `TERM.out` has a known embedded-tab anomaly; parser canonicalizes it to 13 columns.
- Coordinates are loaded raw plus derived decimal lat/lon columns.
- Schema creation includes helper indexes for the matcher's repeated preferred-term,
  parent-selection, and place-type lookup subqueries.
- For an existing loaded database, `psql "$DATABASE_URL" -f sql/15_perf_indexes.sql`
  applies the same helper indexes with `CREATE INDEX CONCURRENTLY` and refreshes stats.
  The target database must already contain the loaded `tgn` schema.
