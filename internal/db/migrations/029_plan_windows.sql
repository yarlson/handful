-- plan_windows holds the latest state of each usage window of the Claude plan, such as
-- five_hour or seven_day, as Claude Code's last rate_limit_event reported it. utilization and
-- surpassed_threshold are fractions of the window's limit, NULL when not reported; resets_at is
-- in Unix seconds, 0 when not reported. A table, not columns, so running the migration again
-- changes nothing.
CREATE TABLE IF NOT EXISTS plan_windows (
  name TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  utilization REAL,
  resets_at INTEGER NOT NULL,
  surpassed_threshold REAL,
  received_at INTEGER NOT NULL
) STRICT;
