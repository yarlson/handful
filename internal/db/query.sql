-- name: CreateBot :one
INSERT INTO bots (id, name, instructions, kind, model, effort, position, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(b.position), 0) + 1 FROM bots b), ?, ?)
RETURNING *;

-- name: GetBot :one
SELECT * FROM bots WHERE id = ?;

-- name: ListBots :many
SELECT * FROM bots ORDER BY position, created_at, rowid;

-- name: SetBotRole :exec
UPDATE bots SET role = ? WHERE id = ?;

-- name: UpdateBotProfile :one
UPDATE bots SET name = ?, instructions = ?, model = ?, effort = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: SetBotSession :exec
UPDATE bots SET claude_session_id = ?, updated_at = ? WHERE id = ?;

-- name: DeleteBot :exec
DELETE FROM bots WHERE id = ?;

-- name: AppendEntry :one
INSERT INTO entries (bot_id, seq, rev, id, kind, turn_id, body, created_at)
VALUES (
  sqlc.arg(bot_id),
  (SELECT COALESCE(MAX(e.seq), 0) + 1 FROM entries e WHERE e.bot_id = sqlc.arg(bot_id)),
  (SELECT COALESCE(MAX(e.rev), 0) + 1 FROM entries e WHERE e.bot_id = sqlc.arg(bot_id)),
  sqlc.arg(id), sqlc.arg(kind), sqlc.arg(turn_id), sqlc.arg(body), sqlc.arg(created_at)
)
RETURNING *;

-- name: UpdateEntryBody :one
UPDATE entries
SET body = sqlc.arg(body),
    rev = (SELECT MAX(e.rev) + 1 FROM entries e WHERE e.bot_id = entries.bot_id)
WHERE entries.id = sqlc.arg(id)
RETURNING *;

-- name: GetEntry :one
SELECT * FROM entries WHERE id = ?;

-- name: ListEntriesAfterRev :many
SELECT * FROM entries
WHERE bot_id = ? AND rev > ?
ORDER BY rev
LIMIT ?;

-- name: CountTurnReplies :one
SELECT COUNT(*) FROM entries WHERE turn_id = ? AND kind = 'bot';

-- name: GetTurnUserEntryBody :one
SELECT body FROM entries WHERE bot_id = ? AND turn_id = ? AND kind = 'user' LIMIT 1;

