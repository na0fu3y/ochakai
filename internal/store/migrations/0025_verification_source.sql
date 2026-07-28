-- A verification says what kind of confirmation it was (design doc 0045
-- §3.2).
--
-- 0043 §3.2 made a bundle's `verified` key raise a verification in the
-- importing actor's name. The name is right — provenance comes from
-- authentication (0002, 0009 §3.2) — but the row could not say whether
-- that actor had read the entry or had run one import over sixty-six of
-- them. Those are different acts, and the difference between them is the
-- thing this project claims to curate.
--
-- Empty is "reviewed here", which is what every existing row is: they
-- were written by a verify call against this instance, or migrated from
-- a status this instance stored. Non-empty names where a claim was
-- adopted from. No index — nothing looks entries up by source yet, and a
-- ledger this small does not need one to be read.

ALTER TABLE knowledge_verification
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT '';
