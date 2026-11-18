-- team_reviewers holds a team's reviewer: the member that reviews each step of the team's tasks.
-- A team without a row has none, as every team has after this migration. The reviewer is never
-- the lead. A table, not a column, so running the migration again changes nothing.
CREATE TABLE IF NOT EXISTS team_reviewers (
  team_id TEXT PRIMARY KEY REFERENCES teams(team_id) ON DELETE CASCADE,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE
) STRICT;
