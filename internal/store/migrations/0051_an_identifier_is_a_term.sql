-- An identifier is a term of its own, beside the words inside it.
--
-- 0036 splits every name on its punctuation before the english pass,
-- so a path segment and each part of order_items is a term — "orders"
-- finds the body that says order_items, and design doc 0022's promise
-- that the id is searchable holds. What the split loses is that the
-- parts were adjacent. The query side has always split the same way, so
-- order_items asked for order and item, and a raw event table with
-- thousands of columns holds both somewhere: it contained the name as
-- fully as the order_items table did, and outranked it for saying more
-- (issue #883). A hyphenated project name went the same way — three
-- words, each held by nearly every table in that project.
--
-- The fix is one more set of lexemes rather than a different split.
-- Each identifier the haystack spells — a run of ASCII letters and
-- digits joined by . _ or - — is stored whole, and each qualifier part
-- of a dotted one that is itself joined (acme-analytics-prod and
-- sales_mart inside acme-analytics-prod.sales_mart.orders). The query
-- asks for the identifiers it contains the same way
-- (store.identifiers), and the scorer weights a fragment by its rarity,
-- so the lexeme only the concepts spelling the name hold is where the
-- query's weight goes. The parts stay, and with them every match they
-- made; nothing becomes unfindable.
--
-- Positions were the other way to ask for adjacency (phraseto_tsquery),
-- and 0036 stripped them because nothing read them: keeping them would
-- grow every row's vector for the sake of the few terms that are names.
--
-- One spelling: lower case, - read as _ (tables/order-items is the file
-- somebody wrote about order_items), . kept, because it qualifies a name
-- rather than joining one. ASCII only, so the class cannot depend on
-- the database's locale; TestIdentifiersAgreeWithGo holds this function
-- and the Go pattern to one reading.
CREATE OR REPLACE FUNCTION ochakai_identifiers(p_text text) RETURNS text[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS $$
    SELECT COALESCE(array_agg(DISTINCT translate(lower(x), '-', '_')), '{}')
    FROM regexp_matches(p_text, '[A-Za-z0-9]+(?:[._-][A-Za-z0-9]+)+', 'g') AS m,
         LATERAL (
             SELECT m[1]
             UNION ALL
             SELECT part FROM unnest(string_to_array(m[1], '.')) AS part
             WHERE m[1] LIKE '%.%' AND part ~ '[_-]'
         ) AS id(x)
    WHERE char_length(x) <= 255
$$;

CREATE OR REPLACE FUNCTION ochakai_search_tsv(p_text text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS $$
    SELECT strip(
        to_tsvector('english', regexp_replace(
            regexp_replace(
                regexp_replace(f.folded, '[' || ochakai_cjk_class() || ']+', ' ', 'g'),
                '[^[:alnum:]]+', ' ', 'g'),
            '[^[:space:]]{255}[^[:space:]]{255}[^[:space:]]*', ' ', 'g'))
        || to_tsvector('simple', ochakai_cjk_bigrams(f.folded))
        || array_to_tsvector(ochakai_identifiers(f.folded)))
    FROM (SELECT normalize(p_text, NFKC)) AS f(folded)
$$;

-- search_tsv is generated, and replacing the function it is generated
-- from recomputes nothing already stored. Assigning search_text to
-- itself is how every migration since 0016 has asked the column again.
UPDATE object SET search_text = search_text WHERE id IS NOT NULL;
