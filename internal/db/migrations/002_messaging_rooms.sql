-- Rooms are rows in bots with kind 'room'; they reuse the transcript, the event
-- stream and the chat UI. Members take turns in the room, each in its own
-- Claude session.
ALTER TABLE bots ADD COLUMN kind TEXT NOT NULL DEFAULT 'agent' CHECK (kind IN ('agent', 'room'));

CREATE TABLE room_members (
  room_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  claude_session_id TEXT NOT NULL DEFAULT '',
  last_seen_seq INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (room_id, bot_id)
) STRICT;

-- Direct messages sent without priority wait here until the recipient's next turn.
CREATE TABLE agent_inbox (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  from_bot_id TEXT NOT NULL,
  from_name TEXT NOT NULL,
  text TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

-- SQLite cannot change a CHECK constraint in place, so entries and turns are rebuilt.
CREATE TABLE entries_v2 (
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  rev INTEGER NOT NULL,
  id TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK (kind IN ('user', 'bot', 'event', 'activity', 'approval', 'agent')),
  turn_id TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (bot_id, seq)
) STRICT;
INSERT INTO entries_v2 (bot_id, seq, rev, id, kind, turn_id, body, created_at)
SELECT bot_id, seq, rev, id, kind, turn_id, body, created_at FROM entries;
DROP TABLE entries;
ALTER TABLE entries_v2 RENAME TO entries;
CREATE INDEX entries_by_rev ON entries(bot_id, rev);

-- room_id is set for a member's turn in a room. hops counts how many agent
-- messages led to the turn, so bots cannot wake each other without end.
CREATE TABLE turns_v2 (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  source TEXT NOT NULL CHECK (source IN ('user', 'routine', 'system', 'agent', 'room')),
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
INSERT INTO turns_v2 (id, bot_id, source, client_nonce, prompt, status, error, created_at, started_at, finished_at)
SELECT id, bot_id, source, client_nonce, prompt, status, error, created_at, started_at, finished_at FROM turns;
DROP TABLE turns;
ALTER TABLE turns_v2 RENAME TO turns;
CREATE INDEX turns_by_status ON turns(status, created_at);
