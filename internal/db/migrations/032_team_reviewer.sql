-- The team's reviewer: the member that reviews each step of the team's tasks,
-- or '' for none, where every team starts. It is never the lead.
ALTER TABLE teams ADD COLUMN reviewer_bot_id TEXT NOT NULL DEFAULT '';
