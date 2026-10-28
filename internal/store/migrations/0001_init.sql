CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- Folders group feeds ("News", "YouTube", "Friends").
CREATE TABLE folders (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  sort       INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX folders_user_name ON folders(user_id, name COLLATE NOCASE);

-- A feed a person follows: RSS, Atom or JSON Feed (a YouTube channel is an Atom feed).
CREATE TABLE feeds (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  folder_id       INTEGER REFERENCES folders(id) ON DELETE SET NULL,
  url             TEXT NOT NULL,                   -- the feed's address
  site_url        TEXT NOT NULL DEFAULT '',
  title           TEXT NOT NULL DEFAULT '',        -- what the feed calls itself
  custom_title    TEXT NOT NULL DEFAULT '',        -- what the person calls it
  description     TEXT NOT NULL DEFAULT '',
  icon_url        TEXT NOT NULL DEFAULT '',
  kind            TEXT NOT NULL DEFAULT 'web',     -- web | youtube | podcast
  full_text       INTEGER NOT NULL DEFAULT 0,      -- fetch each new post's page and keep the whole article
  notify          INTEGER NOT NULL DEFAULT 0,      -- push a notification for each new post
  paused          INTEGER NOT NULL DEFAULT 0,
  refresh_min     INTEGER NOT NULL DEFAULT 0,      -- 0 = the person's default
  etag            TEXT NOT NULL DEFAULT '',
  last_modified   TEXT NOT NULL DEFAULT '',
  fetched_at      TEXT NOT NULL DEFAULT '',
  next_fetch_at   TEXT NOT NULL DEFAULT '',
  newest_at       TEXT NOT NULL DEFAULT '',        -- the newest post's date
  error           TEXT NOT NULL DEFAULT '',
  error_count     INTEGER NOT NULL DEFAULT 0,
  source          TEXT NOT NULL DEFAULT 'app',     -- app | import | greader | <pocket app slug>
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX feeds_user_url ON feeds(user_id, url);
CREATE INDEX feeds_due ON feeds(paused, next_fetch_at);

-- One article: a post from a feed, or a page saved to read later (feed_id NULL).
-- A feed post saved for later is the same row with saved = 1.
CREATE TABLE articles (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feed_id        INTEGER REFERENCES feeds(id) ON DELETE CASCADE,
  guid           TEXT NOT NULL DEFAULT '',          -- the feed's id for the post
  url            TEXT NOT NULL DEFAULT '',
  title          TEXT NOT NULL DEFAULT '',
  author         TEXT NOT NULL DEFAULT '',
  site_name      TEXT NOT NULL DEFAULT '',
  excerpt        TEXT NOT NULL DEFAULT '',          -- plain text, for lists
  content        TEXT NOT NULL DEFAULT '',          -- cleaned HTML
  image_url      TEXT NOT NULL DEFAULT '',
  media_url      TEXT NOT NULL DEFAULT '',          -- an enclosure: audio or video
  media_type     TEXT NOT NULL DEFAULT '',
  lang           TEXT NOT NULL DEFAULT '',
  words          INTEGER NOT NULL DEFAULT 0,
  published_at   TEXT NOT NULL DEFAULT '',
  added_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  read           INTEGER NOT NULL DEFAULT 0,
  read_at        TEXT NOT NULL DEFAULT '',
  starred        INTEGER NOT NULL DEFAULT 0,
  starred_at     TEXT NOT NULL DEFAULT '',
  saved          INTEGER NOT NULL DEFAULT 0,        -- on the read-later list
  saved_at       TEXT NOT NULL DEFAULT '',
  archived       INTEGER NOT NULL DEFAULT 0,        -- done with a saved article
  archived_at    TEXT NOT NULL DEFAULT '',
  progress       REAL NOT NULL DEFAULT 0,           -- how far the person read, 0–1
  extract_state  TEXT NOT NULL DEFAULT '',          -- '' | pending | done | failed
  extract_error  TEXT NOT NULL DEFAULT '',
  tags           TEXT NOT NULL DEFAULT '',
  note           TEXT NOT NULL DEFAULT '',
  ai_summary     TEXT NOT NULL DEFAULT '',
  notes_note_id  INTEGER NOT NULL DEFAULT 0,        -- the Pocket Notes note with the highlights
  notes_synced_at TEXT NOT NULL DEFAULT '',
  source         TEXT NOT NULL DEFAULT 'feed',      -- feed | app | bookmarklet | import | greader | <pocket app slug>
  external_id    TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX articles_feed_guid ON articles(user_id, feed_id, guid) WHERE feed_id IS NOT NULL;
CREATE UNIQUE INDEX articles_saved_url ON articles(user_id, url) WHERE feed_id IS NULL;
CREATE INDEX articles_user_unread ON articles(user_id, read, published_at);
CREATE INDEX articles_user_feed ON articles(user_id, feed_id, published_at);
CREATE INDEX articles_user_saved ON articles(user_id, saved, archived, saved_at);
CREATE INDEX articles_user_starred ON articles(user_id, starred, starred_at);
CREATE INDEX articles_user_url ON articles(user_id, url);
CREATE INDEX articles_user_read_at ON articles(user_id, read_at);

-- Full-text search over title, text and tags (rowid = articles.id).
CREATE VIRTUAL TABLE articles_fts USING fts5(title, body, tags, content='', contentless_delete=1, tokenize='unicode61 remove_diacritics 2');

-- Posts that were cleaned up: a feed that still lists them must not bring them back.
CREATE TABLE gone (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  guid    TEXT NOT NULL,
  at      TEXT NOT NULL,
  PRIMARY KEY (feed_id, guid)
);

-- A passage marked in an article, with an optional note.
CREATE TABLE highlights (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  text       TEXT NOT NULL,
  note       TEXT NOT NULL DEFAULT '',
  color      TEXT NOT NULL DEFAULT 'yellow',   -- yellow | green | blue | pink
  prefix     TEXT NOT NULL DEFAULT '',         -- a few words before and after, to find it again
  suffix     TEXT NOT NULL DEFAULT '',
  position   REAL NOT NULL DEFAULT 0,          -- where in the article, 0–1
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX highlights_user ON highlights(user_id, created_at);
CREATE INDEX highlights_article ON highlights(article_id, position);

-- "Posts that mention X: mark read / star / save / tag / notify me."
CREATE TABLE rules (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL DEFAULT '',
  feed_id    INTEGER REFERENCES feeds(id) ON DELETE CASCADE,   -- NULL = every feed
  folder_id  INTEGER REFERENCES folders(id) ON DELETE CASCADE, -- NULL = every folder
  field      TEXT NOT NULL DEFAULT 'title',    -- title | any | author | url
  words      TEXT NOT NULL,                    -- comma-separated; any of them matches
  action     TEXT NOT NULL DEFAULT 'read',     -- read | star | save | tag | notify
  tag        TEXT NOT NULL DEFAULT '',
  enabled    INTEGER NOT NULL DEFAULT 1,
  hits       INTEGER NOT NULL DEFAULT 0,
  last_hit_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX rules_user ON rules(user_id);
