-- A 'task' turn is the model's own turn after one of its background tasks
-- (a background shell, a monitor) reports. Claude Code starts it, so it is
-- recorded as running, never queued.
-- SQLite cannot change a CHECK constraint in place, so turns is rebuilt.
CREATE TABLE turns_v5 (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  source TEXT NOT NULL CHECK (source IN ('user', 'routine', 'system', 'agent', 'room', 'compact', 'task')),
  client_nonce TEXT,
  prompt TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed', 'interrupted')),
  error TEXT NOT NULL DEFAULT '',
  room_id TEXT NOT NULL DEFAULT '',
  hops INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  started_at INTEGER,
  finished_at INTEGER,
  UNIQUE (bot_id, client_nonce)
) STRICT;
INSERT INTO turns_v5 (id, bot_id, source, client_nonce, prompt, status, error, room_id, hops, created_at, started_at, finished_at)
SELECT id, bot_id, source, client_nonce, prompt, status, error, room_id, hops, created_at, started_at, finished_at FROM turns;
DROP TABLE turns;
ALTER TABLE turns_v5 RENAME TO turns;
CREATE INDEX turns_by_status ON turns(status, created_at);
