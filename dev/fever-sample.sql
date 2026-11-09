-- A small Fever dataset shaped like the old app's fever_* tables, for trying
-- `fever import-fever` without a dump of the real data. It lives in its own
-- schema, fever_sample, so it never touches real fever_* tables loaded into
-- the same database. Load it with `make dev-sample`, import it with
-- `make dev-import-sample`.

DROP SCHEMA IF EXISTS fever_sample CASCADE;
CREATE SCHEMA fever_sample;
SET search_path = fever_sample;

CREATE TABLE fever_favicons (
    id serial PRIMARY KEY,
    cache text NOT NULL,
    url varchar(255) NOT NULL,
    url_checksum bigint NOT NULL,
    last_cached_on_time bigint DEFAULT 0
);

CREATE TABLE fever_groups (
    id serial PRIMARY KEY,
    title varchar(255) NOT NULL,
    item_excerpts smallint NOT NULL DEFAULT -1,
    item_allows smallint NOT NULL DEFAULT -1,
    unread_counts smallint NOT NULL DEFAULT -1,
    sort_order smallint NOT NULL DEFAULT -1
);

CREATE TABLE fever_feeds (
    id serial PRIMARY KEY,
    favicon_id integer DEFAULT 0,
    title varchar(255),
    url varchar(255) NOT NULL DEFAULT '',
    url_checksum bigint NOT NULL,
    site_url varchar(255),
    domain varchar(255),
    requires_auth smallint DEFAULT 0,
    auth varchar(255),
    is_spark smallint NOT NULL DEFAULT 0,
    prevents_hotlinking smallint NOT NULL DEFAULT 0,
    item_excerpts smallint NOT NULL DEFAULT -1,
    item_allows smallint NOT NULL DEFAULT -1,
    unread_counts smallint NOT NULL DEFAULT -1,
    sort_order smallint NOT NULL DEFAULT -1,
    last_refreshed_on_time bigint NOT NULL DEFAULT 0,
    last_updated_on_time bigint NOT NULL DEFAULT 0,
    last_added_on_time bigint NOT NULL DEFAULT 0
);

CREATE TABLE fever_feeds_groups (
    feed_id integer NOT NULL,
    group_id integer
);

CREATE TABLE fever_items (
    id serial PRIMARY KEY,
    feed_id integer,
    uid varchar(255),
    title varchar(255),
    author varchar(255),
    description text,
    link varchar(255),
    url_checksum bigint NOT NULL DEFAULT 0,
    read_on_time bigint NOT NULL DEFAULT 0,
    is_saved smallint NOT NULL DEFAULT 0,
    created_on_time bigint NOT NULL,
    added_on_time bigint NOT NULL
);

INSERT INTO fever_favicons (id, cache, url, url_checksum, last_cached_on_time) VALUES
    (1, 'image/gif;base64,R0lGODlhAQABAIAAAObm5gAAACH5BAEAAAAALAAAAAABAAEAAAICRAEAOw==', 'favicon.png', 1, 1700000000),
    (2, 'image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAKklEQVR42mP8z8Dwn4ECwESJ5lEDRg0YNWDUgFEDRg0YNWDUgOFvAADvYQIRqzYpGwAAAABJRU5ErkJggg==', 'https://example.com/favicon.ico', 2, 1700000000),
    (3, 'not a favicon', 'https://news.example.org/favicon.ico', 3, 1700000000);
SELECT setval('fever_favicons_id_seq', 3);

INSERT INTO fever_groups (id, title, item_excerpts, item_allows, unread_counts, sort_order) VALUES
    (1, 'Tech', -1, -1, -1, -1),
    (2, 'News &amp; Politics', 1, 0, 0, 1);
SELECT setval('fever_groups_id_seq', 2);

INSERT INTO fever_feeds (id, favicon_id, title, url, url_checksum, site_url, requires_auth, auth, item_allows, last_refreshed_on_time, last_added_on_time) VALUES
    (1, 2, 'Example Blog', 'https://example.com/feed.xml', 11, 'https://example.com/', 0, NULL, -1, 1730000000, 1729990000),
    (2, 3, 'Daily News', 'https://news.example.org/rss', 12, 'https://news.example.org/', 0, NULL, 2, 1730000000, 1729990000),
    (3, 0, 'Private Feed', 'https://private.example.net/feed', 13, 'https://private.example.net/', 1, 'am9lOnNlY3JldA==', -1, 1730000000, 0),
    (4, 0, 'Locked Feed', 'https://locked.example.net/feed', 14, 'https://locked.example.net/', 1, '', -1, 0, 0),
    (5, 0, NULL, 'https://untitled.example.com/atom.xml', 15, NULL, 0, NULL, -1, 0, 0);
SELECT setval('fever_feeds_id_seq', 5);

INSERT INTO fever_feeds_groups (feed_id, group_id) VALUES
    (1, 1), (2, 1), (2, 2), (3, 2), (99, 1), (1, 0), (1, 1);

INSERT INTO fever_items (id, feed_id, uid, title, author, description, link, read_on_time, is_saved, created_on_time, added_on_time) VALUES
    (1, 1, 'post-1', 'Hello &amp; welcome', 'Ann', '<p>First <a href="/about">post</a> <img src="img/a.png" onerror="x()"></p><script>alert(1)</script>', 'https://example.com/2024/hello', 1730001000, 0, 1729990000, 1729990100),
    (2, 1, 'post-2', 'Second post', '', '<p>Unread, saved</p>', 'https://example.com/2024/second', 0, 1, 1729995000, 1729995100),
    (3, 1, 'post-2', 'Second post (duplicate)', '', '<p>dup</p>', 'https://example.com/2024/second', 0, 0, 1729995000, 1729995200),
    (4, 2, NULL, '&#8230;', NULL, '<div style="color:red" onclick="steal()">No uid, falls back to link</div>', 'https://news.example.org/a', 0, 0, 1729996000, 1729996100),
    (5, 2, NULL, 'No uid, no link', NULL, '<a href="javascript:alert(1)">bad</a>', NULL, 0, 0, 0, 1729997000),
    (6, 3, 'p-1', 'Private item', NULL, 'Text only', 'https://private.example.net/1', 0, 0, 1729998000, 1729998100),
    (7, 99, 'orphan', 'Item of a deleted feed', NULL, '', '', 0, 0, 1729999000, 1729999100),
    -- The same entry twice: first stored with Fever's checksum of the link as
    -- its uid, later with the link itself, and saved. The saved copy must win.
    (8, 1, '884332246', 'Third post', '', '<p>old copy</p>', 'https://example.com/2024/third', 1730002000, 0, 1729996000, 1729996100),
    (9, 1, 'https://example.com/2024/third', 'Third post', '', '<p>saved copy</p>', 'https://example.com/2024/third', 0, 1, 1729996000, 1729996200);
SELECT setval('fever_items_id_seq', 9);
