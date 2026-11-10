-- One kind of task (spec 9cf81412 §11, phase 4). A request, an item of a
-- team's task that another team serves, becomes a task of the serving team
-- that the asking team sent, linked by waits_on_item_id to the request item.
-- That item stays a step of the asking task, now its team's own and without
-- an owner, so the steps that needed the request still depend on it.
--
-- The requests of every task that is not over change, also of a stopped
-- task and of one in an inbox, later or a plan, which can start again; those
-- of done, ended and pushed back tasks stay as history. The intake maps:
--   requested → incoming, with its new team's lead's decision turn queued,
--               as for any task that arrives;
--   accepted  → queued, after its team's plan, the request that worked in a
--               slot first; it is the team's Now task when the team has no
--               Now task and no plan;
--   deferred  → later, until its date, or for 7 days when it waited for a
--               goal's end or a manual undefer: later lasts at most 7 days;
--   declined  → declined with its note; its item stays dropped.
-- A chained request, which served another request, becomes a task that the
-- serving team sent for the task that request became; its item leaves the
-- asking task, which never asked for it.
--
-- The goal queue is the plan already, and a delivered task already waits
-- for approval without holding its team (016). request_slots, slot_at,
-- jumped_at and the intake columns stay until no code reads them.
CREATE TABLE request_tasks AS
SELECT
  i.id AS item_id, i.rowid AS item_rowid, lower(hex(randomblob(16))) AS hex_id, i.team_id, i.goal_id AS asking_goal_id,
  g.team_id AS asking_team_id, g.depth AS asking_depth, i.for_item_id, i.intake, i.intake_note, i.defer_revisit,
  i.defer_until, i.slot_at, i.jumped_at, i.title, i.brief, i.acceptance, i.created_by, i.created_at, i.updated_at
FROM items i JOIN goals g ON g.id = i.goal_id
WHERE g.state NOT IN ('done', 'ended', 'declined')
  AND (i.intake = 'declined' OR (i.intake IN ('requested', 'accepted', 'deferred') AND i.state NOT IN ('done', 'dropped')));

ALTER TABLE request_tasks ADD COLUMN task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE request_tasks ADD COLUMN turn_hex TEXT NOT NULL DEFAULT '';
ALTER TABLE request_tasks ADD COLUMN plan_rank INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_tasks ADD COLUMN plan_base INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_tasks ADD COLUMN idle INTEGER NOT NULL DEFAULT 0;

UPDATE request_tasks SET turn_hex = lower(hex(randomblob(16)));
UPDATE request_tasks SET task_id =
  substr(hex_id, 1, 8) || '-' || substr(hex_id, 9, 4) || '-' || substr(hex_id, 13, 4) || '-' || substr(hex_id, 17, 4) || '-' || substr(hex_id, 21, 12);

UPDATE request_tasks SET plan_rank = (
  SELECT ranked.r FROM (
    SELECT item_id, ROW_NUMBER() OVER (
      PARTITION BY team_id
      ORDER BY slot_at = 0, CASE WHEN slot_at > 0 THEN slot_at WHEN jumped_at > 0 THEN jumped_at ELSE 4611686018427387904 END, created_at, item_rowid
    ) AS r
    FROM request_tasks WHERE intake = 'accepted'
  ) ranked WHERE ranked.item_id = request_tasks.item_id
)
WHERE intake = 'accepted';

UPDATE request_tasks SET
  plan_base = (SELECT COALESCE(MAX(queue_pos), 0) FROM goals WHERE goals.team_id = request_tasks.team_id AND goals.state = 'queued'),
  idle = NOT EXISTS (SELECT 1 FROM goals WHERE goals.team_id = request_tasks.team_id AND goals.state IN ('active', 'paused', 'queued'));

