-- model and effort are a bot's own Claude Code model and reasoning effort;
-- a team's default_model and default_effort fill them in for its members'
-- turns in the team. '' means inherit: the next level, or Claude Code's own.
ALTER TABLE bots ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE bots ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE teams ADD COLUMN default_model TEXT NOT NULL DEFAULT '';
ALTER TABLE teams ADD COLUMN default_effort TEXT NOT NULL DEFAULT '';
