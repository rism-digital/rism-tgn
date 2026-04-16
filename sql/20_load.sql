TRUNCATE TABLE
    tgn.subject,
    tgn.term,
    tgn.language_rels,
    tgn.subject_rels,
    tgn.subject_merge,
    tgn.place_type,
    tgn.place_type_rels,
    tgn.coordinates,
    tgn.search_term,
    tgn.search_term_index
RESTART IDENTITY;

INSERT INTO tgn.subject (
    subject_id,
    record_type,
    place_type_id,
    pref_historic_flag,
    sort_order,
    special_project_code,
    legacy_subject_id
)
SELECT DISTINCT ON (subject_id)
    subject_id,
    record_type,
    place_type_id,
    pref_historic_flag,
    sort_order,
    special_project_code,
    legacy_subject_id
FROM (
    SELECT
        tgn.to_bigint(c1) AS subject_id,
        NULLIF(c2, '') AS record_type,
        tgn.to_bigint(c3) AS place_type_id,
        NULLIF(c4, '') AS pref_historic_flag,
        tgn.to_int(c5) AS sort_order,
        NULLIF(c6, '') AS special_project_code,
        tgn.to_bigint(c7) AS legacy_subject_id
    FROM tgn_stage.subject_raw
    WHERE tgn.to_bigint(c1) IS NOT NULL
) s
ORDER BY
    subject_id,
    CASE WHEN legacy_subject_id = subject_id THEN 0 ELSE 1 END,
    sort_order NULLS LAST;

-- Some TERM/COORDINATES records reference subject IDs not present in SUBJECT.out.
-- Create minimal placeholder rows so FK enforcement can remain enabled.
INSERT INTO tgn.subject (
    subject_id,
    record_type
)
SELECT DISTINCT
    refs.subject_id,
    'MISSING_SUBJECT'
FROM (
    SELECT tgn.to_bigint(c10) AS subject_id
    FROM tgn_stage.term_raw
    WHERE tgn.to_bigint(c10) IS NOT NULL
    UNION
    SELECT tgn.to_bigint(c34) AS subject_id
    FROM tgn_stage.coordinates_raw
    WHERE tgn.to_bigint(c34) IS NOT NULL
) refs
LEFT JOIN tgn.subject s
    ON s.subject_id = refs.subject_id
WHERE s.subject_id IS NULL;

INSERT INTO tgn.term (
    term_id,
    subject_id,
    term_text,
    term_norm,
    preferred_flag,
    historic_flag,
    language_code,
    term_type,
    display_order,
    start_date_text,
    end_date_text,
    display_date_text,
    other_flags,
    vernacular_flag
)
SELECT
    tgn.to_bigint(c12) AS term_id,
    tgn.to_bigint(c10) AS subject_id,
    COALESCE(NULLIF(c11, ''), '[missing]') AS term_text,
    lower(unaccent(COALESCE(NULLIF(c11, ''), '[missing]'))) AS term_norm,
    NULLIF(c6, '') AS preferred_flag,
    NULLIF(c9, '') AS historic_flag,
    NULLIF(c7, '') AS language_code,
    NULLIF(c8, '') AS term_type,
    tgn.to_int(c4) AS display_order,
    NULLIF(c1, '') AS start_date_text,
    NULLIF(c2, '') AS end_date_text,
    NULLIF(c3, '') AS display_date_text,
    NULLIF(c5, '') AS other_flags,
    NULLIF(c13, '') AS vernacular_flag
FROM tgn_stage.term_raw
WHERE tgn.to_bigint(c12) IS NOT NULL
  AND tgn.to_bigint(c10) IS NOT NULL;

INSERT INTO tgn.language_rels (
    rel_key,
    rel_type,
    subject_id,
    term_id,
    qualifier,
    language_code,
    preferred_flag,
    status_flag
)
SELECT
    NULLIF(c1, '') AS rel_key,
    NULLIF(c2, '') AS rel_type,
    tgn.to_bigint(c3) AS subject_id,
    tgn.to_bigint(c4) AS term_id,
    NULLIF(c5, '') AS qualifier,
    NULLIF(c6, '') AS language_code,
    NULLIF(c7, '') AS preferred_flag,
    NULLIF(c8, '') AS status_flag
FROM tgn_stage.language_rels_raw;

