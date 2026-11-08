-- A goal is a request of the user to a team, which the lead splits into items.
-- A team has at most one open goal: active, delivered or paused.
CREATE TABLE goals (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  text TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('active', 'delivered', 'paused', 'done', 'ended')),
  budget INTEGER NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX goals_one_open ON goals(team_id) WHERE state IN ('active', 'delivered', 'paused');

-- An item is one piece of a goal's work. Its owner does it; when it has a
-- reviewer, the reviewer must pass it before it is done. depends_on is a
-- JSON array of the ids of items of the same goal that must be closed first.
-- team_id is the team that does the item: another team's for a request.
-- blocked_from is the state a blocked item had, to go back to when unblocked.
CREATE TABLE items (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  goal_id TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  brief TEXT NOT NULL,
  acceptance TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('todo', 'doing', 'review', 'done', 'blocked', 'dropped')),
  owner_id TEXT NOT NULL DEFAULT '',
  reviewer_id TEXT NOT NULL DEFAULT '',
  depends_on TEXT NOT NULL DEFAULT '[]',
  result TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  blocked_from TEXT NOT NULL DEFAULT '',
  review_rounds INTEGER NOT NULL DEFAULT 0,
  created_by TEXT NOT NULL,
  position INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE (goal_id, title)
) STRICT;

CREATE INDEX items_by_goal ON items(goal_id, position);

-- A board turn works for a goal: the lead's turn, an owner's work on an item
-- or a reviewer's check. The goal's budget counts its started turns.
ALTER TABLE turns ADD COLUMN goal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE turns ADD COLUMN item_id TEXT NOT NULL DEFAULT '';
ALTER TABLE turns ADD COLUMN board_role TEXT NOT NULL DEFAULT '' CHECK (board_role IN ('', 'lead', 'work', 'review', 'retro'));
CREATE INDEX turns_by_goal ON turns(goal_id);
CREATE INDEX turns_by_item ON turns(item_id);
