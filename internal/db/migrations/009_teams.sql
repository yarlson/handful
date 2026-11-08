-- A team is a room with a lead, a working agreement and a turn budget for its
-- goals. The room row keeps the name, the purpose, the members and the channel.
-- lead_bot_id can name a bot that has left the team; the first member then leads.
CREATE TABLE teams (
  team_id TEXT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
  lead_bot_id TEXT NOT NULL,
  agreement TEXT NOT NULL DEFAULT '',
  goal_budget INTEGER NOT NULL DEFAULT 30
) STRICT;
