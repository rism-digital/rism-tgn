CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE SCHEMA IF NOT EXISTS tgn_stage;
CREATE SCHEMA IF NOT EXISTS tgn;

CREATE TABLE IF NOT EXISTS tgn.import_run (
    run_id bigint PRIMARY KEY,
    release_id text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    status text NOT NULL,
    parser_stats jsonb NOT NULL DEFAULT '{}'::jsonb,
    load_stats jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_message text
);

CREATE TABLE IF NOT EXISTS tgn_stage.subject_raw (
    c1 text, c2 text, c3 text, c4 text, c5 text, c6 text, c7 text
);

CREATE TABLE IF NOT EXISTS tgn_stage.term_raw (
    c1 text, c2 text, c3 text, c4 text, c5 text, c6 text, c7 text,
    c8 text, c9 text, c10 text, c11 text, c12 text, c13 text
);

CREATE TABLE IF NOT EXISTS tgn_stage.language_rels_raw (
    c1 text, c2 text, c3 text, c4 text, c5 text, c6 text, c7 text, c8 text
);

CREATE TABLE IF NOT EXISTS tgn_stage.subject_rels_raw (
    c1 text, c2 text, c3 text, c4 text, c5 text, c6 text, c7 text, c8 text, c9 text
);

CREATE TABLE IF NOT EXISTS tgn_stage.subject_merge_raw (
    c1 text, c2 text, c3 text
);

CREATE TABLE IF NOT EXISTS tgn_stage.ptype_role_raw (
    c1 text, c2 text
);

CREATE TABLE IF NOT EXISTS tgn_stage.ptype_role_rels_raw (
    c1 text, c2 text, c3 text, c4 text, c5 text, c6 text, c7 text, c8 text
);

CREATE TABLE IF NOT EXISTS tgn_stage.coordinates_raw (
    c1 text, c2 text, c3 text, c4 text, c5 text, c6 text, c7 text, c8 text, c9 text,
    c10 text, c11 text, c12 text, c13 text, c14 text, c15 text, c16 text, c17 text,
    c18 text, c19 text, c20 text, c21 text, c22 text, c23 text, c24 text, c25 text,
    c26 text, c27 text, c28 text, c29 text, c30 text, c31 text, c32 text, c33 text,
    c34 text
);

CREATE OR REPLACE FUNCTION tgn.to_bigint(v text)
RETURNS bigint
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE
        WHEN v IS NULL OR btrim(v) = '' THEN NULL
        WHEN btrim(v) ~ '^-?[0-9]+$' THEN btrim(v)::bigint
        ELSE NULL
    END;
$$;

CREATE OR REPLACE FUNCTION tgn.to_int(v text)
RETURNS integer
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE
        WHEN v IS NULL OR btrim(v) = '' THEN NULL
        WHEN btrim(v) ~ '^-?[0-9]+$' THEN btrim(v)::integer
        ELSE NULL
    END;
$$;

CREATE OR REPLACE FUNCTION tgn.to_float(v text)
RETURNS double precision
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE
        WHEN v IS NULL OR btrim(v) = '' THEN NULL
        WHEN btrim(v) ~ '^-?([0-9]+(\.[0-9]+)?|\.[0-9]+)$' THEN btrim(v)::double precision
        ELSE NULL
    END;
$$;

CREATE TABLE IF NOT EXISTS tgn.subject (
    subject_id bigint PRIMARY KEY,
    record_type text,
    place_type_id bigint,
    pref_historic_flag text,
    sort_order integer,
    special_project_code text,
    legacy_subject_id bigint
);

CREATE TABLE IF NOT EXISTS tgn.term (
    term_id bigint PRIMARY KEY,
    subject_id bigint NOT NULL,
    term_text text NOT NULL,
    term_norm text NOT NULL,
    preferred_flag text,
    historic_flag text,
    language_code text,
    term_type text,
    display_order integer,
    start_date_text text,
    end_date_text text,
    display_date_text text,
    other_flags text,
    vernacular_flag text
);

