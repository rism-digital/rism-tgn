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
    ancestor_pairs jsonb
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
        t.subject_id,
        t.term_id,
        t.term_text,
        t.term_norm,
        t.term_type,
        t.preferred_flag,
        t.historic_flag,
        1.0::double precision AS sim,
        TRUE AS is_exact
    FROM tgn.term t
    CROSS JOIN input i
    WHERE t.term_norm = i.q

    UNION ALL

    SELECT
        t.subject_id,
        t.term_id,
        t.term_text,
        t.term_norm,
        t.term_type,
        t.preferred_flag,
        t.historic_flag,
        1.0::double precision AS sim,
        TRUE AS is_exact
    FROM tgn.term t
    JOIN tokens tok
        ON t.term_norm = tok.token
),
fuzzy_candidates AS (
    SELECT
        t.subject_id,
        t.term_id,
        t.term_text,
        t.term_norm,
        t.term_type,
        t.preferred_flag,
        t.historic_flag,
        similarity(t.term_norm, i.q)::double precision AS sim
    FROM tgn.term t
    CROSS JOIN input i
    WHERE t.term_norm % i.q
      AND t.term_norm <> i.q

    UNION ALL

    SELECT
        t.subject_id,
        t.term_id,
        t.term_text,
        t.term_norm,
        t.term_type,
        t.preferred_flag,
        t.historic_flag,
        similarity(t.term_norm, tok.token)::double precision AS sim
    FROM tgn.term t
    JOIN tokens tok
        ON t.term_norm % tok.token
       AND t.term_norm <> tok.token
),
fuzzy_pool AS (
    SELECT
        subject_id,
        term_id,
        term_text,
        term_norm,
        term_type,
        preferred_flag,
        historic_flag,
        sim,
        FALSE AS is_exact
    FROM (
        SELECT
            fc.*,
            ROW_NUMBER() OVER (PARTITION BY fc.term_id ORDER BY fc.sim DESC) AS rn
        FROM fuzzy_candidates fc
    ) d
    WHERE rn = 1
    ORDER BY sim DESC
    LIMIT GREATEST(COALESCE(limit_n, 10) * 80, 400)
),
candidate_pool AS (
    SELECT * FROM exact_pool
    UNION ALL
    SELECT * FROM fuzzy_pool
),
ranked_terms AS (
    SELECT
        cp.*,
        ROW_NUMBER() OVER (
            PARTITION BY cp.subject_id
            ORDER BY
                CASE WHEN cp.is_exact THEN 0 ELSE 1 END,
                cp.sim DESC,
                CASE WHEN btrim(COALESCE(cp.term_type, '')) = 'P' THEN 0 ELSE 1 END,
                cp.term_id
        ) AS rn
    FROM candidate_pool cp
),
subject_best AS (
    SELECT
        subject_id,
        term_id,
        term_text,
        term_norm,
        term_type,
        regexp_replace(term_norm, '[^a-z0-9]+', ' ', 'g') AS term_clean,
        preferred_flag,
        historic_flag,
        sim,
        is_exact
    FROM ranked_terms
    WHERE rn = 1
),
ancestor_context AS (
    SELECT
        sb.subject_id,
        COALESCE(string_agg(ap.term_norm, ' ' ORDER BY ap.depth, ap.ancestor_id), '') AS ancestor_blob
    FROM subject_best sb
    LEFT JOIN LATERAL (
        WITH RECURSIVE anc AS (
            SELECT
                p.parent_subject_id AS ancestor_id,
                1 AS depth,
                ARRAY[sb.subject_id, p.parent_subject_id]::bigint[] AS path
            FROM LATERAL (
                SELECT sr.parent_subject_id
                FROM tgn.subject_rels sr
                WHERE sr.child_subject_id = sb.subject_id
                  AND sr.parent_subject_id IS NOT NULL
                ORDER BY
                    CASE WHEN btrim(COALESCE(sr.historic_flag, '')) = 'H' THEN 1 ELSE 0 END,
                    CASE WHEN btrim(COALESCE(sr.hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END,
                    CASE WHEN btrim(COALESCE(sr.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
                    sr.subject_rel_id
                LIMIT 1
            ) p
            WHERE p.parent_subject_id IS NOT NULL
              AND p.parent_subject_id <> 7029392

            UNION ALL

            SELECT
                p.parent_subject_id AS ancestor_id,
                a.depth + 1 AS depth,
                a.path || p.parent_subject_id
            FROM anc a
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
        anc_pref AS (
            SELECT DISTINCT ON (a.ancestor_id)
                a.ancestor_id,
                a.depth,
                t.term_norm
            FROM anc a
            LEFT JOIN LATERAL (
                SELECT tr.term_norm
                FROM tgn.term tr
                WHERE tr.subject_id = a.ancestor_id
                ORDER BY
                    CASE WHEN btrim(COALESCE(tr.term_type, '')) = 'P' THEN 0 ELSE 1 END,
                    tr.display_order NULLS LAST,
                    tr.term_id
                LIMIT 1
            ) t ON TRUE
            WHERE t.term_norm IS NOT NULL
            ORDER BY a.ancestor_id, a.depth
        )
        SELECT ancestor_id, depth, term_norm
        FROM anc_pref
    ) ap ON TRUE
    GROUP BY sb.subject_id
),
token_hits AS (
    SELECT
        sb.subject_id,
        COUNT(tok.token)::integer AS token_count,
        COALESCE(SUM(CASE WHEN POSITION(tok.token IN sb.term_clean) > 0 THEN 1 ELSE 0 END), 0)::integer AS term_hits,
        COALESCE(SUM(CASE WHEN ac.ancestor_blob <> '' AND POSITION(tok.token IN ac.ancestor_blob) > 0 THEN 1 ELSE 0 END), 0)::integer AS ancestor_hits,
        COALESCE(SUM(CASE
            WHEN POSITION(tok.token IN sb.term_clean) > 0
              OR (ac.ancestor_blob <> '' AND POSITION(tok.token IN ac.ancestor_blob) > 0)
            THEN 1 ELSE 0
        END), 0)::integer AS coverage_hits
    FROM subject_best sb
    LEFT JOIN ancestor_context ac
        ON ac.subject_id = sb.subject_id
    LEFT JOIN tokens tok
        ON TRUE
    GROUP BY sb.subject_id, ac.ancestor_blob
),
scored AS (
    SELECT
        sb.subject_id AS tgn_id,
        sb.term_text AS matched_term,
        COALESCE(pref.preferred_term, sb.term_text) AS preferred_term,
        ptm.place_type_id,
        ptm.place_type_label,
        pm.parent_subject_id,
        COALESCE(coord.lat_decimal_derived, coord.lat_decimal_raw) AS lat,
        COALESCE(coord.lon_decimal_derived, coord.lon_decimal_raw) AS lon,
        CASE WHEN sb.term_clean = i.q_clean THEN 1 ELSE 0 END AS rank_term_exact,
        CASE
            WHEN trim(regexp_replace(lower(unaccent(COALESCE(pref.preferred_term, sb.term_text))), '[^a-z0-9]+', ' ', 'g')) = i.q_clean
            THEN 1 ELSE 0
        END AS rank_pref_exact,
        GREATEST((
            CASE WHEN sb.is_exact THEN 1.0 ELSE 0.0 END
            + CASE WHEN btrim(COALESCE(sb.term_type, '')) = 'P' THEN 0.35 ELSE 0.0 END
            + CASE WHEN sb.historic_flag = 'H' THEN -0.10 ELSE 0.0 END
            + CASE WHEN context_parent_id IS NOT NULL AND pm.parent_subject_id = context_parent_id THEN 0.30 ELSE 0.0 END
            + CASE WHEN context_country_id IS NOT NULL AND (sb.subject_id = context_country_id OR pm.parent_subject_id = context_country_id) THEN 0.20 ELSE 0.0 END
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
    LEFT JOIN LATERAL (
        SELECT t.term_text AS preferred_term
        FROM tgn.term t
        WHERE t.subject_id = sb.subject_id
        ORDER BY
            CASE WHEN btrim(COALESCE(t.term_type, '')) = 'P' THEN 0 ELSE 1 END,
            t.display_order NULLS LAST,
            t.term_id
        LIMIT 1
    ) pref ON TRUE
    LEFT JOIN LATERAL (
        SELECT sr.parent_subject_id
        FROM tgn.subject_rels sr
        WHERE sr.child_subject_id = sb.subject_id
          AND sr.parent_subject_id IS NOT NULL
        ORDER BY
            CASE WHEN btrim(COALESCE(sr.historic_flag, '')) = 'H' THEN 1 ELSE 0 END,
            CASE WHEN btrim(COALESCE(sr.hierarchy_flag, '')) = 'P' THEN 0 ELSE 1 END,
            CASE WHEN btrim(COALESCE(sr.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
            sr.subject_rel_id
        LIMIT 1
    ) pm ON TRUE
    LEFT JOIN LATERAL (
        SELECT
            r.place_type_id,
            pt.place_type_label
        FROM tgn.place_type_rels r
        LEFT JOIN tgn.place_type pt
            ON pt.place_type_id = r.place_type_id
        WHERE r.subject_id = sb.subject_id
        ORDER BY
            CASE WHEN btrim(COALESCE(r.preferred_flag, '')) = 'P' THEN 0 ELSE 1 END,
            r.rel_order NULLS LAST,
            r.place_type_id
        LIMIT 1
    ) ptm ON TRUE
    LEFT JOIN LATERAL (
        SELECT
            c.lat_decimal_derived,
            c.lat_decimal_raw,
            c.lon_decimal_derived,
            c.lon_decimal_raw
        FROM tgn.coordinates c
        WHERE c.subject_id = sb.subject_id
        ORDER BY c.coordinates_id
        LIMIT 1
    ) coord ON TRUE
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
        pt.term_text AS ancestor_name
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
),
ancestor_ranked AS (
    SELECT
        tgn_id,
        ancestor_id,
        min_depth,
        ancestor_name,
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
        ARRAY_AGG(COALESCE(ancestor_name, ancestor_id::text) ORDER BY min_depth, ancestor_id) AS ancestor_names
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
                    ) AS names
            )
            SELECT COALESCE(
                (
                    SELECT jsonb_agg(jsonb_build_array(ids[idx], names[idx]) ORDER BY idx)
                    FROM trimmed, generate_subscripts(trimmed.ids, 1) AS idx
                ),
                '[]'::jsonb
            )
        ),
        '[]'::jsonb
    ) AS ancestor_pairs
FROM best_matches bm
LEFT JOIN ancestor_agg aa
    ON aa.tgn_id = bm.tgn_id
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
    ancestor_pairs jsonb
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
        name.term_text AS ancestor_name
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
),
ancestor_ranked AS (
    SELECT
        tgn_id,
        ancestor_id,
        ancestor_name,
        min_depth,
        ROW_NUMBER() OVER (PARTITION BY tgn_id ORDER BY min_depth, ancestor_id) AS rn
    FROM ancestor_named
),
ancestor_agg AS (
    SELECT
        tgn_id,
        ARRAY_AGG(ancestor_id ORDER BY min_depth, ancestor_id) AS ancestor_ids,
        ARRAY_AGG(COALESCE(ancestor_name, ancestor_id::text) ORDER BY min_depth, ancestor_id) AS ancestor_names
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
                    ) AS names
            )
            SELECT COALESCE(
                (
                    SELECT jsonb_agg(jsonb_build_array(ids[idx], names[idx]) ORDER BY idx)
                    FROM trimmed, generate_subscripts(trimmed.ids, 1) AS idx
                ),
                '[]'::jsonb
            )
        ),
        '[]'::jsonb
    ) AS ancestor_pairs
FROM base b
LEFT JOIN terms tr ON tr.tgn_id = b.tgn_id
LEFT JOIN place_type_map pt ON pt.subject_id = b.tgn_id
LEFT JOIN parent_map pm ON TRUE
LEFT JOIN coord_map cm ON TRUE
LEFT JOIN ancestor_agg aa ON aa.tgn_id = b.tgn_id
WHERE EXISTS (SELECT 1 FROM tgn.subject s WHERE s.subject_id = b.tgn_id);
$$;