INSERT INTO goals (
  id, team_id, text, state, budget, queue_pos, created_at, origin, from_team_id, from_bot_id, from_goal_id, brief,
  intake_note, depth, later_until, waits_on_item_id
)
SELECT
  r.task_id, r.team_id, r.title,
  CASE r.intake
    WHEN 'requested' THEN 'incoming'
    WHEN 'deferred' THEN 'later'
    WHEN 'declined' THEN 'declined'
    WHEN 'accepted' THEN CASE WHEN r.idle AND r.plan_rank = 1 THEN 'active' ELSE 'queued' END
  END,
  CASE WHEN r.intake = 'accepted' AND r.idle AND r.plan_rank = 1 THEN COALESCE((SELECT goal_budget FROM teams WHERE team_id = r.team_id), 0) ELSE 0 END,
  CASE WHEN r.intake <> 'accepted' THEN 0 WHEN r.idle THEN r.plan_rank - 1 ELSE r.plan_base + r.plan_rank END,
  r.created_at, 'handoff',
  COALESCE(chained.team_id, (SELECT team_id FROM items WHERE id = r.for_item_id), r.asking_team_id),
  CASE WHEN EXISTS (SELECT 1 FROM bots WHERE id = r.created_by) THEN r.created_by ELSE '' END,
  COALESCE(chained.task_id, r.asking_goal_id),
  r.brief || CASE WHEN r.acceptance <> '' THEN char(10) || char(10) || 'Done when: ' || r.acceptance ELSE '' END,
  CASE WHEN r.intake IN ('deferred', 'declined') THEN r.intake_note ELSE '' END,
  r.asking_depth + CASE WHEN r.for_item_id = '' THEN 1 ELSE 2 END,
  CASE
    WHEN r.intake <> 'deferred' THEN 0
    WHEN r.defer_revisit = 'date' AND r.defer_until > 0 THEN r.defer_until
    ELSE CAST(strftime('%s', 'now') AS INTEGER) * 1000 + 7 * 24 * 3600 * 1000
  END,
  CASE WHEN r.for_item_id = '' THEN r.item_id ELSE '' END
FROM request_tasks r LEFT JOIN request_tasks chained ON chained.item_id = r.for_item_id;

-- The lead of the team whose inbox a task arrives in decides on it, in a
-- decision turn like the one that send_task queues.
INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, goal_id, item_id, board_role, decision, created_at)
SELECT
  substr(r.turn_hex, 1, 8) || '-' || substr(r.turn_hex, 9, 4) || '-' || substr(r.turn_hex, 13, 4) || '-' || substr(r.turn_hex, 17, 4) || '-' || substr(r.turn_hex, 21, 12),
  t.lead_bot_id, 'room',
  'Team ' || COALESCE(asker.name, 'another team') || ' sent your team a task: "' || r.title || '". It was a request before requests became tasks. ' ||
    'Take it into your plan, ask its sender, defer it with later, pass it on, or push it back.',
  'queued', r.team_id, 0, r.task_id, '', 'lead', 1, CAST(strftime('%s', 'now') AS INTEGER) * 1000
FROM request_tasks r
JOIN goals g ON g.id = r.task_id
JOIN teams t ON t.team_id = r.team_id
JOIN room_members m ON m.room_id = r.team_id AND m.bot_id = t.lead_bot_id
LEFT JOIN bots asker ON asker.id = g.from_team_id
WHERE g.state = 'incoming';

-- Each task's history starts with its arrival, by whoever asked, and the
-- decision that the serving lead made on the request, by the app.
INSERT INTO goal_events (id, goal_id, kind, actor_id, actor_name, text, created_at)
SELECT
  lower(hex(randomblob(16))), r.task_id, 'sent', g.from_bot_id,
  COALESCE((SELECT name FROM bots WHERE id = g.from_bot_id), 'the user'), 'Sent as a request: ' || r.title, r.created_at
FROM request_tasks r JOIN goals g ON g.id = r.task_id;

INSERT INTO goal_events (id, goal_id, kind, actor_id, actor_name, text, created_at)
SELECT
  lower(hex(randomblob(16))), r.task_id,
  CASE r.intake WHEN 'accepted' THEN 'taken' WHEN 'deferred' THEN 'later' ELSE 'pushed_back' END,
  '', 'YarGPT', r.intake_note, r.updated_at
FROM request_tasks r WHERE r.intake IN ('accepted', 'deferred', 'declined');

UPDATE items SET
  team_id = (SELECT asking_team_id FROM request_tasks r WHERE r.item_id = items.id),
  state = 'todo', owner_id = '', reviewer_id = '', blocked_from = '', note = '', intake = '', intake_note = '', defer_revisit = '',
  defer_until = 0, slot_at = 0, jumped_at = 0, updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE id IN (SELECT item_id FROM request_tasks WHERE for_item_id = '' AND intake <> 'declined');

UPDATE items SET
  state = 'dropped', intake = 'withdrawn', slot_at = 0, jumped_at = 0,
  intake_note = 'It became a task of team ' || COALESCE((SELECT name FROM bots WHERE id = items.team_id), 'another team') || '.',
  updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE id IN (SELECT item_id FROM request_tasks WHERE for_item_id <> '' AND intake <> 'declined');

-- The serving members no longer work on another team's task.
UPDATE turns SET status = 'interrupted', error = 'the request became a task', finished_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE status = 'queued' AND board_role <> '' AND item_id IN (SELECT item_id FROM request_tasks);

DROP TABLE request_tasks;