CREATE TABLE IF NOT EXISTS tgn.language_rels (
    rel_key text,
    rel_type text,
    subject_id bigint,
    term_id bigint,
    qualifier text,
    language_code text,
    preferred_flag text,
    status_flag text
);

CREATE TABLE IF NOT EXISTS tgn.subject_rels (
    subject_rel_id bigserial PRIMARY KEY,
    start_date_text text,
    end_date_text text,
    rel_type text,
    preferred_flag text,
    historic_flag text,
    rel_qualifier text,
    parent_subject_id bigint,
    child_subject_id bigint,
    hierarchy_flag text
);

CREATE TABLE IF NOT EXISTS tgn.subject_merge (
    new_subject_id bigint,
    dominant_subject_id bigint,
    merged_subject_id bigint,
    PRIMARY KEY (new_subject_id, dominant_subject_id, merged_subject_id)
);

CREATE TABLE IF NOT EXISTS tgn.place_type (
    place_type_id bigint PRIMARY KEY,
    place_type_label text NOT NULL
);

CREATE TABLE IF NOT EXISTS tgn.place_type_rels (
    rel_note text,
    rel_order integer,
    rel_year_text text,
    rel_type text,
    preferred_flag text,
    place_type_id bigint NOT NULL,
    rel_value text,
    subject_id bigint NOT NULL
);

CREATE TABLE IF NOT EXISTS tgn.coordinates (
    coordinates_id bigserial PRIMARY KEY,
    c1 text,
    c2 text,
    lat_decimal_raw double precision,
    lat_degrees integer,
    lat_hemisphere text,
    lat_minutes integer,
    lat_seconds double precision,
    c8 text,
    c9 text,
    c10 text,
    c11 text,
    c12 text,
    c13 text,
    c14 text,
    c15 text,
    c16 text,
    c17 text,
    lon_decimal_raw double precision,
    lon_degrees integer,
    lon_axis_1 text,
    lon_axis_2 text,
    lon_minutes integer,
    lon_seconds double precision,
    c24 text,
    c25 text,
    c26 text,
    c27 text,
    c28 text,
    c29 text,
    c30 text,
    c31 text,
    c32 text,
    c33 text,
    subject_id bigint NOT NULL,
    lat_decimal_derived double precision,
    lon_decimal_derived double precision
);

CREATE TABLE IF NOT EXISTS tgn.search_term (
    term_id bigint PRIMARY KEY,
    subject_id bigint NOT NULL,
    matched_term text NOT NULL,
    matched_term_norm text NOT NULL,
    matched_term_clean text NOT NULL,
    preferred_term text NOT NULL,
    preferred_term_clean text NOT NULL,
    term_type text,
    preferred_flag text,
    historic_flag text,
    parent_subject_id bigint,
    place_type_id bigint,
    place_type_label text,
    lat double precision,
    lon double precision,
    ancestor_blob text NOT NULL DEFAULT '',
    ancestor_pairs jsonb NOT NULL DEFAULT '[]'::jsonb
);

CREATE TABLE IF NOT EXISTS tgn.search_term_index (
    term_id bigint PRIMARY KEY,
    subject_id bigint NOT NULL,
    matched_term_norm text NOT NULL,
    matched_term_clean text NOT NULL,
    term_type text,
    historic_flag text
);

CREATE INDEX IF NOT EXISTS term_subject_idx ON tgn.term (subject_id);
CREATE INDEX IF NOT EXISTS term_norm_idx ON tgn.term (term_norm);
CREATE INDEX IF NOT EXISTS term_norm_trgm_idx ON tgn.term USING gin (term_norm gin_trgm_ops);
CREATE INDEX IF NOT EXISTS term_norm_cover_idx
    ON tgn.term (term_norm, term_id)
    INCLUDE (subject_id, term_text, preferred_flag, historic_flag);
CREATE INDEX IF NOT EXISTS term_pref_by_subject_idx
    ON tgn.term (subject_id, display_order, term_id)
    WHERE preferred_flag = 'P';
