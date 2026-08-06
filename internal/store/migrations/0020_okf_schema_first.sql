-- OKF v0.2's provenance (SPEC §5.1) and Attested Computation (SPEC §10.2)
-- families move out of attrs and into the envelope (design doc 0036).
--
-- The v0.2 work this follows kept them in attrs under a server-centered
-- rule: only what ochakai itself observes or matches earns a column.
-- Design doc 0036 §2.3 replaces that with a spec-centered one — the keys OKF defines are
-- fields, because the consumers who read them are the bundle's readers and
-- the humans editing them, not this server. Holding executor and attester
-- is still not running them (0081 stands: no LLM, no SQL execution).
--
-- One column per family: jsonb where the shape is structured (the links /
-- attrs precedent), text where the spec's value is a scalar. A single blob
-- would give back exactly the queryability the promotion was for.
--
-- The backfill moves values out of attrs AND deletes the keys. Leaving
-- them would double them on the wire: reservedKeys suppresses an attr only
-- on OKF export, so a REST read, the MCP payload, and the Web UI's
-- attributes tab would each show the value twice, and the next full-
-- replacement PUT would write the duplicate back for good.
--
-- updated_at is deliberately untouched. It is the optimistic-locking
-- version (design doc 0030) and OKF's generated.at (0036 §3.2): moving a
-- value between columns is not the content meaningfully changing, and
-- bumping it would invalidate every held ETag and misdate every entry.
-- Revision snapshots are left alone for the same reason — history keeps
-- the shape it had when it was written.
--
-- No index. Nothing queries by source yet; the reverse lookup design doc
-- 0036 §5 names ("entries derived from this material") is still unbuilt,
-- and a GIN index lands with it.

ALTER TABLE knowledge
    ADD COLUMN IF NOT EXISTS sources      jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS usage_window jsonb,
    ADD COLUMN IF NOT EXISTS runtime      text  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS parameters   jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS computation  text  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS executor     jsonb,
    ADD COLUMN IF NOT EXISTS attester     jsonb;

-- sources. Only rows whose attrs.sources is a list of mappings are
-- converted; anything else is left completely alone, attr included, so a
-- malformed value stays visible to a human instead of being half-read.
-- A source with no resource is dropped: SPEC §5.1 requires one, and an
-- entry that names no artifact is not a citation.
--
-- YAML types an unquoted date, so a date that passed through attrs before
-- this migration came back as a full timestamp (0036 §3.7). left(...,10)
-- returns the day the producer actually wrote.
UPDATE knowledge SET sources = COALESCE((
    SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
        'resource',     s->>'resource',
        'id',           NULLIF(s->>'id', ''),
        'title',        NULLIF(s->>'title', ''),
        'author',       NULLIF(s->>'author', ''),
        'usage_count',  CASE WHEN jsonb_typeof(s->'usage_count') = 'number' THEN s->'usage_count' END,
        'last_modified', CASE WHEN s->>'last_modified' ~ '^\d{4}-\d{2}-\d{2}'
                              THEN left(s->>'last_modified', 10) END,
        'usage_window', CASE WHEN jsonb_typeof(s->'usage_window') = 'object' THEN jsonb_strip_nulls(jsonb_build_object(
                                 'from', CASE WHEN s->'usage_window'->>'from' ~ '^\d{4}-\d{2}-\d{2}'
                                              THEN left(s->'usage_window'->>'from', 10) END,
                                 'to',   CASE WHEN s->'usage_window'->>'to' ~ '^\d{4}-\d{2}-\d{2}'
                                              THEN left(s->'usage_window'->>'to', 10) END)) END
    )) ORDER BY ord)
    FROM jsonb_array_elements(attrs->'sources') WITH ORDINALITY AS t(s, ord)
    WHERE COALESCE(s->>'resource', '') <> ''), '[]'::jsonb)