-- name: InsertTurn :one
INSERT INTO turns (id, bot_id, source, client_nonce, prompt, status, room_id, hops, goal_id, item_id, board_role, decision, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: InsertRunningTurn :one
INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, goal_id, item_id, created_at, started_at)
VALUES (?, ?, ?, ?, 'running', ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetTurnTask :one
-- The task and step a turn works on, and what kind of board turn it is.
SELECT goal_id, item_id, board_role, decision FROM turns WHERE id = ?;

-- name: GetTurnByNonce :one
SELECT * FROM turns WHERE bot_id = ? AND client_nonce = ?;

-- name: ListQueuedTurns :many
SELECT * FROM turns WHERE status = 'queued' ORDER BY created_at, rowid LIMIT ?;

-- name: StartTurn :execrows
UPDATE turns SET status = 'running', started_at = ? WHERE id = ? AND status = 'queued';

-- name: FinishTurn :exec
UPDATE turns SET status = ?, error = ?, finished_at = ? WHERE id = ?;

-- name: AbandonRunningTurns :many
UPDATE turns SET status = 'interrupted', error = 'server restarted', finished_at = ?
WHERE status = 'running'
RETURNING *;

-- name: InsertMemory :execrows
INSERT INTO memories (id, scope_key, tier, fact, learned_by, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (scope_key, fact) DO NOTHING;

-- name: DeleteMemoryByFact :execrows
DELETE FROM memories WHERE scope_key = ? AND fact = ?;

-- name: DeleteMemory :execrows
DELETE FROM memories WHERE id = ?;

-- name: ListMemoriesForBot :many
SELECT * FROM memories
WHERE scope_key IN (sqlc.arg(bot_id), 'user', sqlc.arg(team_key))
ORDER BY created_at DESC;

-- name: DeleteMemoriesInScope :exec
DELETE FROM memories WHERE scope_key = ?;

-- name: PruneNotes :execrows
DELETE FROM memories WHERE tier = 'note' AND created_at < ?;

-- name: CreateRoutine :one
-- The count and the insert are one statement, so concurrent creates cannot
-- pass the limit together. No row comes back when the bot is at the limit.
INSERT INTO routines (id, bot_id, name, prompt, schedule, enabled, next_run_at, created_at, updated_at)
SELECT sqlc.arg(id), sqlc.arg(bot_id), sqlc.arg(name), sqlc.arg(prompt), sqlc.arg(schedule), sqlc.arg(enabled), sqlc.arg(next_run_at), sqlc.arg(created_at), sqlc.arg(updated_at)
WHERE (SELECT COUNT(*) FROM routines AS r WHERE r.bot_id = sqlc.arg(bot_id)) < CAST(sqlc.arg(max_routines) AS INTEGER)
RETURNING *;

-- name: GetRoutine :one
SELECT * FROM routines WHERE id = ?;

-- name: ListRoutines :many
SELECT * FROM routines WHERE bot_id = ? ORDER BY created_at, rowid;

-- name: UpdateRoutine :one
UPDATE routines
SET name = ?, prompt = ?, schedule = ?, enabled = ?, next_run_at = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeleteRoutine :execrows
DELETE FROM routines WHERE id = ?;

-- name: ListDueRoutines :many
SELECT * FROM routines WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?;

-- name: MarkRoutineFired :exec
UPDATE routines SET last_run_at = ?, last_run_by = ?, next_run_at = ? WHERE id = ?;

-- name: SetRoutineLastRun :exec
UPDATE routines SET last_run_at = ?, last_run_by = ? WHERE id = ?;

-- name: AddToolGrant :exec
INSERT INTO tool_grants (bot_id, tool_name, created_at) VALUES (?, ?, ?)
ON CONFLICT (bot_id, tool_name) DO NOTHING;

-- name: ListToolGrants :many
SELECT tool_name FROM tool_grants WHERE bot_id = ? ORDER BY tool_name;

-- name: DeleteToolGrant :execrows
DELETE FROM tool_grants WHERE bot_id = ? AND tool_name = ?;

-- name: ListOpenEntries :many
SELECT * FROM entries
WHERE (kind = 'approval' AND json_extract(body, '$.status') = 'pending')
   OR (kind = 'activity' AND json_extract(body, '$.status') = 'running');

-- name: CancelQueuedRoomTurns :exec
UPDATE turns SET status = 'interrupted', error = 'stopped by the user', finished_at = ?
WHERE room_id = ? AND status = 'queued';

-- name: CancelQueuedTurnsOf :exec
UPDATE turns SET status = 'interrupted', error = 'the bot was deleted', finished_at = ?
WHERE (bot_id = sqlc.arg(id) OR room_id = sqlc.arg(id)) AND status = 'queued';

-- name: AddRoomMember :exec
INSERT INTO room_members (room_id, bot_id) VALUES (?, ?)
ON CONFLICT (room_id, bot_id) DO NOTHING;

-- name: RemoveRoomMember :exec
DELETE FROM room_members WHERE room_id = ? AND bot_id = ?;

-- name: ListRoomMembers :many
SELECT rm.room_id, rm.bot_id, rm.claude_session_id, rm.last_seen_seq, b.name, b.model, b.effort
FROM room_members rm JOIN bots b ON b.id = rm.bot_id
WHERE rm.room_id = ?
ORDER BY b.position, b.created_at, b.rowid;

-- name: ListAllRoomMembers :many
SELECT rm.room_id, rm.bot_id, b.name, b.model, b.effort
FROM room_members rm JOIN bots b ON b.id = rm.bot_id
ORDER BY b.position, b.created_at, b.rowid;

-- name: ListRoomsOfBot :many
SELECT b.* FROM bots b JOIN room_members rm ON rm.room_id = b.id
WHERE rm.bot_id = ?
ORDER BY b.position, b.created_at, b.rowid;

-- name: SetRoomMemberSession :exec
UPDATE room_members SET claude_session_id = ? WHERE room_id = ? AND bot_id = ?;

-- name: SetRoomMemberSeen :exec
UPDATE room_members SET last_seen_seq = ? WHERE room_id = ? AND bot_id = ?;

-- name: ListConversationAfterSeq :many
SELECT * FROM entries
WHERE bot_id = ? AND seq > ? AND kind IN ('user', 'bot', 'agent')
ORDER BY seq
LIMIT ?;

-- name: ListLatestConversation :many
SELECT * FROM entries
WHERE bot_id = ? AND seq < ? AND kind IN ('user', 'bot', 'agent')
ORDER BY seq DESC
LIMIT ?;

-- name: InsertInboxMessage :exec
INSERT INTO agent_inbox (id, bot_id, from_bot_id, from_name, text, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListInbox :many
SELECT * FROM agent_inbox WHERE bot_id = ? ORDER BY created_at, rowid;

-- name: DeleteInboxMessage :exec
DELETE FROM agent_inbox WHERE id = ?;

-- name: ListBotsWithPendingApprovals :many
SELECT DISTINCT bot_id FROM entries
WHERE kind = 'approval' AND json_extract(body, '$.status') = 'pending';

-- name: CountOpenQuestions :many
-- How many open questions each bot or room has.
SELECT bot_id, COUNT(*) AS n FROM entries
WHERE kind = 'bot' AND json_extract(body, '$.questionState') = 'open'
GROUP BY bot_id;

-- name: ListOpenQuestionEntries :many
SELECT * FROM entries
WHERE bot_id = ? AND kind = 'bot' AND json_extract(body, '$.questionState') = 'open'
ORDER BY seq;

-- name: ListAllOpenQuestionEntries :many
SELECT * FROM entries
WHERE kind = 'bot' AND json_extract(body, '$.questionState') = 'open'
ORDER BY bot_id, seq;

-- name: ListClosedQuestionEntries :many
-- The questions of a chat that closed in the given way after a time (ms).
SELECT * FROM entries
WHERE bot_id = sqlc.arg(bot_id) AND kind = 'bot'
  AND json_extract(body, '$.questionState') = sqlc.arg(state)
  AND json_extract(body, '$.closedAt') > CAST(sqlc.arg(after) AS INTEGER)
ORDER BY seq;

-- name: ListClosedQuestionsSince :many
-- The questions of every chat that closed at or after a time (ms), newest first.
SELECT * FROM entries
WHERE kind = 'bot'
  AND json_extract(body, '$.questionState') IN ('answered', 'dismissed', 'withdrawn')
  AND json_extract(body, '$.closedAt') >= CAST(sqlc.arg(since) AS INTEGER)
ORDER BY json_extract(body, '$.closedAt') DESC, seq DESC;

-- name: SetBotPosition :exec
UPDATE bots SET position = ? WHERE id = ?;

-- name: InsertArtifact :one
INSERT INTO artifacts (id, kind, title, content, links, author_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetArtifact :one
SELECT * FROM artifacts WHERE id = ?;

-- name: UpdateArtifact :one
UPDATE artifacts
SET kind = ?, title = ?, content = ?, links = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeleteArtifact :execrows
DELETE FROM artifacts WHERE id = ?;

-- name: ListArtifactTags :many
SELECT tag FROM artifact_tags WHERE artifact_id = ? ORDER BY tag;

-- name: DeleteArtifactTags :exec
DELETE FROM artifact_tags WHERE artifact_id = ?;

-- name: InsertArtifactTag :exec
INSERT INTO artifact_tags (artifact_id, tag) VALUES (?, ?);

-- name: SearchArtifacts :many
SELECT id, kind, title, links, author_id, created_at, updated_at FROM artifacts a
WHERE (sqlc.narg(kind) IS NULL OR a.kind = sqlc.narg(kind))
  AND (sqlc.narg(author_id) IS NULL OR a.author_id = sqlc.narg(author_id))
  AND (sqlc.narg(tag) IS NULL OR EXISTS (SELECT 1 FROM artifact_tags t WHERE t.artifact_id = a.id AND t.tag = sqlc.narg(tag)))
  AND (sqlc.narg(query) IS NULL OR instr(lower(a.title), lower(sqlc.narg(query))) > 0 OR instr(lower(a.content), lower(sqlc.narg(query))) > 0)
ORDER BY a.updated_at DESC, a.rowid DESC
LIMIT sqlc.arg(max_rows);

-- name: ListTagsForArtifacts :many
SELECT artifact_id, tag FROM artifact_tags
WHERE artifact_id IN (sqlc.slice(ids))
ORDER BY artifact_id, tag;

-- name: CreateTeam :exec
INSERT INTO teams (team_id, lead_bot_id, agreement, goal_budget, default_model, default_effort, request_slots, inbox_max, inbox_per_sender, decision_minutes, daily_budget, budget_since) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTeam :one
SELECT * FROM teams WHERE team_id = ?;

-- name: ListTeams :many
SELECT * FROM teams;

-- name: UpdateTeam :exec
UPDATE teams SET lead_bot_id = ?, agreement = ?, goal_budget = ?, default_model = ?, default_effort = ?, request_slots = ?, inbox_max = ?, inbox_per_sender = ?, decision_minutes = ?, daily_budget = ? WHERE team_id = ?;

-- name: CountTeamTurnsBetween :one
-- The turns that count against a team's daily budget and started in
-- [from_ms, to_ms): every turn in the team's room (chat, inherited and board
-- work), or of a task of the team outside any room. Left out: the lead's
-- decisions and its other board wakes on no task, which are free.
SELECT COUNT(*) FROM turns t
WHERE t.started_at >= CAST(sqlc.arg(from_ms) AS INTEGER) AND t.started_at < CAST(sqlc.arg(to_ms) AS INTEGER)
  AND t.decision = 0 AND NOT (t.board_role = 'lead' AND t.goal_id = '')
  AND (t.room_id = sqlc.arg(team_id)
    OR (t.room_id = '' AND t.goal_id IN (SELECT g.id FROM goals g WHERE g.team_id = sqlc.arg(team_id))));

-- name: NoteTeamBudgetPause :execrows
-- Marks the local day whose budget pause the team's channel was told of; no
-- row changes when it was told already.
UPDATE teams SET budget_noted_on = sqlc.arg(day) WHERE team_id = sqlc.arg(team_id) AND budget_noted_on <> sqlc.arg(day);

-- name: WaitTeamBudget :execrows
-- The user waits out the team's budget pause until the local day ends.
UPDATE teams SET budget_waited_on = sqlc.arg(day) WHERE team_id = sqlc.arg(team_id);

-- name: SetRequestSlots :exec
UPDATE teams SET request_slots = ? WHERE team_id = ?;

-- name: InsertGoal :one
INSERT INTO goals (id, team_id, text, state, budget, queue_pos, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: InsertTask :one
-- A task that arrives in its team's inbox, or that the user starts at once.
INSERT INTO goals (id, team_id, text, state, budget, queue_pos, created_at, origin, from_team_id, from_bot_id, from_goal_id, brief, artifact_ids, depth, waits_on_item_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListIncomingGoals :many
SELECT * FROM goals WHERE team_id = ? AND state = 'incoming' ORDER BY created_at, rowid;

-- name: ListInboxGoals :many
-- The tasks in a team's inbox: waiting for a decision, asked, or deferred.
SELECT * FROM goals WHERE team_id = ? AND state IN ('incoming', 'later') ORDER BY created_at, rowid;

-- name: ListDoneGoals :many
-- A team's tasks that are done and wait for approval.
SELECT * FROM goals WHERE team_id = ? AND state = 'delivered' ORDER BY created_at, rowid;

-- name: SetGoalLater :execrows
UPDATE goals SET state = 'later', later_until = sqlc.arg(later_until), intake_note = sqlc.arg(intake_note), asked_at = 0,
  decision_ms = 0, decision_tick = 0, missed_at = 0
WHERE id = sqlc.arg(id) AND state = 'incoming';

-- name: ListDueLaterGoals :many
SELECT * FROM goals WHERE state = 'later' AND later_until <= sqlc.arg(now) ORDER BY later_until, rowid;

-- name: ReturnToInbox :execrows
-- A deferred task that came due, or a stopped one that is reopened, goes back to the inbox.
UPDATE goals SET state = 'incoming', later_until = 0, queue_pos = 0, pinned = 0, decision_ms = 0, decision_tick = 0, missed_at = 0
WHERE id = sqlc.arg(id) AND state = sqlc.arg(from_state);

-- name: PassGoal :execrows
UPDATE goals SET team_id = sqlc.arg(team_id), passes = passes + 1, passed_by = sqlc.arg(passed_by), intake_note = sqlc.arg(intake_note), asked_at = 0,
  decision_ms = 0, decision_tick = 0, missed_at = 0
WHERE id = sqlc.arg(id) AND state = 'incoming' AND team_id = sqlc.arg(from_team_id);

-- name: ListClockedGoals :many
-- The tasks whose decision clock may run: in an inbox, with no question to
-- the sender waiting, and not yet left to the user.
SELECT * FROM goals WHERE state = 'incoming' AND asked_at = 0 AND missed_at = 0
ORDER BY team_id, created_at, rowid;

-- name: SetDecisionClock :execrows
UPDATE goals SET decision_ms = sqlc.arg(decision_ms), decision_tick = sqlc.arg(decision_tick)
WHERE id = sqlc.arg(id) AND state = 'incoming' AND missed_at = 0;

-- name: MissDecision :execrows
UPDATE goals SET decision_ms = sqlc.arg(decision_ms), decision_tick = 0, missed_at = sqlc.arg(missed_at)
WHERE id = sqlc.arg(id) AND state = 'incoming' AND missed_at = 0;

-- name: ListNeedsUserGoals :many
-- The tasks that only the user decides on now: a missed decision, or asked
-- or passed on too often.
SELECT * FROM goals WHERE state = 'incoming' AND (missed_at > 0 OR ask_rounds > sqlc.arg(max_ask_rounds) OR passes > sqlc.arg(max_passes))
ORDER BY created_at, rowid;

-- name: ListDeliveredUserGoals :many
-- The tasks that are done and wait for the user's approval: the user's own,
-- and those the user handed over, which have no sending lead
-- (Goal.userApproves holds the same rule).
SELECT * FROM goals WHERE state = 'delivered' AND (origin = 'user' OR (origin = 'handoff' AND from_bot_id = ''))
ORDER BY created_at, rowid;

-- name: SetGoalPinned :execrows
UPDATE goals SET pinned = ? WHERE id = ? AND state = 'queued';

-- name: InsertGoalEvent :exec
INSERT INTO goal_events (id, goal_id, kind, actor_id, actor_name, text, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: LatestGoalEventAt :one
-- When the latest decision on a task was made (ms), or 0.
SELECT CAST(COALESCE(MAX(created_at), 0) AS INTEGER) FROM goal_events WHERE goal_id = ?;

-- name: ListGoalEvents :many
SELECT * FROM goal_events WHERE goal_id = ? ORDER BY created_at, rowid;

-- name: CountQueuedDecisionTurns :one
SELECT COUNT(*) FROM turns
WHERE bot_id = ? AND room_id = ? AND goal_id = ? AND board_role = 'lead' AND decision = 1 AND status = 'queued';

-- name: CountIncomingGoals :many
SELECT team_id, COUNT(*) AS n FROM goals WHERE state = 'incoming' GROUP BY team_id;

-- name: ListStoppedGoals :many
-- The stopped tasks of a team that were stopped since a time (ms), the one
-- stopped last first.
SELECT * FROM goals
WHERE team_id = sqlc.arg(team_id) AND state = 'stopped'
  AND (SELECT MAX(ev.created_at) FROM goal_events ev WHERE ev.goal_id = goals.id AND ev.kind = 'stopped') >= sqlc.arg(since)
ORDER BY (SELECT MAX(ev.created_at) FROM goal_events ev WHERE ev.goal_id = goals.id AND ev.kind = 'stopped') DESC, rowid DESC;

-- name: ListHandedOverGoals :many
-- The handoffs of a team since a time (ms), newest first.
SELECT * FROM goals WHERE origin = 'handoff' AND from_team_id = sqlc.arg(from_team_id) AND created_at >= sqlc.arg(since)
ORDER BY created_at DESC, rowid DESC;

-- name: AcceptHandoffGoal :execrows
UPDATE goals SET state = sqlc.arg(state), budget = sqlc.arg(budget), queue_pos = sqlc.arg(queue_pos), asked_at = 0
WHERE id = sqlc.arg(id) AND state = 'incoming';

-- name: AskAboutHandoff :execrows
UPDATE goals SET intake_note = sqlc.arg(intake_note), asked_at = sqlc.arg(asked_at), ask_rounds = sqlc.arg(ask_rounds), decision_tick = 0
WHERE id = sqlc.arg(id) AND state = 'incoming' AND ask_rounds = sqlc.arg(from_rounds);

-- name: AnswerHandoffGoal :execrows
UPDATE goals SET brief = sqlc.arg(brief), asked_at = 0
WHERE id = sqlc.arg(id) AND state = 'incoming' AND brief = sqlc.arg(from_brief);

-- name: DeclineHandoffGoal :execrows
UPDATE goals SET state = 'declined', intake_note = sqlc.arg(intake_note), asked_at = 0
WHERE id = sqlc.arg(id) AND state = 'incoming';

-- name: SetItemDependsOn :exec
UPDATE items SET depends_on = ? WHERE id = ?;

-- name: ListQueuedGoals :many
SELECT * FROM goals WHERE team_id = ? AND state = 'queued' ORDER BY queue_pos, rowid;

-- name: ActivateGoal :execrows
UPDATE goals SET state = 'active', budget = sqlc.arg(budget), queue_pos = 0
WHERE id = sqlc.arg(id) AND state = 'queued';

-- name: SetQueuePos :exec
UPDATE goals SET queue_pos = ? WHERE id = ? AND state = 'queued';

-- name: GetGoal :one
SELECT * FROM goals WHERE id = ?;

-- name: GetLatestGoal :one
-- A goal that never started (queued, incoming, declined, or one withdrawn
-- or cancelled before it started, which left its budget 0) is not the
-- team's latest goal, whoever sent it.
SELECT * FROM goals WHERE team_id = ? AND state NOT IN ('queued', 'incoming', 'later', 'declined')
  AND NOT (state = 'ended' AND budget = 0)
ORDER BY state IN ('active', 'paused') DESC, state = 'delivered' DESC, created_at DESC, rowid DESC
LIMIT 1;

-- name: ListOpenGoals :many
SELECT * FROM goals WHERE state IN ('active', 'delivered', 'paused') ORDER BY created_at, rowid;

-- name: SetGoalState :execrows
UPDATE goals SET state = sqlc.arg(state), queue_pos = 0
WHERE id = sqlc.arg(id) AND state = sqlc.arg(from_state);

-- name: SetGoalBudget :exec
UPDATE goals SET budget = ? WHERE id = ?;

-- name: GetTurnGoal :one
SELECT goal_id FROM turns WHERE id = ?;

-- name: CountGoalTurns :one
-- The goal's turns so far: its board turns in its own team's room. Another
-- team's work on a request of the goal counts on that team's daily budget.
SELECT COUNT(*) FROM turns
WHERE goal_id = sqlc.arg(goal_id) AND started_at IS NOT NULL AND board_role NOT IN ('', 'retro') AND decision = 0
  AND room_id = (SELECT g.team_id FROM goals g WHERE g.id = sqlc.arg(goal_id));

-- name: CancelQueuedGoalTurns :exec
UPDATE turns SET status = 'interrupted', error = 'the goal ended', finished_at = ?
WHERE goal_id = ? AND status = 'queued' AND board_role <> '';

-- name: InsertItem :one
INSERT INTO items (id, team_id, goal_id, title, brief, acceptance, state, owner_id, reviewer_id, depends_on, created_by, intake, for_item_id, position, created_at, updated_at)
VALUES (
  sqlc.arg(id), sqlc.arg(team_id), sqlc.arg(goal_id), sqlc.arg(title), sqlc.arg(brief), sqlc.arg(acceptance), 'todo',
  sqlc.arg(owner_id), sqlc.arg(reviewer_id), sqlc.arg(depends_on), sqlc.arg(created_by), sqlc.arg(intake), sqlc.arg(for_item_id),
  (SELECT COALESCE(MAX(i.position), 0) + 1 FROM items i WHERE i.goal_id = sqlc.arg(goal_id)),
  sqlc.arg(created_at), sqlc.arg(updated_at)
)
RETURNING *;

-- name: GetItem :one
SELECT * FROM items WHERE id = ?;

-- name: ListGoalItems :many
SELECT * FROM items WHERE goal_id = ? ORDER BY position;

-- name: UpdateItem :execrows
UPDATE items
SET state = sqlc.arg(state), owner_id = sqlc.arg(owner_id), reviewer_id = sqlc.arg(reviewer_id),
    result = sqlc.arg(result), note = sqlc.arg(note), blocked_from = sqlc.arg(blocked_from),
    review_rounds = sqlc.arg(review_rounds), updated_at = sqlc.arg(updated_at),
    intake = sqlc.arg(intake), intake_note = sqlc.arg(intake_note), defer_revisit = sqlc.arg(defer_revisit),
    defer_until = sqlc.arg(defer_until), jumped_at = sqlc.arg(jumped_at)
WHERE id = sqlc.arg(id) AND state = sqlc.arg(from_state) AND intake = sqlc.arg(from_intake);

-- name: SetItemSlot :exec
UPDATE items SET slot_at = ? WHERE id = ?;

-- name: ReleaseGoalSlots :exec
UPDATE items SET slot_at = 0 WHERE goal_id = ? AND slot_at > 0;

-- name: ListChainedItems :many
SELECT * FROM items WHERE for_item_id = ? ORDER BY created_at, rowid;

-- name: ListDueDeferrals :many
SELECT i.* FROM items i JOIN goals g ON g.id = i.goal_id
WHERE i.intake = 'deferred' AND i.defer_revisit = 'date' AND i.defer_until <= sqlc.arg(now)
  AND g.state IN ('active', 'delivered', 'paused')
ORDER BY i.created_at, i.rowid;

-- name: ListGoalEndDeferrals :many
SELECT i.* FROM items i JOIN goals g ON g.id = i.goal_id
WHERE i.team_id = sqlc.arg(team_id) AND g.team_id <> sqlc.arg(team_id) AND i.intake = 'deferred'
  AND i.defer_revisit = 'goal_end' AND g.state IN ('active', 'delivered', 'paused')
ORDER BY i.created_at, i.rowid;

-- name: StartItem :execrows
UPDATE items SET state = 'doing', updated_at = ?
WHERE id = ? AND owner_id = ? AND state IN ('todo', 'doing');

-- name: ListMemberItems :many
SELECT i.* FROM items i JOIN goals g ON g.id = i.goal_id
WHERE i.team_id = sqlc.arg(team_id) AND (i.owner_id = sqlc.arg(bot_id) OR i.reviewer_id = sqlc.arg(bot_id))
  AND i.state NOT IN ('done', 'dropped') AND g.state IN ('active', 'delivered', 'paused')
ORDER BY i.created_at, i.rowid;

-- name: CountReviewsBy :one
SELECT COUNT(*) FROM items i JOIN goals g ON g.id = i.goal_id
WHERE i.team_id = ? AND i.reviewer_id = ? AND i.state = 'review' AND g.state IN ('active', 'delivered', 'paused');

-- name: CancelQueuedTurnsOfBotInRoom :exec
UPDATE turns SET status = 'interrupted', error = 'the bot left the team', finished_at = ?
WHERE bot_id = ? AND room_id = ? AND status = 'queued';

-- name: CountOpenGoalTurns :one
SELECT COUNT(*) FROM turns WHERE goal_id = ? AND status IN ('queued', 'running') AND board_role <> '';

-- name: LastGoalTurnStatus :one
SELECT status FROM turns WHERE goal_id = ? AND status NOT IN ('queued', 'running') AND board_role <> ''
ORDER BY created_at DESC, rowid DESC LIMIT 1;

-- name: CountOpenTurnsOfBotInRoom :one
SELECT COUNT(*) FROM turns WHERE bot_id = ? AND room_id = ? AND status IN ('queued', 'running');

-- name: CancelTurn :exec
UPDATE turns SET status = 'interrupted', error = ?, finished_at = ? WHERE id = ? AND status = 'queued';

-- name: CancelQueuedItemTurns :exec
UPDATE turns SET status = 'interrupted', error = 'the item changed', finished_at = ?
WHERE item_id = ? AND status = 'queued' AND board_role <> '';

-- name: CountOpenItemTurns :one
SELECT COUNT(*) FROM turns
WHERE item_id = ? AND bot_id = ? AND status IN ('queued', 'running') AND board_role <> '';

-- name: CountQueuedLeadTurns :one
SELECT COUNT(*) FROM turns
WHERE bot_id = ? AND room_id = ? AND item_id = '' AND status = 'queued'
  AND (goal_id = ? OR goal_id = '') AND board_role <> 'retro';

-- name: ListTeamsWithPausedGoals :many
SELECT team_id FROM goals WHERE state = 'paused';

-- name: ListTeamRequests :many
-- The requests of other teams' open goals to a team, in arrival order.
SELECT sqlc.embed(i), g.team_id AS from_team_id, g.state AS goal_state FROM items i JOIN goals g ON g.id = i.goal_id
WHERE i.team_id = sqlc.arg(team_id) AND g.team_id <> sqlc.arg(team_id) AND g.state IN ('active', 'delivered', 'paused')
ORDER BY i.created_at, i.rowid;

-- name: ListClosedTeamRequests :many
-- The requests to a team that were declined or withdrawn since a time, also
-- of goals that are over.
SELECT sqlc.embed(i), g.team_id AS from_team_id, g.state AS goal_state FROM items i JOIN goals g ON g.id = i.goal_id
WHERE i.team_id = sqlc.arg(team_id) AND g.team_id <> sqlc.arg(team_id) AND i.intake IN ('declined', 'withdrawn')
  AND i.updated_at >= sqlc.arg(since) AND g.state NOT IN ('active', 'delivered', 'paused')
ORDER BY i.created_at, i.rowid;

-- name: QueueUserPostTurn :execrows
-- Queues the room turn that a user's post gives a member, unless one waits
-- already: that turn sees every post that came before it starts.
INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, goal_id, item_id, board_role, created_at)
SELECT sqlc.arg(id), sqlc.arg(bot_id), 'room', sqlc.arg(prompt), 'queued', sqlc.arg(room_id), 0, sqlc.arg(goal_id), '', '', sqlc.arg(created_at)
WHERE NOT EXISTS (
  SELECT 1 FROM turns
  WHERE bot_id = sqlc.arg(bot_id) AND room_id = sqlc.arg(room_id) AND source = 'room' AND status = 'queued'
    AND hops = 0 AND item_id = '' AND board_role = ''
);

-- name: InsertItemEvent :exec
INSERT INTO item_events (id, item_id, kind, actor_id, actor_name, round, text, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListItemEvents :many
SELECT * FROM item_events WHERE item_id = ? ORDER BY created_at, rowid;

-- name: PreviousTurnStart :one
SELECT CAST(COALESCE(MAX(started_at), 0) AS INTEGER) AS started FROM turns
WHERE bot_id = ? AND room_id = ? AND id != ? AND started_at IS NOT NULL;

-- name: ListOffTaskTurns :many
-- The turns of a team since a time (ms) that are on no task (spec 9cf81412
-- section 5 point 4), oldest first, with the first post of the bot in the
-- turn. Left out: the lead's board turns (decisions and its other wakes),
-- retros, and the turns of the lead that a user post woke, which are the
-- talk of the lead with the user.
SELECT t.id, t.bot_id, b.name, CAST(t.started_at AS INTEGER) AS started_at, t.prompt,
  CAST(COALESCE((SELECT json_extract(e.body, '$.text') FROM entries e
    WHERE e.bot_id = t.room_id AND e.turn_id = t.id AND e.kind = 'bot' AND json_extract(e.body, '$.speakerId') = t.bot_id
    ORDER BY e.seq LIMIT 1), '') AS TEXT) AS first_post
FROM turns t JOIN bots b ON b.id = t.bot_id
WHERE t.room_id = sqlc.arg(team_id) AND t.goal_id = '' AND t.board_role NOT IN ('lead', 'retro')
  AND t.started_at >= CAST(sqlc.arg(since) AS INTEGER)
  AND NOT (t.source = 'room' AND t.hops = 0 AND t.board_role = '' AND t.bot_id = sqlc.arg(lead_id))
ORDER BY t.started_at, t.rowid;

-- name: AttachOffTaskTurn :execrows
-- Attaches a turn of a team that is on no task to the task made from it. It
-- stays a conversation turn, with no board role, so it does not count
-- against the budget of the task.
UPDATE turns SET goal_id = sqlc.arg(goal_id)
WHERE id = sqlc.arg(id) AND room_id = sqlc.arg(team_id) AND goal_id = '' AND board_role NOT IN ('lead', 'retro')
  AND started_at IS NOT NULL;

-- name: OffTaskShare :many
-- The measure of acceptance criterion 21; docs/measures/off-task-share.sql
-- holds the same SELECT, and a test keeps them equal.
SELECT tm.team_id,
  CAST(COALESCE(SUM(t.goal_id = ''), 0) AS INTEGER) AS off_task,
  COUNT(t.id) AS total
FROM teams tm JOIN turns t ON t.room_id = tm.team_id
WHERE t.started_at >= sqlc.arg(from_ms) AND t.started_at < sqlc.arg(to_ms) AND t.board_role <> 'lead'
  AND NOT (t.source = 'room' AND t.hops = 0 AND t.board_role = '' AND t.bot_id = COALESCE(
    (SELECT rm.bot_id FROM room_members rm WHERE rm.room_id = tm.team_id AND rm.bot_id = tm.lead_bot_id),
    (SELECT rm.bot_id FROM room_members rm JOIN bots b ON b.id = rm.bot_id WHERE rm.room_id = tm.team_id
     ORDER BY b.position, b.created_at, b.rowid LIMIT 1),
    ''))
GROUP BY tm.team_id
ORDER BY tm.team_id;