INSERT INTO tgn.subject_rels (
    start_date_text,
    end_date_text,
    rel_type,
    preferred_flag,
    historic_flag,
    rel_qualifier,
    parent_subject_id,
    child_subject_id,
    hierarchy_flag
)
SELECT
    NULLIF(c1, '') AS start_date_text,
    NULLIF(c2, '') AS end_date_text,
    NULLIF(c3, '') AS rel_type,
    NULLIF(c4, '') AS preferred_flag,
    NULLIF(c5, '') AS historic_flag,
    NULLIF(c6, '') AS rel_qualifier,
    tgn.to_bigint(c7) AS parent_subject_id,
    tgn.to_bigint(c8) AS child_subject_id,
    NULLIF(c9, '') AS hierarchy_flag
FROM tgn_stage.subject_rels_raw;

INSERT INTO tgn.subject_merge (
    new_subject_id,
    dominant_subject_id,
    merged_subject_id
)
SELECT DISTINCT
    tgn.to_bigint(c1) AS new_subject_id,
    tgn.to_bigint(c2) AS dominant_subject_id,
    tgn.to_bigint(c3) AS merged_subject_id
FROM tgn_stage.subject_merge_raw
WHERE tgn.to_bigint(c1) IS NOT NULL
  AND tgn.to_bigint(c2) IS NOT NULL
  AND tgn.to_bigint(c3) IS NOT NULL;

INSERT INTO tgn.place_type (
    place_type_id,
    place_type_label
)
SELECT DISTINCT ON (place_type_id)
    place_type_id,
    place_type_label
FROM (
    SELECT
        tgn.to_bigint(c2) AS place_type_id,
        NULLIF(c1, '') AS place_type_label
    FROM tgn_stage.ptype_role_raw
    WHERE tgn.to_bigint(c2) IS NOT NULL
) p
ORDER BY
    place_type_id,
    CASE WHEN place_type_label IS NULL THEN 1 ELSE 0 END,
    place_type_label;

INSERT INTO tgn.place_type_rels (
    rel_note,
    rel_order,
    rel_year_text,
    rel_type,
    preferred_flag,
    place_type_id,
    rel_value,
    subject_id
)
SELECT
    NULLIF(c1, '') AS rel_note,
    tgn.to_int(c2) AS rel_order,
    NULLIF(c3, '') AS rel_year_text,
    NULLIF(c4, '') AS rel_type,
    NULLIF(c5, '') AS preferred_flag,
    tgn.to_bigint(c6) AS place_type_id,
    NULLIF(c7, '') AS rel_value,
    tgn.to_bigint(c8) AS subject_id
FROM tgn_stage.ptype_role_rels_raw
WHERE tgn.to_bigint(c6) IS NOT NULL
  AND tgn.to_bigint(c8) IS NOT NULL;

INSERT INTO tgn.coordinates (
    c1,
    c2,
    lat_decimal_raw,
    lat_degrees,
    lat_hemisphere,
    lat_minutes,
    lat_seconds,
    c8,
    c9,
    c10,
    c11,
    c12,
    c13,
    c14,
    c15,
    c16,
    c17,
    lon_decimal_raw,
    lon_degrees,
    lon_axis_1,
    lon_axis_2,
    lon_minutes,
    lon_seconds,
    c24,
    c25,
    c26,
    c27,
    c28,
    c29,
    c30,
    c31,
    c32,
    c33,
    subject_id,
    lat_decimal_derived,
    lon_decimal_derived
)
SELECT
    NULLIF(c1, ''),
    NULLIF(c2, ''),
    tgn.to_float(c3),
    tgn.to_int(c4),
    NULLIF(c5, ''),
    tgn.to_int(c6),
    tgn.to_float(c7),
    NULLIF(c8, ''),
    NULLIF(c9, ''),
    NULLIF(c10, ''),
    NULLIF(c11, ''),
    NULLIF(c12, ''),
    NULLIF(c13, ''),
    NULLIF(c14, ''),
    NULLIF(c15, ''),
    NULLIF(c16, ''),
    NULLIF(c17, ''),
    tgn.to_float(c18),
    tgn.to_int(c19),
    NULLIF(c20, ''),
    NULLIF(c21, ''),
    tgn.to_int(c22),
    tgn.to_float(c23),
    NULLIF(c24, ''),
    NULLIF(c25, ''),
    NULLIF(c26, ''),
    NULLIF(c27, ''),
    NULLIF(c28, ''),
    NULLIF(c29, ''),
    NULLIF(c30, ''),
    NULLIF(c31, ''),
    NULLIF(c32, ''),
    NULLIF(c33, ''),
    tgn.to_bigint(c34) AS subject_id,
    COALESCE(
        tgn.to_float(c3),
        CASE
            WHEN tgn.to_int(c4) IS NULL THEN NULL
            ELSE (
                abs(tgn.to_int(c4))
                + COALESCE(tgn.to_int(c6), 0) / 60.0
                + COALESCE(tgn.to_float(c7), 0) / 3600.0
            ) * CASE WHEN upper(COALESCE(c5, 'N')) = 'S' THEN -1 ELSE 1 END
        END
    ) AS lat_decimal_derived,
    COALESCE(
        tgn.to_float(c18),
        CASE
            WHEN tgn.to_int(c19) IS NULL THEN NULL
            ELSE (
                abs(tgn.to_int(c19))
                + COALESCE(tgn.to_int(c22), 0) / 60.0
                + COALESCE(tgn.to_float(c23), 0) / 3600.0
            ) * CASE WHEN upper(COALESCE(c21, 'E')) = 'W' THEN -1 ELSE 1 END
        END
    ) AS lon_decimal_derived
