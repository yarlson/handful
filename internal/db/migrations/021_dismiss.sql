-- The user puts things away on a team's Work panel (spec 4fe41f6c section 4).
-- goal_dismissals holds when the user put away a task that a team sent and
-- the other team's lead pushed back; only the sending team's board reads it.
-- off_task_dismissals holds when the user put away a team's turns on no
-- task: the board counts only the turns that started after it. Tables, not
-- columns, so running the migration again changes nothing.
CREATE TABLE IF NOT EXISTS goal_dismissals (
  goal_id TEXT PRIMARY KEY REFERENCES goals(id) ON DELETE CASCADE,
  dismissed_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS off_task_dismissals (
  team_id TEXT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
  dismissed_at INTEGER NOT NULL
);
