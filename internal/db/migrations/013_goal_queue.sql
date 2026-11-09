-- A team keeps the goals that the user gives it while it works on one in a
-- queue: a queued goal has a 1-based queue_pos among its team's queued goals
-- and does nothing until it is promoted. queued never counts as open, so a
-- team still has at most one open goal. A queued goal gets its budget when it
-- becomes active.
--
-- A request (an item of another team's goal) has an intake: how the serving
-- team took it. slot_at > 0 while it holds one of the serving team's request
-- slots; jumped_at > 0 when its lead took it out of order. for_item_id is the
-- request that a chained request serves.
--
-- Requests that were accepted before keep working: each open one holds a
-- slot, so nothing that ran stops.
--
-- SQLite cannot change a CHECK, so goals is rebuilt, and items with it,
-- because its foreign key to goals would cascade on the drop.
CREATE TABLE goals_new (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  text TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('queued', 'active', 'delivered', 'paused', 'done', 'ended')),
  budget INTEGER NOT NULL,
  queue_pos INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
) STRICT;

INSERT INTO goals_new (rowid, id, team_id, text, state, budget, queue_pos, created_at)
SELECT rowid, id, team_id, text, state, budget, 0, created_at FROM goals;

CREATE TABLE items_new (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  goal_id TEXT NOT NULL REFERENCES goals_new(id) ON DELETE CASCADE,
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
  intake TEXT NOT NULL DEFAULT '' CHECK (intake IN ('', 'requested', 'accepted', 'declined', 'deferred', 'withdrawn')),
  intake_note TEXT NOT NULL DEFAULT '',
  defer_revisit TEXT NOT NULL DEFAULT '' CHECK (defer_revisit IN ('', 'goal_end', 'date', 'manual')),
  defer_until INTEGER NOT NULL DEFAULT 0,
  slot_at INTEGER NOT NULL DEFAULT 0,
  jumped_at INTEGER NOT NULL DEFAULT 0,
  for_item_id TEXT NOT NULL DEFAULT '',
  UNIQUE (goal_id, title)
) STRICT;

INSERT INTO items_new (
  rowid, id, team_id, goal_id, title, brief, acceptance, state, owner_id, reviewer_id, depends_on, result, note,
  blocked_from, review_rounds, created_by, position, created_at, updated_at, intake, slot_at
)
SELECT
  i.rowid, i.id, i.team_id, i.goal_id, i.title, i.brief, i.acceptance, i.state, i.owner_id, i.reviewer_id, i.depends_on,
  i.result, i.note, i.blocked_from, i.review_rounds, i.created_by, i.position, i.created_at, i.updated_at,
  CASE
    WHEN i.team_id = g.team_id THEN ''
    WHEN i.state = 'dropped' THEN 'declined'
    WHEN i.owner_id = '' THEN 'requested'
    ELSE 'accepted'
  END,
  CASE WHEN i.team_id <> g.team_id AND i.owner_id <> '' AND i.state NOT IN ('done', 'dropped') THEN i.updated_at ELSE 0 END
FROM items i JOIN goals g ON g.id = i.goal_id;

DROP TABLE items;
DROP TABLE goals;
ALTER TABLE goals_new RENAME TO goals;
ALTER TABLE items_new RENAME TO items;

CREATE UNIQUE INDEX goals_one_open ON goals(team_id) WHERE state IN ('active', 'delivered', 'paused');
CREATE INDEX goals_queue ON goals(team_id, queue_pos) WHERE state = 'queued';
CREATE INDEX items_by_goal ON items(goal_id, position);
CREATE INDEX items_by_team ON items(team_id);

-- request_slots is how many requests of other teams a team works on at once
-- while its own goal is active.
ALTER TABLE teams ADD COLUMN request_slots INTEGER NOT NULL DEFAULT 1;