WHERE jsonb_typeof(attrs->'sources') = 'array'
  AND NOT EXISTS (
      SELECT 1 FROM jsonb_array_elements(attrs->'sources') AS t(s)
      WHERE jsonb_typeof(s) <> 'object');

UPDATE knowledge SET usage_window = jsonb_strip_nulls(jsonb_build_object(
    'from', CASE WHEN attrs->'usage_window'->>'from' ~ '^\d{4}-\d{2}-\d{2}'
                 THEN left(attrs->'usage_window'->>'from', 10) END,
    'to',   CASE WHEN attrs->'usage_window'->>'to' ~ '^\d{4}-\d{2}-\d{2}'
                 THEN left(attrs->'usage_window'->>'to', 10) END))
WHERE jsonb_typeof(attrs->'usage_window') = 'object';

UPDATE knowledge SET runtime = attrs->>'runtime'
WHERE jsonb_typeof(attrs->'runtime') = 'string';

UPDATE knowledge SET computation = attrs->>'computation'
WHERE jsonb_typeof(attrs->'computation') = 'string';

-- parameters. name and type are required within an entry (SPEC §10.2), so
-- an element missing either is dropped rather than stored half-formed.
UPDATE knowledge SET parameters = COALESCE((
    SELECT jsonb_agg(jsonb_build_object(
        'name',     p->>'name',
        'type',     p->>'type',
        'required', COALESCE((p->>'required')::boolean, false)
    ) ORDER BY ord)
    FROM jsonb_array_elements(attrs->'parameters') WITH ORDINALITY AS t(p, ord)
    WHERE COALESCE(p->>'name', '') <> '' AND COALESCE(p->>'type', '') <> ''), '[]'::jsonb)
WHERE jsonb_typeof(attrs->'parameters') = 'array'
  AND NOT EXISTS (
      SELECT 1 FROM jsonb_array_elements(attrs->'parameters') AS t(p)
      WHERE jsonb_typeof(p) <> 'object'
         OR (p->>'required') IS NOT NULL AND jsonb_typeof(p->'required') <> 'boolean');

-- executor / attester. Both of the executor's keys are required within it
-- (SPEC §10.2); a half-stated executor is dropped so nothing downstream
-- reads it as a complete contract.
UPDATE knowledge SET executor = jsonb_build_object(
    'resource', attrs->'executor'->>'resource',
    'receipt',  attrs->'executor'->'receipt')
WHERE jsonb_typeof(attrs->'executor') = 'object'
  AND COALESCE(attrs->'executor'->>'resource', '') <> ''
  AND jsonb_typeof(attrs->'executor'->'receipt') = 'array';

UPDATE knowledge SET attester = jsonb_build_object('resource', attrs->'attester'->>'resource')
WHERE jsonb_typeof(attrs->'attester') = 'object'
  AND COALESCE(attrs->'attester'->>'resource', '') <> '';

-- Strip every key that now has a column, but only where the column
-- actually took the value — a family left untouched above keeps its attr,
-- which is the whole point of the guards.
UPDATE knowledge SET attrs = attrs - 'sources'
WHERE attrs ? 'sources' AND sources <> '[]'::jsonb;
UPDATE knowledge SET attrs = attrs - 'usage_window'
WHERE attrs ? 'usage_window' AND usage_window IS NOT NULL;
UPDATE knowledge SET attrs = attrs - 'runtime'
WHERE attrs ? 'runtime' AND runtime <> '';
UPDATE knowledge SET attrs = attrs - 'parameters'
WHERE attrs ? 'parameters' AND parameters <> '[]'::jsonb;
UPDATE knowledge SET attrs = attrs - 'computation'
WHERE attrs ? 'computation' AND computation <> '';
UPDATE knowledge SET attrs = attrs - 'executor'
WHERE attrs ? 'executor' AND executor IS NOT NULL;
UPDATE knowledge SET attrs = attrs - 'attester'
WHERE attrs ? 'attester' AND attester IS NOT NULL;
