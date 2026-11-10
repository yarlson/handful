-- The decision clock: a task in a team's inbox must get its lead's decision
-- within the team's decision time. decision_ms is the time counted so far,
-- only while the lead could act; decision_tick is when it last counted (ms),
-- 0 while the clock stands. missed_at is when the time ran out (ms): the task
-- then waits for the user.
ALTER TABLE goals ADD COLUMN decision_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE goals ADD COLUMN decision_tick INTEGER NOT NULL DEFAULT 0;
ALTER TABLE goals ADD COLUMN missed_at INTEGER NOT NULL DEFAULT 0;

-- The team's decision time, in minutes: 5 to 1440.
ALTER TABLE teams ADD COLUMN decision_minutes INTEGER NOT NULL DEFAULT 30;

-- A missed decision is a task event. SQLite cannot change a CHECK, so
-- goal_events is rebuilt; nothing refers to it.
CREATE TABLE goal_events_new (
  id TEXT PRIMARY KEY,
  goal_id TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('sent', 'proposed', 'taken', 'ordered', 'pinned', 'unpinned', 'asked', 'answered', 'later', 'due', 'passed', 'pushed_back', 'closed', 'approved', 'changes', 'stopped', 'reopened', 'withdrawn', 'missed')),
  actor_id TEXT NOT NULL,
  actor_name TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
) STRICT;

INSERT INTO goal_events_new (rowid, id, goal_id, kind, actor_id, actor_name, text, created_at)
SELECT rowid, id, goal_id, kind, actor_id, actor_name, text, created_at FROM goal_events;

DROP TABLE goal_events;
ALTER TABLE goal_events_new RENAME TO goal_events;
CREATE INDEX goal_events_by_goal ON goal_events(goal_id, created_at);
