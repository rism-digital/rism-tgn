CREATE INDEX IF NOT EXISTS term_best_by_subject_idx
    ON tgn.term (
        subject_id,
        (CASE WHEN btrim(COALESCE(term_type, '')) = 'P' THEN 0 ELSE 1 END),
        display_order,
        term_id
    )
    INCLUDE (term_text, term_norm);

CREATE INDEX IF NOT EXISTS subject_rels_parent_choice_idx
    ON tgn.subject_rels (
        child_subject_id,
        (CASE WHEN btrim(COALESCE(historic_flag, '')) = 'H' THEN 1 ELSE 0 END),
        (CASE WHEN btrim(COALESCE(hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END),
        (CASE WHEN btrim(COALESCE(preferred_flag, '')) = 'P' THEN 0 ELSE 1 END),
        subject_rel_id
    )
    INCLUDE (parent_subject_id);

CREATE INDEX IF NOT EXISTS place_type_rels_best_by_subject_idx
    ON tgn.place_type_rels (
        subject_id,
        (CASE WHEN btrim(COALESCE(preferred_flag, '')) = 'P' THEN 0 ELSE 1 END),
        rel_order,
        place_type_id
    );

CREATE INDEX IF NOT EXISTS search_term_index_norm_idx ON tgn.search_term_index (matched_term_norm);

CREATE INDEX IF NOT EXISTS search_term_index_norm_trgm_idx
    ON tgn.search_term_index USING gin (matched_term_norm gin_trgm_ops);

CREATE INDEX IF NOT EXISTS search_term_index_doc_idx
    ON tgn.search_term_index
    USING gin (to_tsvector('simple', matched_term_clean));

DROP FUNCTION IF EXISTS tgn.match_place_name(text, bigint, bigint, integer);

CREATE OR REPLACE FUNCTION tgn.match_place_name(
    in_name text,
    context_parent_id bigint DEFAULT NULL,
    context_country_id bigint DEFAULT NULL,
    limit_n integer DEFAULT 10
)
RETURNS TABLE (
    tgn_id bigint,
    matched_term text,
    preferred_term text,
    place_type_id bigint,
    place_type_label text,
    score double precision,
    parent_subject_id bigint,
    lat double precision,
    lon double precision,
    ancestor_pairs jsonb,
    alternate_names jsonb
)
LANGUAGE sql
STABLE
AS $$
WITH RECURSIVE input AS (
    SELECT
        lower(unaccent(trim(in_name))) AS q,
        trim(regexp_replace(lower(unaccent(trim(in_name))), '[^a-z0-9]+', ' ', 'g')) AS q_clean
),
tokens AS (
    SELECT DISTINCT token
    FROM input i,
    LATERAL regexp_split_to_table(trim(i.q_clean), ' +') AS token
    WHERE token <> ''
      AND length(token) >= 2
),
exact_pool AS (
    SELECT
        sti.term_id,
        1.0::double precision AS sim,
        TRUE AS is_exact
    FROM tgn.search_term_index sti
    CROSS JOIN input i
    WHERE i.q <> ''
      AND sti.matched_term_norm = i.q
),
doc_pool AS (
    SELECT
        sti.term_id,
        ts_rank_cd(
            to_tsvector('simple', sti.matched_term_clean),
            plainto_tsquery('simple', i.q_clean)
        )::double precision AS sim,
        FALSE AS is_exact
    FROM tgn.search_term_index sti
    CROSS JOIN input i
    WHERE i.q_clean <> ''
      AND EXISTS (SELECT 1 FROM tokens)
      AND to_tsvector('simple', sti.matched_term_clean) @@ plainto_tsquery('simple', i.q_clean)
    ORDER BY sim DESC, sti.term_id
    LIMIT GREATEST(COALESCE(limit_n, 10) * 40, 200)
),
fuzzy_pool AS (
    SELECT
        term_id,
        similarity(sti.matched_term_norm, i.q)::double precision AS sim,
        FALSE AS is_exact
    FROM tgn.search_term_index sti
    CROSS JOIN input i
    WHERE i.q <> ''
      AND sti.matched_term_norm % i.q
      AND sti.matched_term_norm <> i.q
    ORDER BY sim DESC
    LIMIT GREATEST(COALESCE(limit_n, 10) * 40, 200)
),
candidate_pool AS (
    SELECT * FROM exact_pool
    UNION ALL
    SELECT * FROM doc_pool
    UNION ALL
    SELECT * FROM fuzzy_pool
),
ranked_term_hits AS (
    SELECT
        cp.*,
        ROW_NUMBER() OVER (
            PARTITION BY cp.term_id
            ORDER BY
                CASE WHEN cp.is_exact THEN 0 ELSE 1 END,
                cp.sim DESC,
                cp.term_id
        ) AS term_rn
    FROM candidate_pool cp
),
ranked_terms AS (
    SELECT
        st.subject_id,
        st.term_id,
        st.matched_term,
        st.matched_term_norm,
        st.matched_term_clean,
        st.preferred_term,
        st.preferred_term_clean,
        st.term_type,
        st.preferred_flag,
        st.historic_flag,
        st.parent_subject_id,
        st.place_type_id,
        st.place_type_label,
        st.lat,
        st.lon,
        st.ancestor_blob,
        st.ancestor_pairs,
        rth.sim,
        rth.is_exact,
        ROW_NUMBER() OVER (
            PARTITION BY st.subject_id
            ORDER BY
                CASE WHEN rth.is_exact THEN 0 ELSE 1 END,
                rth.sim DESC,
                CASE WHEN btrim(COALESCE(st.term_type, '')) = 'P' THEN 0 ELSE 1 END,
                st.term_id
        ) AS subject_rn
    FROM ranked_term_hits rth
    JOIN tgn.search_term st
        ON st.term_id = rth.term_id
    WHERE rth.term_rn = 1
      AND EXISTS (
          SELECT 1
          FROM tgn.place_type_rels ptr
          WHERE ptr.subject_id = st.subject_id
            AND ptr.place_type_id IN (83002, 81010, 84251, 82411, 81115, 81175, 81161)
      )
),
subject_best AS (
    SELECT
        subject_id,
        term_id,
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
        ancestor_pairs,
        sim,
        is_exact
    FROM ranked_terms
    WHERE subject_rn = 1
),
token_hits AS (
    SELECT
        sb.subject_id,
        COUNT(tok.token)::integer AS token_count,
        COALESCE(SUM(CASE WHEN POSITION(tok.token IN sb.matched_term_clean) > 0 THEN 1 ELSE 0 END), 0)::integer AS term_hits,
        COALESCE(SUM(CASE WHEN sb.ancestor_blob <> '' AND POSITION(tok.token IN sb.ancestor_blob) > 0 THEN 1 ELSE 0 END), 0)::integer AS ancestor_hits,
        COALESCE(SUM(CASE
            WHEN POSITION(tok.token IN sb.matched_term_clean) > 0
              OR (sb.ancestor_blob <> '' AND POSITION(tok.token IN sb.ancestor_blob) > 0)
            THEN 1 ELSE 0
        END), 0)::integer AS coverage_hits
    FROM subject_best sb
    LEFT JOIN tokens tok
        ON TRUE
    GROUP BY sb.subject_id, sb.ancestor_blob
),
scored AS (
    SELECT
        sb.subject_id AS tgn_id,
        sb.matched_term,
        sb.preferred_term,
        sb.place_type_id,
        sb.place_type_label,
        sb.parent_subject_id,
        sb.lat,
        sb.lon,
        sb.ancestor_pairs,
        CASE WHEN sb.matched_term_clean = i.q_clean THEN 1 ELSE 0 END AS rank_term_exact,
        CASE
            WHEN sb.preferred_term_clean = i.q_clean
            THEN 1 ELSE 0
        END AS rank_pref_exact,
        GREATEST((
            CASE WHEN sb.is_exact THEN 1.0 ELSE 0.0 END
            + CASE WHEN btrim(COALESCE(sb.term_type, '')) = 'P' THEN 0.35 ELSE 0.0 END
            + CASE WHEN sb.historic_flag = 'H' THEN -0.10 ELSE 0.0 END
            + CASE WHEN context_parent_id IS NOT NULL AND sb.parent_subject_id = context_parent_id THEN 0.30 ELSE 0.0 END
            + CASE WHEN context_country_id IS NOT NULL AND (sb.subject_id = context_country_id OR sb.parent_subject_id = context_country_id) THEN 0.20 ELSE 0.0 END
            + sb.sim
            + CASE
                WHEN th.token_count > 0 THEN (th.term_hits::double precision / th.token_count::double precision) * 0.55
                ELSE 0.0
              END
            + CASE
                WHEN th.token_count > 0 THEN (th.ancestor_hits::double precision / th.token_count::double precision) * 0.85
                ELSE 0.0
              END
            + CASE
                WHEN th.token_count > 0 THEN (th.coverage_hits::double precision / th.token_count::double precision) * 1.70
                ELSE 0.0
              END
            - CASE
                WHEN th.token_count > 0 THEN ((th.token_count - th.coverage_hits)::double precision / th.token_count::double precision) * 0.80
                ELSE 0.0
              END
        )::double precision, 0.0::double precision) AS score
    FROM subject_best sb
    CROSS JOIN input i
    LEFT JOIN token_hits th
        ON th.subject_id = sb.subject_id
),
best_matches AS (
    SELECT
        tgn_id,
        matched_term,
        preferred_term,
        place_type_id,
        place_type_label,
        score,
        parent_subject_id,
        lat,
        lon,
        ancestor_pairs,
        rank_term_exact,
        rank_pref_exact
    FROM scored
    ORDER BY
        rank_term_exact DESC,
        rank_pref_exact DESC,
        score DESC,
        preferred_term,
        tgn_id
    LIMIT GREATEST(COALESCE(limit_n, 10), 1)
),
ancestors AS (
    SELECT
        bm.tgn_id,
        p.parent_subject_id AS ancestor_id,
        1 AS depth,
        ARRAY[bm.tgn_id, p.parent_subject_id]::bigint[] AS path
    FROM best_matches bm
    JOIN LATERAL (
        SELECT sr.parent_subject_id
        FROM tgn.subject_rels sr
        WHERE sr.child_subject_id = bm.tgn_id
          AND sr.parent_subject_id IS NOT NULL
        ORDER BY
            CASE WHEN btrim(COALESCE(sr.historic_flag, '')) = 'H' THEN 1 ELSE 0 END,
            CASE WHEN btrim(COALESCE(sr.hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END,
            CASE WHEN btrim(COALESCE(sr.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
            sr.subject_rel_id
        LIMIT 1
    ) p ON TRUE
    WHERE p.parent_subject_id IS NOT NULL
      AND p.parent_subject_id <> 7029392

    UNION ALL

    SELECT
        a.tgn_id,
        p.parent_subject_id AS ancestor_id,
        a.depth + 1 AS depth,
        a.path || p.parent_subject_id
    FROM ancestors a
    JOIN LATERAL (
        SELECT sr.parent_subject_id
        FROM tgn.subject_rels sr
        WHERE sr.child_subject_id = a.ancestor_id
          AND sr.parent_subject_id IS NOT NULL
        ORDER BY
            CASE WHEN btrim(COALESCE(sr.historic_flag, '')) = 'H' THEN 1 ELSE 0 END,
            CASE WHEN btrim(COALESCE(sr.hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END,
            CASE WHEN btrim(COALESCE(sr.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
            sr.subject_rel_id
        LIMIT 1
    ) p ON TRUE
    WHERE p.parent_subject_id IS NOT NULL
      AND a.depth < 50
      AND p.parent_subject_id <> 7029392
      AND NOT (p.parent_subject_id = ANY(a.path))
),
ancestor_dedup AS (
    SELECT
        tgn_id,
        ancestor_id,
        MIN(depth) AS min_depth
    FROM ancestors
    GROUP BY tgn_id, ancestor_id
),
ancestor_named AS (
    SELECT
        ad.tgn_id,
        ad.ancestor_id,
        ad.min_depth,
        pt.term_text AS ancestor_name,
        apt.place_type_id AS ancestor_place_type_id,
        apt.place_type_label AS ancestor_place_type_label
    FROM ancestor_dedup ad
    LEFT JOIN LATERAL (
        SELECT t.term_text
        FROM tgn.term t
        WHERE t.subject_id = ad.ancestor_id
        ORDER BY
            CASE WHEN btrim(COALESCE(t.term_type, '')) = 'P' THEN 0 ELSE 1 END,
            t.display_order NULLS LAST,
            t.term_id
        LIMIT 1
    ) pt ON TRUE
    LEFT JOIN LATERAL (
        SELECT
            r.place_type_id,
            ptt.place_type_label
        FROM tgn.place_type_rels r
        LEFT JOIN tgn.place_type ptt
            ON ptt.place_type_id = r.place_type_id
        WHERE r.subject_id = ad.ancestor_id
        ORDER BY
            CASE WHEN btrim(COALESCE(r.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
            r.rel_order NULLS LAST,
            r.place_type_id
        LIMIT 1
    ) apt ON TRUE
),
ancestor_ranked AS (
    SELECT
        tgn_id,
        ancestor_id,
        min_depth,
        ancestor_name,
        ancestor_place_type_id,
        ancestor_place_type_label,
        ROW_NUMBER() OVER (
            PARTITION BY tgn_id
            ORDER BY min_depth, ancestor_id
        ) AS rn
    FROM ancestor_named
),
ancestor_agg AS (
    SELECT
        tgn_id,
        ARRAY_AGG(ancestor_id ORDER BY min_depth, ancestor_id) AS ancestor_ids,
        ARRAY_AGG(COALESCE(ancestor_name, ancestor_id::text) ORDER BY min_depth, ancestor_id) AS ancestor_names,
        ARRAY_AGG(ancestor_place_type_id ORDER BY min_depth, ancestor_id) AS ancestor_place_type_ids,
        ARRAY_AGG(ancestor_place_type_label ORDER BY min_depth, ancestor_id) AS ancestor_place_type_labels
    FROM ancestor_ranked
    GROUP BY tgn_id
)
SELECT
    bm.tgn_id,
    bm.matched_term,
    bm.preferred_term,
    bm.place_type_id,
    bm.place_type_label,
    bm.score,
    bm.parent_subject_id,
    bm.lat,
    bm.lon,
    COALESCE(
        (
            WITH trimmed AS (
                SELECT
                    COALESCE(
                        CASE
                            WHEN aa.ancestor_ids IS NULL THEN ARRAY[]::bigint[]
                            WHEN array_position(aa.ancestor_ids, 7029392) IS NULL THEN aa.ancestor_ids
                            WHEN array_position(aa.ancestor_ids, 7029392) = 1 THEN ARRAY[]::bigint[]
                            ELSE aa.ancestor_ids[1:array_position(aa.ancestor_ids, 7029392)-1]
                        END,
                        ARRAY[]::bigint[]
                    ) AS ids,
                    COALESCE(
                        CASE
                            WHEN aa.ancestor_names IS NULL THEN ARRAY[]::text[]
                            WHEN array_position(aa.ancestor_ids, 7029392) IS NULL THEN aa.ancestor_names
                            WHEN array_position(aa.ancestor_ids, 7029392) = 1 THEN ARRAY[]::text[]
                            ELSE aa.ancestor_names[1:array_position(aa.ancestor_ids, 7029392)-1]
                        END,
                        ARRAY[]::text[]
                    ) AS names,
                    COALESCE(
                        CASE
                            WHEN aa.ancestor_place_type_ids IS NULL THEN ARRAY[]::bigint[]
                            WHEN array_position(aa.ancestor_ids, 7029392) IS NULL THEN aa.ancestor_place_type_ids
                            WHEN array_position(aa.ancestor_ids, 7029392) = 1 THEN ARRAY[]::bigint[]
                            ELSE aa.ancestor_place_type_ids[1:array_position(aa.ancestor_ids, 7029392)-1]
                        END,
                        ARRAY[]::bigint[]
                    ) AS place_type_ids,
                    COALESCE(
                        CASE
                            WHEN aa.ancestor_place_type_labels IS NULL THEN ARRAY[]::text[]
                            WHEN array_position(aa.ancestor_ids, 7029392) IS NULL THEN aa.ancestor_place_type_labels
                            WHEN array_position(aa.ancestor_ids, 7029392) = 1 THEN ARRAY[]::text[]
                            ELSE aa.ancestor_place_type_labels[1:array_position(aa.ancestor_ids, 7029392)-1]
                        END,
                        ARRAY[]::text[]
                    ) AS place_type_labels
            )
            SELECT COALESCE(
                (
                    SELECT jsonb_agg(
                        jsonb_build_array(
                            ids[idx],
                            names[idx],
                            place_type_ids[idx],
                            place_type_labels[idx]
                        )
                        ORDER BY idx
                    )
                    FROM trimmed, generate_subscripts(trimmed.ids, 1) AS idx
                ),
                '[]'::jsonb
            )
        ),
        '[]'::jsonb
    ) AS ancestor_pairs,
    COALESCE(alt.alternate_names, '[]'::jsonb) AS alternate_names
FROM best_matches bm
LEFT JOIN ancestor_agg aa
    ON aa.tgn_id = bm.tgn_id
LEFT JOIN LATERAL (
    SELECT jsonb_agg(name.term_text ORDER BY name.sort_group, name.display_order, name.term_id) AS alternate_names
    FROM (
        SELECT DISTINCT ON (t.term_text)
            t.term_text,
            CASE WHEN btrim(COALESCE(t.term_type, '')) = 'P' THEN 0 ELSE 1 END AS sort_group,
            t.display_order,
            t.term_id
        FROM tgn.term t
        WHERE t.subject_id = bm.tgn_id
        ORDER BY t.term_text, sort_group, t.display_order NULLS LAST, t.term_id
    ) name
) alt ON TRUE
ORDER BY
    bm.rank_term_exact DESC,
    bm.rank_pref_exact DESC,
    bm.score DESC,
    bm.preferred_term,
    bm.tgn_id;
$$;

DROP FUNCTION IF EXISTS tgn.get_place_by_id(bigint);

CREATE OR REPLACE FUNCTION tgn.get_place_by_id(
    in_id bigint
)
RETURNS TABLE (
    tgn_id bigint,
    matched_term text,
    preferred_term text,
    place_type_id bigint,
    place_type_label text,
    score double precision,
    parent_subject_id bigint,
    lat double precision,
    lon double precision,
    ancestor_pairs jsonb,
    alternate_names jsonb
)
LANGUAGE sql
STABLE
AS $$
WITH RECURSIVE
base AS (
    SELECT in_id AS tgn_id
),
terms AS (
    SELECT
        b.tgn_id,
        COALESCE(mt.term_text, pt.term_text, b.tgn_id::text) AS matched_term,
        COALESCE(pt.term_text, mt.term_text, b.tgn_id::text) AS preferred_term
    FROM base b
    LEFT JOIN LATERAL (
        SELECT t.term_text
        FROM tgn.term t
        WHERE t.subject_id = b.tgn_id
        ORDER BY
            CASE WHEN btrim(COALESCE(t.term_type, '')) = 'P' THEN 0 ELSE 1 END,
            t.display_order NULLS LAST,
            t.term_id
        LIMIT 1
    ) pt ON TRUE
    LEFT JOIN LATERAL (
        SELECT t.term_text
        FROM tgn.term t
        WHERE t.subject_id = b.tgn_id
        ORDER BY t.term_id
        LIMIT 1
    ) mt ON TRUE
),
place_type_map AS (
    SELECT DISTINCT ON (r.subject_id)
        r.subject_id,
        r.place_type_id,
        pt.place_type_label
    FROM tgn.place_type_rels r
    LEFT JOIN tgn.place_type pt ON pt.place_type_id = r.place_type_id
    WHERE r.subject_id = in_id
    ORDER BY
        r.subject_id,
        CASE WHEN btrim(COALESCE(r.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
        r.rel_order NULLS LAST,
        r.place_type_id
),
parent_map AS (
    SELECT
        sr.parent_subject_id
    FROM tgn.subject_rels sr
    WHERE sr.child_subject_id = in_id
      AND sr.parent_subject_id IS NOT NULL
    ORDER BY
        CASE WHEN btrim(COALESCE(sr.historic_flag, '')) = 'H' THEN 1 ELSE 0 END,
        CASE WHEN btrim(COALESCE(sr.hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END,
        CASE WHEN btrim(COALESCE(sr.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
        sr.subject_rel_id
    LIMIT 1
),
coord_map AS (
    SELECT
        COALESCE(c.lat_decimal_derived, c.lat_decimal_raw) AS lat,
        COALESCE(c.lon_decimal_derived, c.lon_decimal_raw) AS lon
    FROM tgn.coordinates c
    WHERE c.subject_id = in_id
    ORDER BY c.coordinates_id
    LIMIT 1
),
ancestors AS (
    SELECT
        b.tgn_id,
        p.parent_subject_id AS ancestor_id,
        1 AS depth,
        ARRAY[b.tgn_id, p.parent_subject_id]::bigint[] AS path
    FROM base b
    JOIN LATERAL (
        SELECT sr.parent_subject_id
        FROM tgn.subject_rels sr
        WHERE sr.child_subject_id = b.tgn_id
          AND sr.parent_subject_id IS NOT NULL
        ORDER BY
            CASE WHEN btrim(COALESCE(sr.historic_flag, '')) = 'H' THEN 1 ELSE 0 END,
            CASE WHEN btrim(COALESCE(sr.hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END,
            CASE WHEN btrim(COALESCE(sr.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
            sr.subject_rel_id
        LIMIT 1
    ) p ON TRUE
    WHERE p.parent_subject_id IS NOT NULL
      AND p.parent_subject_id <> 7029392

    UNION ALL

    SELECT
        a.tgn_id,
        p.parent_subject_id AS ancestor_id,
        a.depth + 1 AS depth,
        a.path || p.parent_subject_id
    FROM ancestors a
    JOIN LATERAL (
        SELECT sr.parent_subject_id
        FROM tgn.subject_rels sr
        WHERE sr.child_subject_id = a.ancestor_id
          AND sr.parent_subject_id IS NOT NULL
        ORDER BY
            CASE WHEN btrim(COALESCE(sr.historic_flag, '')) = 'H' THEN 1 ELSE 0 END,
            CASE WHEN btrim(COALESCE(sr.hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END,
            CASE WHEN btrim(COALESCE(sr.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
            sr.subject_rel_id
        LIMIT 1
    ) p ON TRUE
    WHERE p.parent_subject_id IS NOT NULL
      AND a.depth < 50
      AND p.parent_subject_id <> 7029392
      AND NOT (p.parent_subject_id = ANY(a.path))
),
ancestor_dedup AS (
    SELECT
        tgn_id,
        ancestor_id,
        MIN(depth) AS min_depth
    FROM ancestors
    GROUP BY tgn_id, ancestor_id
),
ancestor_named AS (
    SELECT
        ad.tgn_id,
        ad.ancestor_id,
        ad.min_depth,
        name.term_text AS ancestor_name,
        apt.place_type_id AS ancestor_place_type_id,
        apt.place_type_label AS ancestor_place_type_label
    FROM ancestor_dedup ad
    LEFT JOIN LATERAL (
        SELECT t.term_text
        FROM tgn.term t
        WHERE t.subject_id = ad.ancestor_id
        ORDER BY
            CASE WHEN btrim(COALESCE(t.term_type, '')) = 'P' THEN 0 ELSE 1 END,
            t.display_order NULLS LAST,
            t.term_id
        LIMIT 1
    ) name ON TRUE
    LEFT JOIN LATERAL (
        SELECT
            r.place_type_id,
            pt.place_type_label
        FROM tgn.place_type_rels r
        LEFT JOIN tgn.place_type pt
            ON pt.place_type_id = r.place_type_id
        WHERE r.subject_id = ad.ancestor_id
        ORDER BY
            CASE WHEN btrim(COALESCE(r.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
            r.rel_order NULLS LAST,
            r.place_type_id
        LIMIT 1
    ) apt ON TRUE
),
ancestor_ranked AS (
    SELECT
        tgn_id,
        ancestor_id,
        ancestor_name,
        ancestor_place_type_id,
        ancestor_place_type_label,
        min_depth,
        ROW_NUMBER() OVER (PARTITION BY tgn_id ORDER BY min_depth, ancestor_id) AS rn
    FROM ancestor_named
),
ancestor_agg AS (
    SELECT
        tgn_id,
        ARRAY_AGG(ancestor_id ORDER BY min_depth, ancestor_id) AS ancestor_ids,
        ARRAY_AGG(COALESCE(ancestor_name, ancestor_id::text) ORDER BY min_depth, ancestor_id) AS ancestor_names,
        ARRAY_AGG(ancestor_place_type_id ORDER BY min_depth, ancestor_id) AS ancestor_place_type_ids,
        ARRAY_AGG(ancestor_place_type_label ORDER BY min_depth, ancestor_id) AS ancestor_place_type_labels
    FROM ancestor_ranked
    GROUP BY tgn_id
)
SELECT
    b.tgn_id,
    tr.matched_term,
    tr.preferred_term,
    pt.place_type_id,
    pt.place_type_label,
    NULL::double precision AS score,
    pm.parent_subject_id,
    cm.lat,
    cm.lon,
    COALESCE(
        (
            WITH trimmed AS (
                SELECT
                    COALESCE(
                        CASE
                            WHEN aa.ancestor_ids IS NULL THEN ARRAY[]::bigint[]
                            WHEN array_position(aa.ancestor_ids, 7029392) IS NULL THEN aa.ancestor_ids
                            WHEN array_position(aa.ancestor_ids, 7029392) = 1 THEN ARRAY[]::bigint[]
                            ELSE aa.ancestor_ids[1:array_position(aa.ancestor_ids, 7029392)-1]
                        END,
                        ARRAY[]::bigint[]
                    ) AS ids,
                    COALESCE(
                        CASE
                            WHEN aa.ancestor_names IS NULL THEN ARRAY[]::text[]
                            WHEN array_position(aa.ancestor_ids, 7029392) IS NULL THEN aa.ancestor_names
                            WHEN array_position(aa.ancestor_ids, 7029392) = 1 THEN ARRAY[]::text[]
                            ELSE aa.ancestor_names[1:array_position(aa.ancestor_ids, 7029392)-1]
                        END,
                        ARRAY[]::text[]
                    ) AS names,
                    COALESCE(
                        CASE
                            WHEN aa.ancestor_place_type_ids IS NULL THEN ARRAY[]::bigint[]
                            WHEN array_position(aa.ancestor_ids, 7029392) IS NULL THEN aa.ancestor_place_type_ids
                            WHEN array_position(aa.ancestor_ids, 7029392) = 1 THEN ARRAY[]::bigint[]
                            ELSE aa.ancestor_place_type_ids[1:array_position(aa.ancestor_ids, 7029392)-1]
                        END,
                        ARRAY[]::bigint[]
                    ) AS place_type_ids,
                    COALESCE(
                        CASE
                            WHEN aa.ancestor_place_type_labels IS NULL THEN ARRAY[]::text[]
                            WHEN array_position(aa.ancestor_ids, 7029392) IS NULL THEN aa.ancestor_place_type_labels
                            WHEN array_position(aa.ancestor_ids, 7029392) = 1 THEN ARRAY[]::text[]
                            ELSE aa.ancestor_place_type_labels[1:array_position(aa.ancestor_ids, 7029392)-1]
                        END,
                        ARRAY[]::text[]
                    ) AS place_type_labels
            )
            SELECT COALESCE(
                (
                    SELECT jsonb_agg(
                        jsonb_build_array(
                            ids[idx],
                            names[idx],
                            place_type_ids[idx],
                            place_type_labels[idx]
                        )
                        ORDER BY idx
                    )
                    FROM trimmed, generate_subscripts(trimmed.ids, 1) AS idx
                ),
                '[]'::jsonb
            )
        ),
        '[]'::jsonb
    ) AS ancestor_pairs,
    COALESCE(alt.alternate_names, '[]'::jsonb) AS alternate_names
FROM base b
LEFT JOIN terms tr ON tr.tgn_id = b.tgn_id
LEFT JOIN place_type_map pt ON pt.subject_id = b.tgn_id
LEFT JOIN parent_map pm ON TRUE
LEFT JOIN coord_map cm ON TRUE
LEFT JOIN ancestor_agg aa ON aa.tgn_id = b.tgn_id
LEFT JOIN LATERAL (
    SELECT jsonb_agg(name.term_text ORDER BY name.sort_group, name.display_order, name.term_id) AS alternate_names
    FROM (
        SELECT DISTINCT ON (t.term_text)
            t.term_text,
            CASE WHEN btrim(COALESCE(t.term_type, '')) = 'P' THEN 0 ELSE 1 END AS sort_group,
            t.display_order,
            t.term_id
        FROM tgn.term t
        WHERE t.subject_id = b.tgn_id
        ORDER BY t.term_text, sort_group, t.display_order NULLS LAST, t.term_id
    ) name
) alt ON TRUE
WHERE EXISTS (SELECT 1 FROM tgn.subject s WHERE s.subject_id = b.tgn_id);
$$;
