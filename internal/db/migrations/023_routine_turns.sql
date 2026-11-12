-- routine_turns links a routine's runs to their turns, so that a routine
-- queues at most one run and its queued runs end when it changes or goes. A
-- table, not a column, so running the migration again changes nothing.
CREATE TABLE IF NOT EXISTS routine_turns (
  turn_id TEXT PRIMARY KEY REFERENCES turns(id) ON DELETE CASCADE,
  routine_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS routine_turns_by_routine ON routine_turns(routine_id);
