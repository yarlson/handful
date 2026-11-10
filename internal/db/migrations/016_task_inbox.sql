-- Every task reaches its team through the inbox (the incoming state), and the
-- lead decides on it. Two new states: later (deferred to later_until, in ms;
-- it returns to the inbox when due) and stopped (no new turns; reopening
-- returns it to the inbox). A proposal is a task that a member of the team
-- proposed for it.
--
-- pinned holds a queued task where the user put it: its lead cannot move it.
-- passes counts how often a lead passed the task on to another team, and
-- passed_by says the latest ("Passed to Dev by Product Lead").
-- waits_on_item_id is the sender's step that waits on the task: a push back
-- blocks that step.
--
-- A delivered task waits for its asker's approval without holding its team:
-- only active and paused count as open, so the next task can start.
--
-- SQLite cannot change a CHECK, so goals is rebuilt, and items and
-- item_events with it, because their foreign keys would cascade on the drop.
CREATE TABLE goals_new (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  text TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('incoming', 'later', 'queued', 'active', 'delivered', 'paused', 'done', 'ended', 'declined', 'stopped')),
  budget INTEGER NOT NULL,
  queue_pos INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  origin TEXT NOT NULL DEFAULT 'user' CHECK (origin IN ('user', 'handoff', 'proposal')),
  from_team_id TEXT NOT NULL DEFAULT '',
  from_bot_id TEXT NOT NULL DEFAULT '',
  from_goal_id TEXT NOT NULL DEFAULT '',
  brief TEXT NOT NULL DEFAULT '',
  artifact_ids TEXT NOT NULL DEFAULT '[]',
  intake_note TEXT NOT NULL DEFAULT '',
  asked_at INTEGER NOT NULL DEFAULT 0,
  ask_rounds INTEGER NOT NULL DEFAULT 0,
  depth INTEGER NOT NULL DEFAULT 0,
  pinned INTEGER NOT NULL DEFAULT 0,
  later_until INTEGER NOT NULL DEFAULT 0,
  passes INTEGER NOT NULL DEFAULT 0,
  passed_by TEXT NOT NULL DEFAULT '',
  waits_on_item_id TEXT NOT NULL DEFAULT ''
) STRICT;

INSERT INTO goals_new (
  rowid, id, team_id, text, state, budget, queue_pos, created_at, origin, from_team_id, from_bot_id, from_goal_id,
  brief, artifact_ids, intake_note, asked_at, ask_rounds, depth
)
SELECT
  rowid, id, team_id, text, state, budget, queue_pos, created_at, origin, from_team_id, from_bot_id, from_goal_id,
  brief, artifact_ids, intake_note, asked_at, ask_rounds, depth
FROM goals;

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

CREATE UNIQUE INDEX goals_one_open ON goals(team_id) WHERE state IN ('active', 'paused');
CREATE INDEX goals_queue ON goals(team_id, queue_pos) WHERE state = 'queued';
CREATE INDEX goals_incoming ON goals(team_id, created_at) WHERE state IN ('incoming', 'later');
CREATE INDEX goals_handed_over ON goals(from_team_id, created_at) WHERE origin = 'handoff';
CREATE INDEX items_by_goal ON items(goal_id, position);
CREATE INDEX items_by_team ON items(team_id);
CREATE INDEX item_events_by_item ON item_events(item_id, created_at);

-- Each decision on a task leaves an event: who, when and the text.
CREATE TABLE goal_events (
  id TEXT PRIMARY KEY,
  goal_id TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('sent', 'proposed', 'taken', 'ordered', 'pinned', 'unpinned', 'asked', 'answered', 'later', 'due', 'passed', 'pushed_back', 'closed', 'approved', 'changes', 'stopped', 'reopened', 'withdrawn')),
  actor_id TEXT NOT NULL,
  actor_name TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX goal_events_by_goal ON goal_events(goal_id, created_at);

-- A lead's decision turn on a task in its inbox (an arrival, an answer, a
-- deferral that came due, a task passed on): it runs whatever the task's
-- state, and no goal pays for it.
ALTER TABLE turns ADD COLUMN decision INTEGER NOT NULL DEFAULT 0;

-- The inbox limits of a team: open tasks from all senders, and from one
-- sending team. The user's tasks are exempt.
ALTER TABLE teams ADD COLUMN inbox_max INTEGER NOT NULL DEFAULT 10;
ALTER TABLE teams ADD COLUMN inbox_per_sender INTEGER NOT NULL DEFAULT 3;
