-- The same gap, asked five ways, is one gap.
--
-- 0031 kept the query as it was asked, which is right — it is somebody's
-- own words, and provenance does not get tidied. What it also did was
-- group by those exact bytes, so "Revenue by channel", "revenue by
-- channel" and "revenue by channel?" are three rows in a list ten long
-- (stats.missQueryLimit). The one signal that says *what to write next*
-- was spending its ten slots on spellings of the same question, and the
-- reader could not see how much any of them was actually asked.
--
-- So the key moves off the bytes. normalized is what two askings have to
-- share to count as one question — case folded, width folded, inner
-- runs of space collapsed, edge punctuation dropped (domain.MissKey) —
-- and the query column keeps every asking exactly as typed. The list
-- still shows somebody's words; it groups by what they meant.
--
-- Existing rows are keyed by their own query text. Backfilling them
-- properly would mean a second implementation of MissKey in SQL, which
-- is the duplication the CJK class in 0036 needed a whole test to keep
-- honest; here it is not worth it, because misses are pruned at 180 days
-- (0029 §3.4) and the old rows group as they always did until they age
-- out.

ALTER TABLE search_miss ADD COLUMN IF NOT EXISTS normalized text NOT NULL DEFAULT '';

UPDATE search_miss SET normalized = query WHERE normalized = '';

-- The read is "the most-asked questions since a moment", so the index
-- carries the window and the key together.
CREATE INDEX IF NOT EXISTS search_miss_at_normalized ON search_miss (at DESC, normalized);
