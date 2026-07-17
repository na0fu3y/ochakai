-- Free knowledge types (design doc 0005): the five built-in types become
-- recommendations, not a closed set. Any path-segment slug is a valid type,
-- so foreign OKF bundles import without renaming; validation lives in the
-- application (domain.ValidType).

ALTER TABLE knowledge DROP CONSTRAINT IF EXISTS knowledge_type_check;
