-- turn_spend holds what a turn cost, from the result events of Claude Code's stream: the cost of
-- its calls, its helpers' included, in US dollars at list price (Claude Code's estimate), and the
-- helpers it started. turn_model_spend holds the same turn's tokens and cost per model. A turn's
-- cost_usd is the sum of its models' cost_usd. Tables, not columns, so running the migration
-- again changes nothing.
CREATE TABLE IF NOT EXISTS turn_spend (
  turn_id TEXT PRIMARY KEY REFERENCES turns(id) ON DELETE CASCADE,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  cost_usd REAL NOT NULL,
  helpers INTEGER NOT NULL,
  recorded_at INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS turn_spend_by_time ON turn_spend(recorded_at);
CREATE TABLE IF NOT EXISTS turn_model_spend (
  turn_id TEXT NOT NULL REFERENCES turn_spend(turn_id) ON DELETE CASCADE,
  model TEXT NOT NULL,
  input_tokens INTEGER NOT NULL,
  output_tokens INTEGER NOT NULL,
  cache_read_tokens INTEGER NOT NULL,
  cache_write_tokens INTEGER NOT NULL,
  cost_usd REAL NOT NULL,
  PRIMARY KEY (turn_id, model)
) STRICT;
