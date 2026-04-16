# AGENTS.md

## Purpose
This repository contains a Getty TGN REL dump and a PostgreSQL ingest pipeline focused on place-name matching (`name -> TGN id`), with coordinates, ancestors, and place type labels.

## Repo Layout
- `*.out`: Getty source dump files.
- `scripts/tgn_ingest.py`: End-to-end loader CLI.
- `sql/10_schema.sql`: Extensions, schemas, stage/final tables, helper functions, indexes.
- `sql/20_load.sql`: Full-reload transforms from `tgn_stage` to `tgn`.
- `sql/30_match.sql`: `tgn.match_place_name(...)` and `tgn.get_place_by_id(...)` functions.
- `sql/40_validate.sql`: Post-load sanity checks.
- `README_INGEST.md`: operator runbook.

## What The Pipeline Imports
Current ingest includes:
- `SUBJECT.out`
- `TERM.out`
- `LANGUAGE_RELS.out`
- `SUBJECT_RELS.out`
- `SUBJECT_MERGE.out`
- `PTYPE_ROLE.out`
- `PTYPE_ROLE_RELS.out`
- `COORDINATES.out`

## Canonical Load Flow
1. `sql/10_schema.sql`
2. Stage truncate + `\copy` into `tgn_stage.*`
3. `sql/20_load.sql`
4. `sql/30_match.sql`
5. `sql/40_validate.sql`

`./scripts/tgn_ingest.py --input-dir ./tgn_rel_0126 --release-id <id>` runs the above (with run metadata in `tgn.import_run`).

## Data Quirks Already Handled
- `SUBJECT.out` has duplicate `subject_id` rows.
  - Load uses deterministic `DISTINCT ON (subject_id)`.
- `SUBJECT_MERGE.out` has duplicate triples.
  - Load uses `SELECT DISTINCT`.
- `TERM.out` contains an embedded-tab anomaly.
  - Parser canonicalizes TERM rows to 13 columns.
- Some `TERM`/`COORDINATES` rows reference subject IDs not present in `SUBJECT.out`.
  - Loader inserts placeholder `tgn.subject` rows (`record_type='MISSING_SUBJECT'`) before FK creation.
- Place type label is **not** reliably from `subject.place_type_id`.
  - Correct mapping comes from `PTYPE_ROLE_RELS` (`subject_id -> place_type_id`) joined to `PTYPE_ROLE` labels.

## Match Function Contract
Function: `tgn.match_place_name(in_name text, context_parent_id bigint, context_country_id bigint, limit_n integer)`

Returns:
- `tgn_id`
- `matched_term`
- `preferred_term`
- `place_type_id`
- `place_type_label`
- `score`
- `parent_subject_id`
- `lat`, `lon`
- `ancestor_pairs` as tuple-like JSON arrays: `[[id, "label"], ...]` (traversal stops before ancestor `7029392`)

Function: `tgn.get_place_by_id(in_id bigint)`

Returns:
- Same payload columns as `match_place_name`, with `score = NULL` for deterministic ID lookups.

Notes:
- Function script starts with:
  - `DROP FUNCTION IF EXISTS tgn.match_place_name(text, bigint, bigint, integer);`
  - Needed because OUT-column changes cannot use plain `CREATE OR REPLACE`.

## Performance Notes
Important indexes are created in `sql/10_schema.sql`, including:
- `tgn.term(term_norm)` and trigram GIN on `term_norm`
- `tgn.search_term(matched_term_norm)` and trigram/full-text indexes for the query hot path
- `tgn.search_term_index(matched_term_norm)` as the narrow candidate table for the query hot path
- Preferred-term and hierarchy helper indexes
- Coordinates first-row lookup index
- Place-type relation index (`tgn.place_type_rels(subject_id, preferred_flag, rel_order, place_type_id)`)

`sql/20_load.sql` also rebuilds `tgn.search_term` and runs `ANALYZE` on major tables at the end.

## Safe Edit Rules For Future Agents
- Keep ingest idempotent for full reloads.
- Prefer fixing parser/load SQL over manual data edits.
- If changing `match_place_name` output columns, keep `DROP FUNCTION IF EXISTS ...` at top.
- If adding new source files, update all of:
  - `scripts/tgn_ingest.py` (`TABLE_SPECS`, stage truncate list)
  - `sql/10_schema.sql` (new stage/final tables/indexes)
  - `sql/20_load.sql` (transform/load + analyze)
  - `README_INGEST.md` and `sql/40_validate.sql`

## Quick Ops Commands
- Run full ingest:
  - `./scripts/tgn_ingest.py --input-dir ./tgn_rel_0126 --release-id tgn_rel_0126`
- Refresh matcher only:
  - `psql "$DATABASE_URL" -f sql/30_match.sql`
- Check top match:
  - `SELECT * FROM tgn.match_place_name('Genf', NULL, NULL, 10) ORDER BY score DESC LIMIT 1;`
