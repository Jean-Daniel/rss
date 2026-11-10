-- +goose Up

-- Search matches words in titles, authors and article text (not in HTML tags
-- or their attributes), whatever their case and accents: "cafe" finds
-- "Café". No stemming and no stop words, so it works the same in every
-- language. Accents are dropped by the unaccent extension, which comes with
-- PostgreSQL; if the database user may not install it, search still works,
-- accents included (the README says how to add it later).
-- +goose StatementBegin
DO $$
DECLARE
    ext_schema name;
BEGIN
    BEGIN
        CREATE EXTENSION IF NOT EXISTS unaccent;
    EXCEPTION WHEN insufficient_privilege OR undefined_file THEN
        RAISE NOTICE 'unaccent is not available: search will not ignore accents';
    END;
    CREATE TEXT SEARCH CONFIGURATION fever_search (COPY = simple);
    SELECT n.nspname INTO ext_schema
    FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
    WHERE e.extname = 'unaccent';
    IF ext_schema IS NOT NULL THEN
        -- Every kind of word that may hold letters with accents.
        EXECUTE format('ALTER TEXT SEARCH CONFIGURATION fever_search
                        ALTER MAPPING FOR word, hword, hword_part, numword, numhword, hword_numpart
                        WITH %I.unaccent, simple', ext_schema);
    END IF;
END $$;
-- +goose StatementEnd

-- The words of each item, in a table of their own: read and saved changes
-- update the items rows all the time, and would otherwise write every word
-- of the item into the index again each time.
CREATE TABLE item_search (
    item_id bigint PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    search  tsvector NOT NULL
);
CREATE INDEX item_search_words ON item_search USING gin (search);

-- search_vector is what is indexed of an item. PostgreSQL refuses a tsvector
-- over 1 MB, so only the beginning of very long articles counts: 64 Ki
-- characters of text, tags removed, stay under it whatever the text. Word
-- positions are dropped: searches do not use them.
-- +goose StatementBegin
CREATE FUNCTION search_vector(title text, author text, content text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE SET search_path FROM CURRENT AS $$
    SELECT strip(to_tsvector('fever_search', title || ' ' || author || ' ' ||
        left(regexp_replace(left(content, 1048576), '<[^>]*>', ' ', 'g'), 65536)))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION index_item() RETURNS trigger
LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
BEGIN
    INSERT INTO item_search (item_id, search)
    VALUES (NEW.id, search_vector(NEW.title, NEW.author, NEW.content))
    ON CONFLICT (item_id) DO UPDATE SET search = EXCLUDED.search;
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER items_index AFTER INSERT OR UPDATE OF title, author, content ON items
FOR EACH ROW EXECUTE FUNCTION index_item();

INSERT INTO item_search (item_id, search)
SELECT id, search_vector(title, author, content) FROM items;

-- search_query turns what the reader typed into a query: every word must be
-- there, or a word starting with it (from 3 letters: "swift" finds
-- "SwiftUI"). The words are read as the indexed text is, and joined as they
-- are (a cast, not to_tsquery, which would make hyphenated words phrases).
-- +goose StatementBegin
CREATE FUNCTION search_query(q text) RETURNS tsquery
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE SET search_path FROM CURRENT AS $$
    SELECT string_agg(
        '''' || replace(replace(lexeme, '\', '\\'), '''', '''''') || '''' ||
        CASE WHEN length(lexeme) >= 3 THEN ':*' ELSE '' END, ' & ')::tsquery
    FROM unnest(to_tsvector('fever_search', q))
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION search_query(text);
DROP TRIGGER items_index ON items;
DROP FUNCTION index_item();
DROP TABLE item_search;
DROP FUNCTION search_vector(text, text, text);
DROP TEXT SEARCH CONFIGURATION fever_search;
