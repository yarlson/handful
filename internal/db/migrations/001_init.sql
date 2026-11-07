CREATE TABLE IF NOT EXISTS bots (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  instructions TEXT NOT NULL DEFAULT '',
  claude_session_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

-- seq orders entries in the transcript; rev increases on every insert or update,
-- so a client that remembers the highest rev it saw can catch up on both.
CREATE TABLE IF NOT EXISTS entries (
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  rev INTEGER NOT NULL,
  id TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK (kind IN ('user', 'bot', 'event', 'activity', 'approval')),
  turn_id TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (bot_id, seq)
) STRICT;

CREATE INDEX IF NOT EXISTS entries_by_rev ON entries(bot_id, rev);

CREATE TABLE IF NOT EXISTS turns (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  source TEXT NOT NULL CHECK (source IN ('user', 'routine', 'system')),
  client_nonce TEXT,
  prompt TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed', 'interrupted')),
  error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  started_at INTEGER,
  finished_at INTEGER,
  UNIQUE (bot_id, client_nonce)
) STRICT;

CREATE INDEX IF NOT EXISTS turns_by_status ON turns(status, created_at);

-- scope_key is the owning bot id for agent memory, or 'user' for memory shared by all bots.
CREATE TABLE IF NOT EXISTS memories (
  id TEXT PRIMARY KEY,
  scope_key TEXT NOT NULL,
  tier TEXT NOT NULL CHECK (tier IN ('profile', 'log', 'note')),
  fact TEXT NOT NULL,
  learned_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE (scope_key, fact)
) STRICT;

CREATE TABLE IF NOT EXISTS routines (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  prompt TEXT NOT NULL,
  schedule TEXT NOT NULL,
  enabled INTEGER NOT NULL,
  next_run_at INTEGER,
  last_run_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS routines_due ON routines(enabled, next_run_at);

CREATE TABLE IF NOT EXISTS tool_grants (
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  tool_name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (bot_id, tool_name)
) STRICT;
