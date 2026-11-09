-- Every change of a board item leaves one event, so the review history of an
-- item survives its next move: item.note keeps only the latest note.
-- actor_id is a bot id, or '' for the user and the app; actor_name is the
-- name at that time. round is the review round of a 'changes' event.
CREATE TABLE item_events (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('created', 'assigned', 'submitted', 'changes', 'passed', 'blocked', 'unblocked', 'dropped', 'deferred', 'undeferred', 'withdrawn')),
  actor_id TEXT NOT NULL,
  actor_name TEXT NOT NULL,
  round INTEGER NOT NULL DEFAULT 0,
  text TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX item_events_by_item ON item_events(item_id, created_at);

-- A question (a bot message with needsReply) now stays open until the user
-- answers or dismisses it, or its bot withdraws it. Only the questions that
-- show as waiting today come out open: the latest user or bot message of a
-- chat. Every older one counts as answered.
UPDATE entries SET body = json_set(body, '$.questionState',
  CASE WHEN seq = (SELECT MAX(x.seq) FROM entries x WHERE x.bot_id = entries.bot_id AND x.kind IN ('user', 'bot'))
    THEN 'open' ELSE 'answered' END)
WHERE kind = 'bot' AND json_extract(body, '$.needsReply') = 1;

CREATE INDEX entries_open_questions ON entries(bot_id, seq)
  WHERE kind = 'bot' AND json_extract(body, '$.questionState') = 'open';
