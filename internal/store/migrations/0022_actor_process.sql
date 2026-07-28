-- The actor kind ochakai writes for anything that is not a person becomes
-- "process", the spelling OKF SPEC §7 defines (design doc 0043 §3.8).
--
-- ochakai wrote "agent" from 0001 onward. It was never a spelling the
-- spec offered: §7 names human:<id>, process:<id>, and
-- <producer>/<version>, and §5.3 derives trust from the human: prefix
-- alone, lumping every other writer into one machine-confirmed tier. So
-- the distinction "agent" drew — an LLM agent rather than any other
-- automation — cost conformance and bought nothing: no query, no filter,
-- and no branch in the server ever read it.
--
-- Every column that stores an actor kind is rewritten, and so is every
-- "via" column, because a delegation is stored as the caller's whole
-- kind:name string (design doc 0027). Leaving via behind would make an
-- entry written through a delegating application say "process:tanaka via
-- agent:app" — half migrated, and wrong in the half that names the
-- machine.
--
-- updated_at is not touched anywhere here. The provenance spelling is not
-- the entry's content: no held ETag is invalidated, no generated.at is
-- misdated, and nothing needs re-embedding.

-- 1. The entry's own provenance. Four actors per row.
UPDATE knowledge SET
    created_by_kind  = CASE WHEN created_by_kind  = 'agent' THEN 'process' ELSE created_by_kind  END,
    updated_by_kind  = CASE WHEN updated_by_kind  = 'agent' THEN 'process' ELSE updated_by_kind  END,
    verified_by_kind = CASE WHEN verified_by_kind = 'agent' THEN 'process' ELSE verified_by_kind END,
    rejected_by_kind = CASE WHEN rejected_by_kind = 'agent' THEN 'process' ELSE rejected_by_kind END,
    created_by_via   = CASE WHEN created_by_via  LIKE 'agent:%' THEN 'process:' || substring(created_by_via  from 7) ELSE created_by_via  END,
    updated_by_via   = CASE WHEN updated_by_via  LIKE 'agent:%' THEN 'process:' || substring(updated_by_via  from 7) ELSE updated_by_via  END,
    verified_by_via  = CASE WHEN verified_by_via LIKE 'agent:%' THEN 'process:' || substring(verified_by_via from 7) ELSE verified_by_via END,
    rejected_by_via  = CASE WHEN rejected_by_via LIKE 'agent:%' THEN 'process:' || substring(rejected_by_via from 7) ELSE rejected_by_via END
WHERE created_by_kind = 'agent' OR updated_by_kind = 'agent'
   OR verified_by_kind = 'agent' OR rejected_by_kind = 'agent'
   OR created_by_via LIKE 'agent:%' OR updated_by_via LIKE 'agent:%'
   OR verified_by_via LIKE 'agent:%' OR rejected_by_via LIKE 'agent:%';

-- 2. History. The revision row's own actor columns, and the actors inside
--    the snapshot JSON — a revision read back must not show a spelling the
--    program no longer writes. Design doc 0043 §3.9 withdraws 0036 §3.13's
--    "history keeps the shape it had": a history whose shape depends on
--    knowing every past generation of the struct is the distortion, not
--    the fidelity.
UPDATE knowledge_revision SET
    changed_by_kind = CASE WHEN changed_by_kind = 'agent' THEN 'process' ELSE changed_by_kind END,
    changed_by_via  = CASE WHEN changed_by_via LIKE 'agent:%' THEN 'process:' || substring(changed_by_via from 7) ELSE changed_by_via END
WHERE changed_by_kind = 'agent' OR changed_by_via LIKE 'agent:%';

-- The snapshot is rewritten key by key rather than by a text replace on
-- the whole document: a body that quotes {"kind": "agent"} is content, and
-- content must survive a provenance migration untouched.
DO $$
DECLARE
    actor_key text;
BEGIN
    FOREACH actor_key IN ARRAY ARRAY['created_by', 'updated_by', 'verified_by', 'rejected_by'] LOOP
        EXECUTE format($fmt$
            UPDATE knowledge_revision
               SET snapshot = jsonb_set(snapshot, ARRAY[%1$L, 'kind'], '"process"')
             WHERE snapshot -> %1$L ->> 'kind' = 'agent'
        $fmt$, actor_key);

        EXECUTE format($fmt$
            UPDATE knowledge_revision
               SET snapshot = jsonb_set(snapshot, ARRAY[%1$L, 'via'],
                       to_jsonb('process:' || substring(snapshot -> %1$L ->> 'via' from 7)))
             WHERE snapshot -> %1$L ->> 'via' LIKE 'agent:%%'
        $fmt$, actor_key);
    END LOOP;
END $$;

-- 3. The remaining ledgers. knowledge_event has no via column by design
--    (0017): its actor is the caller as observed, with no delegation.
UPDATE knowledge_event SET actor_kind = 'process' WHERE actor_kind = 'agent';

UPDATE attachment SET created_by_kind = 'process' WHERE created_by_kind = 'agent';

UPDATE knowledge_purge SET
    purged_by_kind = CASE WHEN purged_by_kind = 'agent' THEN 'process' ELSE purged_by_kind END,
    purged_by_via  = CASE WHEN purged_by_via LIKE 'agent:%' THEN 'process:' || substring(purged_by_via from 7) ELSE purged_by_via END
WHERE purged_by_kind = 'agent' OR purged_by_via LIKE 'agent:%';
