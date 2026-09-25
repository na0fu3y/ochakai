-- A base remembers how new it was when it was made (design doc 0146).
--
-- What a deployment gets when nobody names an embedding model moved from
-- gemini-embedding-001 in the deployment's own region to
-- gemini-embedding-2 in global. A base that already existed keeps the
-- old default — its vectors are in that model's space and its text has
-- only ever gone to that region — so the start has to be able to tell a
-- base that was there before the move from one made after it. Whether
-- schema_migrations is empty answers that exactly once, on the first
-- start, and this row is where the answer is kept.
--
-- `migration` is the newest migration the base held when it was made.
-- For a base older than this table that is the newest it held when the
-- table arrived, which is all anybody can still know, and all the start
-- needs: it compares the value against this file's own name.
--
-- Written here as the older answer. Migrate replaces it with its own
-- newest migration only when it found schema_migrations empty, after
-- every migration has committed — so a start that dies in between leaves
-- the base reading as old, which is the side that changes nothing.
CREATE TABLE base_birth (
    only_row  boolean PRIMARY KEY DEFAULT true CHECK (only_row),
    migration text NOT NULL
);

INSERT INTO base_birth (migration) SELECT max(version) FROM schema_migrations;