-- Match the preferred-term ORDER BY used by match_place_name/get_place_by_id so the
-- planner can satisfy repeated subject-level lookups without an extra sort.
CREATE INDEX IF NOT EXISTS term_best_by_subject_idx
    ON tgn.term (
        subject_id,
        (CASE WHEN btrim(COALESCE(term_type, '')) = 'P' THEN 0 ELSE 1 END),
        display_order,
        term_id
    )
    INCLUDE (term_text, term_norm);
CREATE INDEX IF NOT EXISTS language_rels_term_idx ON tgn.language_rels (term_id);
CREATE INDEX IF NOT EXISTS subject_rels_parent_idx ON tgn.subject_rels (parent_subject_id);
CREATE INDEX IF NOT EXISTS subject_rels_child_idx ON tgn.subject_rels (child_subject_id);
CREATE INDEX IF NOT EXISTS subject_rels_child_pref_idx
    ON tgn.subject_rels (child_subject_id, preferred_flag, subject_rel_id)
    INCLUDE (parent_subject_id);
-- Match the parent-selection ORDER BY used throughout the recursive hierarchy walks.
CREATE INDEX IF NOT EXISTS subject_rels_parent_choice_idx
    ON tgn.subject_rels (
        child_subject_id,
        (CASE WHEN btrim(COALESCE(historic_flag, '')) = 'H' THEN 1 ELSE 0 END),
        (CASE WHEN btrim(COALESCE(hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END),
        (CASE WHEN btrim(COALESCE(preferred_flag, '')) = 'P' THEN 0 ELSE 1 END),
        subject_rel_id
    )
    INCLUDE (parent_subject_id);
CREATE INDEX IF NOT EXISTS coordinates_subject_idx ON tgn.coordinates (subject_id);
CREATE INDEX IF NOT EXISTS coordinates_subject_first_idx
    ON tgn.coordinates (subject_id, coordinates_id)
    INCLUDE (lat_decimal_derived, lat_decimal_raw, lon_decimal_derived, lon_decimal_raw);
CREATE INDEX IF NOT EXISTS coordinates_latlon_idx ON tgn.coordinates (lat_decimal_derived, lon_decimal_derived);
CREATE INDEX IF NOT EXISTS place_type_label_idx ON tgn.place_type (place_type_label);
CREATE INDEX IF NOT EXISTS place_type_rels_subject_idx ON tgn.place_type_rels (subject_id, preferred_flag, rel_order, place_type_id);
CREATE INDEX IF NOT EXISTS place_type_rels_best_by_subject_idx
    ON tgn.place_type_rels (
        subject_id,
        (CASE WHEN btrim(COALESCE(preferred_flag, '')) = 'P' THEN 0 ELSE 1 END),
        rel_order,
        place_type_id
    );
CREATE INDEX IF NOT EXISTS search_term_subject_idx ON tgn.search_term (subject_id);
CREATE INDEX IF NOT EXISTS search_term_norm_idx ON tgn.search_term (matched_term_norm);
CREATE INDEX IF NOT EXISTS search_term_norm_trgm_idx ON tgn.search_term USING gin (matched_term_norm gin_trgm_ops);
CREATE INDEX IF NOT EXISTS search_term_doc_idx
    ON tgn.search_term
    USING gin (to_tsvector('simple', matched_term_clean || ' ' || ancestor_blob));
CREATE INDEX IF NOT EXISTS search_term_index_subject_idx ON tgn.search_term_index (subject_id);
CREATE INDEX IF NOT EXISTS search_term_index_norm_idx ON tgn.search_term_index (matched_term_norm);
CREATE INDEX IF NOT EXISTS search_term_index_norm_trgm_idx ON tgn.search_term_index USING gin (matched_term_norm gin_trgm_ops);
CREATE INDEX IF NOT EXISTS search_term_index_doc_idx
    ON tgn.search_term_index
    USING gin (to_tsvector('simple', matched_term_clean));
