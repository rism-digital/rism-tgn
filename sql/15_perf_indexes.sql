-- Online-safe helper indexes for matcher latency on an already-loaded database.
-- Run with psql outside an explicit transaction block.
\set ON_ERROR_STOP on

SELECT current_database() AS current_db \gset
SELECT EXISTS (
    SELECT 1
    FROM information_schema.schemata
    WHERE schema_name = 'tgn'
) AS has_tgn_schema \gset

\if :has_tgn_schema
\echo Applying performance indexes to database :current_db
\else
\echo Database :current_db does not contain schema "tgn".
\echo Run the ingest against this database first, or connect to the database that already has the loaded TGN schemas.
\quit 3
\endif

CREATE INDEX CONCURRENTLY IF NOT EXISTS term_best_by_subject_idx
    ON tgn.term (
        subject_id,
        (CASE WHEN btrim(COALESCE(term_type, '')) = 'P' THEN 0 ELSE 1 END),
        display_order,
        term_id
    )
    INCLUDE (term_text, term_norm);

CREATE INDEX CONCURRENTLY IF NOT EXISTS subject_rels_parent_choice_idx
    ON tgn.subject_rels (
        child_subject_id,
        (CASE WHEN btrim(COALESCE(historic_flag, '')) = 'H' THEN 1 ELSE 0 END),
        (CASE WHEN btrim(COALESCE(hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END),
        (CASE WHEN btrim(COALESCE(preferred_flag, '')) = 'P' THEN 0 ELSE 1 END),
        subject_rel_id
    )
    INCLUDE (parent_subject_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS place_type_rels_best_by_subject_idx
    ON tgn.place_type_rels (
        subject_id,
        (CASE WHEN btrim(COALESCE(preferred_flag, '')) = 'P' THEN 0 ELSE 1 END),
        rel_order,
        place_type_id
    );

ANALYZE tgn.term;
ANALYZE tgn.subject_rels;
ANALYZE tgn.place_type_rels;
