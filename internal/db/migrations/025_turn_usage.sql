-- turn_usage holds the tokens of a turn's Claude calls, from Claude Code's
-- stream: the turn's input, cache-read, cache-write and output tokens, the
-- number of calls, the context of its last call, and whether its first call
-- wrote more than half of its context to the cache (cold). A table, not
-- columns, so running the migration again changes nothing.
CREATE TABLE IF NOT EXISTS turn_usage (
  turn_id TEXT PRIMARY KEY REFERENCES turns(id) ON DELETE CASCADE,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  input_tokens INTEGER NOT NULL,
  cache_read_tokens INTEGER NOT NULL,
  cache_write_tokens INTEGER NOT NULL,
  output_tokens INTEGER NOT NULL,
  calls INTEGER NOT NULL,
  context_tokens INTEGER NOT NULL,
  cold INTEGER NOT NULL CHECK (cold IN (0, 1)),
  recorded_at INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS turn_usage_by_bot ON turn_usage(bot_id, recorded_at);
