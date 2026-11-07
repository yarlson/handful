-- Who started a routine's last run: "the schedule", "the user" or
-- bot "<name>". Rows from before this column have NULL and show no trigger.
ALTER TABLE routines ADD COLUMN last_run_by TEXT;
