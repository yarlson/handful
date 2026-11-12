-- The paired phones (spec 6a748f8a section 5.3). A session's token is never
-- stored, only its SHA-256 hash. scope is view, chat or chat+approve.
-- last_seen_at and created_at decide when it expires; revoked_at is set when
-- the user revokes it. paired_by is who signed in to the proxy at pairing (the
-- Cloudflare Access email), if anyone. CREATE IF NOT EXISTS, so a second run
-- changes nothing.
CREATE TABLE IF NOT EXISTS phone_sessions (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('view', 'chat', 'chat+approve')),
  token_hash BLOB NOT NULL UNIQUE,
  paired_by TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  revoked_at INTEGER NOT NULL DEFAULT 0
);
