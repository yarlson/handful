-- session_prompts holds, per Claude session, the system prompt the session
-- knows: what Claude Code recorded (--system-prompt-snapshot on) plus the
-- context updates delivered since. chat_id is the bot's own id for its chat,
-- or the room's id for its seat there. A row whose session_id is not the
-- chat's current session is stale. An empty prompt means Claude Code holds no
-- Handful prompt, so the next launch records its own. A table, not a column,
-- so running the migration again changes nothing.
CREATE TABLE IF NOT EXISTS session_prompts (
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  chat_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL,
  prompt TEXT NOT NULL,
  PRIMARY KEY (bot_id, chat_id)
) STRICT;
