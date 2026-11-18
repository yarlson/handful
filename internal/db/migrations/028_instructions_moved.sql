-- instructions_moved lists the bots whose instructions column has been copied
-- to the CLAUDE.md in their Claude Code folder, which holds a bot's
-- instructions from then on. The column stays as it was, read only by that
-- copy. A table, not a column, so running the migration again changes nothing.
CREATE TABLE IF NOT EXISTS instructions_moved (
  bot_id TEXT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE
) STRICT;
