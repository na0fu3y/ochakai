-- How close the concepts in a base sit to each other, as a distribution
-- and as the pairs behind it.
--
--     psql "$OCHAKAI_DATABASE_URL" -f scripts/duplicate-distances.sql
--
-- Read-only: it takes no locks a reader does not, writes nothing, and
-- adds nothing to any surface. Run it on a base you actually curate — a
-- demo bundle has too few concepts to have a distribution.
--
-- WHY THIS EXISTS. "Return one of the near-duplicates instead of five"
-- sounds like a feature until someone has to name the distance at which
-- two concepts stop being two concepts. Nobody can name it from an
-- armchair: on one base 0.12 is a paraphrase, on another it is two
-- metrics that share a domain. So the number comes first, and the
-- mechanism — a queue a human rules on, never a silent filter at search
-- time — comes only if the number says there is a boundary to draw.
--
-- WHAT TO LOOK FOR, in this order:
--
--   1. Does the histogram have a bump near zero, separated from the
--      rest? A single smooth slope means "duplicate" and "related" are
--      the same thing here, and no threshold can tell them apart. That
--      is a finding: it says do not build the queue.
--   2. Read the pairs. The distance where you stop saying "these are the
--      same knowledge" and start saying "these belong near each other"
--      is the only definition of the threshold that means anything.
--   3. Compare the two ends. If the closest pair in the base is at 0.3,
--      there is nothing to clean up and the queue would open empty.
--
-- It measures concepts only. File vectors are keyed by path and answer a
-- different question (design doc 0091).

\pset border 2
\timing off
\set ON_ERROR_STOP on

-- A base with no vectors is an ordinary deployment, not a failure: the
-- lexical half of search works without them (design doc 0080 §5). Say so
-- once and stop, rather than letting every query below repeat it.
SELECT to_regclass('knowledge_embedding') IS NULL AS absent \gset
\if :absent
\echo 'This base has no vectors — semantic search was never configured here'
\echo '(OCHAKAI_EMBEDDINGS; design doc 0080 §1). Nothing to measure.'
\quit
\endif

-- Every embedded live concept, under the model the base actually uses.
-- A base mid-reembed holds two models at once and comparing across them
-- is meaningless — vectors from different models live in different
-- spaces (design doc 0080 §3) — so the busiest one wins and the count
-- below says how many were left out.
CREATE TEMPORARY VIEW embedded AS
SELECT e.id, e.embedding, k.title, k.type
  FROM knowledge_embedding e
  JOIN object k ON k.id = e.id
 WHERE k.deleted_at IS NULL
   AND e.model = (SELECT model FROM knowledge_embedding
                  GROUP BY model ORDER BY count(*) DESC LIMIT 1);

-- Two concepts is the arithmetic minimum; fifty is where a histogram
-- starts being a shape rather than a handful of bars. Below that the
-- pairs are still worth reading and the distribution is not.
SELECT (SELECT count(*) FROM embedded) < 2 AS too_few,
       (SELECT count(*) FROM embedded) < 50 AS small \gset
\if :too_few
\echo 'Fewer than two embedded concepts. Nothing to compare.'
\quit
\endif
\if :small
\echo 'NOTE: under 50 concepts. Read the pairs; the histogram is too few'
\echo 'bars to be a shape, and a threshold read off it would be noise.'
\echo ''
\endif

\echo '== what is being measured'
SELECT (SELECT count(*) FROM embedded)                        AS concepts,
       (SELECT count(*) FROM knowledge_embedding)             AS vectors_all_models,
       (SELECT model FROM knowledge_embedding
         GROUP BY model ORDER BY count(*) DESC LIMIT 1)       AS model;

-- Each concept's nearest neighbour. Cosine distance, which is what
-- search ranks on: 0 is the same direction, 1 is unrelated, 2 is
-- opposite.
CREATE TEMPORARY VIEW nearest AS
SELECT a.id AS a_id, a.title AS a_title, a.type AS a_type,
       n.id AS b_id, n.title AS b_title, n.type AS b_type,
       n.dist
  FROM embedded a
 CROSS JOIN LATERAL (
   SELECT b.id, b.title, b.type, a.embedding <=> b.embedding AS dist
     FROM embedded b
    WHERE b.id <> a.id
    ORDER BY a.embedding <=> b.embedding
    LIMIT 1
 ) n;

\echo ''
\echo '== nearest-neighbour distance, as a shape'
\echo '   a bump near zero that is separated from the rest is a threshold;'
\echo '   one smooth slope is the finding that there is not one'
-- Empty buckets are printed, not skipped: the gap between a cluster of
-- paraphrases and the rest of the base is the whole signal, and a gap
-- that shows as missing rows is one a reader has to notice rather than
-- see.
WITH b AS (SELECT generate_series(1, 20) * 0.05 AS bucket),
     n AS (SELECT width_bucket(dist, 0, 1, 20) * 0.05 AS bucket FROM nearest)
SELECT b.bucket AS "under", count(n.bucket) AS concepts,
       repeat('#', (count(n.bucket) * 50
              / greatest(max(count(n.bucket)) OVER (), 1))::int) AS shape
  FROM b LEFT JOIN n ON n.bucket = b.bucket
 GROUP BY b.bucket
 ORDER BY b.bucket;

\echo ''
\echo '== how many concepts have a neighbour closer than …'
\echo '   the queue this would feed is this many rows deep'
SELECT t AS "under",
       (SELECT count(*) FROM nearest WHERE dist < t) AS concepts,
       round(100.0 * (SELECT count(*) FROM nearest WHERE dist < t)
             / greatest((SELECT count(*) FROM nearest), 1), 1) AS "%"
  FROM unnest(ARRAY[0.02, 0.05, 0.10, 0.15, 0.20, 0.30]) AS t;

\echo ''
\echo '== the closest 40 pairs — this is the part to read'
\echo '   somewhere in this list they stop being the same knowledge;'
\echo '   that line is the threshold, and nothing else is'
SELECT round(dist::numeric, 4) AS dist,
       least(a_id, b_id)    AS concept_a,
       greatest(a_id, b_id) AS concept_b,
       CASE WHEN a_type = b_type THEN a_type::text
            ELSE a_type || ' / ' || b_type END AS types,
       left(CASE WHEN a_id < b_id THEN a_title ELSE b_title END, 40) AS title_a,
       left(CASE WHEN a_id < b_id THEN b_title ELSE a_title END, 40) AS title_b
  FROM (SELECT DISTINCT ON (least(a_id, b_id), greatest(a_id, b_id))
               a_id, b_id, a_title, b_title, a_type, b_type, dist
          FROM nearest
         ORDER BY least(a_id, b_id), greatest(a_id, b_id), dist) p
 ORDER BY dist
 LIMIT 40;
