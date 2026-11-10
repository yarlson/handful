-- One daily budget per team (spec 9cf81412 section 6). daily_budget is how
-- many turns the team may start in a local day; 0 means no limit, which is
-- where every team starts: the budget is watched, not enforced, until the
-- user sets one. budget_since is when the watch began (ms): the migration's
-- run for an existing team, the creation for a new one. budget_noted_on is
-- the local day (YYYY-MM-DD) whose pause the team's channel was told of;
-- budget_waited_on is the local day the user chose to wait out.
ALTER TABLE teams ADD COLUMN daily_budget INTEGER NOT NULL DEFAULT 0;
ALTER TABLE teams ADD COLUMN budget_since INTEGER NOT NULL DEFAULT 0;
ALTER TABLE teams ADD COLUMN budget_noted_on TEXT NOT NULL DEFAULT '';
ALTER TABLE teams ADD COLUMN budget_waited_on TEXT NOT NULL DEFAULT '';
UPDATE teams SET budget_since = CAST(strftime('%s', 'now') AS INTEGER) * 1000;
