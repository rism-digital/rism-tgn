WITH counts AS (
    SELECT 'subject'::text AS table_name, count(*)::bigint AS row_count FROM tgn.subject
    UNION ALL SELECT 'term', count(*)::bigint FROM tgn.term
    UNION ALL SELECT 'language_rels', count(*)::bigint FROM tgn.language_rels
    UNION ALL SELECT 'subject_rels', count(*)::bigint FROM tgn.subject_rels
    UNION ALL SELECT 'subject_merge', count(*)::bigint FROM tgn.subject_merge
    UNION ALL SELECT 'place_type', count(*)::bigint FROM tgn.place_type
    UNION ALL SELECT 'place_type_rels', count(*)::bigint FROM tgn.place_type_rels
    UNION ALL SELECT 'coordinates', count(*)::bigint FROM tgn.coordinates
)
SELECT * FROM counts ORDER BY table_name;

SELECT
    count(*) AS orphan_terms
FROM tgn.term t
LEFT JOIN tgn.subject s ON s.subject_id = t.subject_id
WHERE s.subject_id IS NULL;

SELECT
    count(*) AS orphan_coordinates
FROM tgn.coordinates c
LEFT JOIN tgn.subject s ON s.subject_id = c.subject_id
WHERE s.subject_id IS NULL;