FROM tgn_stage.coordinates_raw
WHERE tgn.to_bigint(c34) IS NOT NULL;

ALTER TABLE tgn.term
    DROP CONSTRAINT IF EXISTS term_subject_fk;
ALTER TABLE tgn.term
    ADD CONSTRAINT term_subject_fk
    FOREIGN KEY (subject_id) REFERENCES tgn.subject(subject_id) ON DELETE CASCADE;

ALTER TABLE tgn.coordinates
    DROP CONSTRAINT IF EXISTS coordinates_subject_fk;
ALTER TABLE tgn.coordinates
    ADD CONSTRAINT coordinates_subject_fk
    FOREIGN KEY (subject_id) REFERENCES tgn.subject(subject_id) ON DELETE CASCADE;

INSERT INTO tgn.search_term_index (
    term_id,
    subject_id,
    matched_term_norm,
    matched_term_clean,
    term_type,
    historic_flag
)
SELECT
    t.term_id,
    t.subject_id,
    t.term_norm AS matched_term_norm,
    trim(regexp_replace(t.term_norm, '[^a-z0-9]+', ' ', 'g')) AS matched_term_clean,
    t.term_type,
    t.historic_flag
FROM tgn.term t;

INSERT INTO tgn.search_term (
    term_id,
    subject_id,
    matched_term,
    matched_term_norm,
    matched_term_clean,
    preferred_term,
    preferred_term_clean,
    term_type,
    preferred_flag,
    historic_flag,
    parent_subject_id,
    place_type_id,
    place_type_label,
    lat,
    lon,
    ancestor_blob,
    ancestor_pairs
)
WITH RECURSIVE preferred_term AS (
    SELECT DISTINCT ON (t.subject_id)
        t.subject_id,
        t.term_text AS preferred_term,
        t.term_norm AS preferred_term_norm,
        trim(regexp_replace(t.term_norm, '[^a-z0-9]+', ' ', 'g')) AS preferred_term_clean
    FROM tgn.term t
    ORDER BY
        t.subject_id,
        CASE WHEN btrim(COALESCE(t.term_type, '')) = 'P' THEN 0 ELSE 1 END,
        t.display_order NULLS LAST,
        t.term_id
),
parent_choice AS (
    SELECT DISTINCT ON (sr.child_subject_id)
        sr.child_subject_id AS subject_id,
        sr.parent_subject_id
    FROM tgn.subject_rels sr
    WHERE sr.child_subject_id IS NOT NULL
      AND sr.parent_subject_id IS NOT NULL
    ORDER BY
        sr.child_subject_id,
        CASE WHEN btrim(COALESCE(sr.historic_flag, '')) = 'H' THEN 1 ELSE 0 END,
        CASE WHEN btrim(COALESCE(sr.hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END,
        CASE WHEN btrim(COALESCE(sr.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
        sr.subject_rel_id
),
place_type_choice AS (
    SELECT DISTINCT ON (r.subject_id)
        r.subject_id,
        r.place_type_id,
        pt.place_type_label
    FROM tgn.place_type_rels r
    LEFT JOIN tgn.place_type pt
        ON pt.place_type_id = r.place_type_id
    ORDER BY
        r.subject_id,
        CASE WHEN btrim(COALESCE(r.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
        r.rel_order NULLS LAST,
        r.place_type_id
),
coord_choice AS (
    SELECT DISTINCT ON (c.subject_id)
        c.subject_id,
        COALESCE(c.lat_decimal_derived, c.lat_decimal_raw) AS lat,
        COALESCE(c.lon_decimal_derived, c.lon_decimal_raw) AS lon
    FROM tgn.coordinates c
    ORDER BY c.subject_id, c.coordinates_id
),
ancestors AS (
    SELECT
        pc.subject_id,
        pc.parent_subject_id AS ancestor_id,
        1 AS depth,
        ARRAY[pc.subject_id, pc.parent_subject_id]::bigint[] AS path
    FROM parent_choice pc
    WHERE pc.parent_subject_id IS NOT NULL
      AND pc.parent_subject_id <> 7029392

    UNION ALL

    SELECT
        a.subject_id,
        pc.parent_subject_id AS ancestor_id,
        a.depth + 1 AS depth,
        a.path || pc.parent_subject_id
    FROM ancestors a
    JOIN parent_choice pc
        ON pc.subject_id = a.ancestor_id
    WHERE pc.parent_subject_id IS NOT NULL
      AND a.depth < 50
      AND pc.parent_subject_id <> 7029392
      AND NOT (pc.parent_subject_id = ANY(a.path))
),
ancestor_dedup AS (
    SELECT
        subject_id,
        ancestor_id,
        MIN(depth) AS min_depth
    FROM ancestors
    GROUP BY subject_id, ancestor_id
),
ancestor_named AS (
    SELECT
        ad.subject_id,
        ad.ancestor_id,
        ad.min_depth,
        pt.preferred_term,
        pt.preferred_term_norm
    FROM ancestor_dedup ad
    LEFT JOIN preferred_term pt
        ON pt.subject_id = ad.ancestor_id
),
ancestor_agg AS (
    SELECT
        an.subject_id,
        COALESCE(
            string_agg(
                COALESCE(an.preferred_term_norm, an.ancestor_id::text),
                ' '
                ORDER BY an.min_depth, an.ancestor_id
            ),
            ''
        ) AS ancestor_blob,
        COALESCE(
            jsonb_agg(
                jsonb_build_array(
                    an.ancestor_id,
                    COALESCE(an.preferred_term, an.ancestor_id::text)
                )
                ORDER BY an.min_depth, an.ancestor_id
            ),
            '[]'::jsonb
        ) AS ancestor_pairs
    FROM ancestor_named an
    GROUP BY an.subject_id
)
SELECT
    t.term_id,
    t.subject_id,
    t.term_text AS matched_term,
    t.term_norm AS matched_term_norm,
    trim(regexp_replace(t.term_norm, '[^a-z0-9]+', ' ', 'g')) AS matched_term_clean,
    COALESCE(pt.preferred_term, t.term_text) AS preferred_term,
    COALESCE(pt.preferred_term_clean, trim(regexp_replace(t.term_norm, '[^a-z0-9]+', ' ', 'g'))) AS preferred_term_clean,
    t.term_type,
    t.preferred_flag,
    t.historic_flag,
    pc.parent_subject_id,
    ptc.place_type_id,
    ptc.place_type_label,
    cc.lat,
    cc.lon,
    COALESCE(aa.ancestor_blob, '') AS ancestor_blob,
    COALESCE(aa.ancestor_pairs, '[]'::jsonb) AS ancestor_pairs
FROM tgn.term t
LEFT JOIN preferred_term pt
    ON pt.subject_id = t.subject_id
LEFT JOIN parent_choice pc
    ON pc.subject_id = t.subject_id
LEFT JOIN place_type_choice ptc
    ON ptc.subject_id = t.subject_id
LEFT JOIN coord_choice cc
    ON cc.subject_id = t.subject_id
LEFT JOIN ancestor_agg aa
    ON aa.subject_id = t.subject_id;

ANALYZE tgn.term;
ANALYZE tgn.subject_rels;
ANALYZE tgn.coordinates;
ANALYZE tgn.place_type;
ANALYZE tgn.place_type_rels;
ANALYZE tgn.search_term;
ANALYZE tgn.search_term_index;
