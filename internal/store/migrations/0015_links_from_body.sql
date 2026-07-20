-- Links become a value derived from the body (design doc 0024). Stored
-- links were authored as a field, so they exist independently of the
-- prose; from now on only the body's markdown links are read. Without
-- this migration those edges would vanish silently the next time an
-- entry is written.
--
-- So: write them back into the body first, then re-derive the column
-- from what was written. Each edge becomes "- [<rel>](/<target>.md)" —
-- the old rel becomes the anchor text, which is exactly the reading
-- 0024 gives to text. An empty rel falls back to the target's last
-- segment, so no link renders as an empty "[]()".
--
-- Revision snapshots are left alone: history should keep the shape it
-- had, and unlike the ids 0013 rewrote, links are not a key.

UPDATE knowledge SET
        body = CASE WHEN COALESCE(btrim(body), '') = '' THEN '' ELSE btrim(body) || E'\n\n' END
            || E'# Links\n\n'
            || (SELECT string_agg(
                    format('- [%s](/%s.md)',
                        CASE WHEN COALESCE(l->>'rel', '') <> '' THEN l->>'rel'
                             ELSE regexp_replace(regexp_replace(l->>'target', '^ochakai://', ''), '^.*/', '')
                        END,
                        regexp_replace(l->>'target', '^ochakai://', '')),
                    E'\n' ORDER BY ord)
                FROM jsonb_array_elements(links) WITH ORDINALITY AS t(l, ord)
                WHERE COALESCE(l->>'target', '') <> ''),
        links = (SELECT COALESCE(jsonb_agg(DISTINCT jsonb_build_object(
                    'target', regexp_replace(l->>'target', '^ochakai://', ''),
                    'text', CASE WHEN COALESCE(l->>'rel', '') <> '' THEN l->>'rel'
                                 ELSE regexp_replace(regexp_replace(l->>'target', '^ochakai://', ''), '^.*/', '')
                            END)), '[]'::jsonb)
                FROM jsonb_array_elements(links) AS t(l)
                WHERE COALESCE(l->>'target', '') <> ''),
        updated_at = now()
    WHERE deleted_at IS NULL
      AND jsonb_typeof(links) = 'array'
      AND EXISTS (SELECT 1 FROM jsonb_array_elements(links) AS t(l)
                  WHERE COALESCE(l->>'target', '') <> '');

-- Entries whose links array held nothing usable (all targets empty, or a
-- non-array left by an older write path) settle on the canonical empty
-- array so the containment queries behind backlinks stay well-typed.
UPDATE knowledge SET links = '[]'::jsonb
    WHERE jsonb_typeof(links) IS DISTINCT FROM 'array'
       OR (links <> '[]'::jsonb
           AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(links) AS t(l)
                           WHERE COALESCE(l->>'target', '') <> ''));
