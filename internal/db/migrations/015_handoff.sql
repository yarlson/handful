-- A team hands another team work as a goal of its own: a handoff. It waits
-- as an incoming goal until the receiving lead accepts it (it joins the
-- queue, or starts), declines it (declined, final) or its sender withdraws it
-- (ended). Neither incoming nor declined counts as open, so a team still has
-- at most one open goal.
--
-- origin is 'user' for the goals the user gives a team, 'handoff' for a
-- handoff. from_team_id and from_bot_id are its sender ('' bot for the
-- user); from_goal_id is the sender's goal at the time, for context only.
-- ask_rounds counts the receiver's questions to the sender, asked_at > 0
-- while one waits for its answer, and depth is how many handoffs led to it.
--
-- SQLite cannot change a CHECK, so goals is rebuilt, and items and
-- item_events with it, because their foreign keys would cascade on the drop.
CREATE TABLE goals_new (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  text TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('incoming', 'queued', 'active', 'delivered', 'paused', 'done', 'ended', 'declined')),
  budget INTEGER NOT NULL,
  queue_pos INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  origin TEXT NOT NULL DEFAULT 'user' CHECK (origin IN ('user', 'handoff')),
  from_team_id TEXT NOT NULL DEFAULT '',
  from_bot_id TEXT NOT NULL DEFAULT '',
  from_goal_id TEXT NOT NULL DEFAULT '',
  brief TEXT NOT NULL DEFAULT '',
  artifact_ids TEXT NOT NULL DEFAULT '[]',
  intake_note TEXT NOT NULL DEFAULT '',
  asked_at INTEGER NOT NULL DEFAULT 0,
  ask_rounds INTEGER NOT NULL DEFAULT 0,
  depth INTEGER NOT NULL DEFAULT 0
) STRICT;

INSERT INTO goals_new (rowid, id, team_id, text, state, budget, queue_pos, created_at)
SELECT rowid, id, team_id, text, state, budget, queue_pos, created_at FROM goals;

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
  blocked_from, review_rounds, created_by, position, created_at, updated_at, intake, intake_note, defer_revisit,
  defer_until, slot_at, jumped_at, for_item_id
)
SELECT
  rowid, id, team_id, goal_id, title, brief, acceptance, state, owner_id, reviewer_id, depends_on, result, note,
  blocked_from, review_rounds, created_by, position, created_at, updated_at, intake, intake_note, defer_revisit,
  defer_until, slot_at, jumped_at, for_item_id
FROM items;

CREATE TABLE item_events_new (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES items_new(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('created', 'assigned', 'submitted', 'changes', 'passed', 'blocked', 'unblocked', 'dropped', 'deferred', 'undeferred', 'withdrawn')),
  actor_id TEXT NOT NULL,
  actor_name TEXT NOT NULL,
  round INTEGER NOT NULL DEFAULT 0,
  text TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
) STRICT;

INSERT INTO item_events_new (rowid, id, item_id, kind, actor_id, actor_name, round, text, created_at)
SELECT rowid, id, item_id, kind, actor_id, actor_name, round, text, created_at FROM item_events;

DROP TABLE item_events;
DROP TABLE items;
DROP TABLE goals;
ALTER TABLE goals_new RENAME TO goals;
ALTER TABLE items_new RENAME TO items;
ALTER TABLE item_events_new RENAME TO item_events;

CREATE UNIQUE INDEX goals_one_open ON goals(team_id) WHERE state IN ('active', 'delivered', 'paused');
CREATE INDEX goals_queue ON goals(team_id, queue_pos) WHERE state = 'queued';
CREATE INDEX goals_incoming ON goals(team_id, created_at) WHERE state = 'incoming';
CREATE INDEX goals_handed_over ON goals(from_team_id, created_at) WHERE origin = 'handoff';
CREATE INDEX items_by_goal ON items(goal_id, position);
CREATE INDEX items_by_team ON items(team_id);
CREATE INDEX item_events_by_item ON item_events(item_id, created_at);

-- start_handoffs is what an accepted handoff does when the team is free:
-- 'auto' starts it, 'ask' holds the queue until the user starts it.
ALTER TABLE teams ADD COLUMN start_handoffs TEXT NOT NULL DEFAULT 'auto' CHECK (start_handoffs IN ('auto', 'ask'));
