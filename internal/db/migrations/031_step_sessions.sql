-- step_sessions holds a member's Claude session for one step of a team's board in one role, work
-- or review: the session, the last message of the team's room that the session has seen, and the
-- system prompt that the session knows (prompt_session_id is the session it was stored for; ''
-- when none is). The member's other turns in the team keep the seat's session and cursor in
-- room_members. A table, not a column, so running the migration again changes nothing.
CREATE TABLE IF NOT EXISTS step_sessions (
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('work', 'review')),
  claude_session_id TEXT NOT NULL DEFAULT '',
  last_seen_seq INTEGER NOT NULL DEFAULT 0,
  prompt_session_id TEXT NOT NULL DEFAULT '',
  prompt TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (item_id, bot_id, role)
) STRICT;
