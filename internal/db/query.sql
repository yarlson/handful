-- name: CreateBot :one
INSERT INTO bots (id, name, instructions, kind, position, created_at, updated_at)
VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(b.position), 0) + 1 FROM bots b), ?, ?)
RETURNING *;

-- name: GetBot :one
SELECT * FROM bots WHERE id = ?;

-- name: ListBots :many
SELECT * FROM bots ORDER BY position, created_at, rowid;

-- name: SetBotRole :exec
UPDATE bots SET role = ? WHERE id = ?;

-- name: UpdateBotProfile :one
UPDATE bots SET name = ?, instructions = ?, updated_at = ?
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
INSERT INTO turns (id, bot_id, source, client_nonce, prompt, status, room_id, hops, goal_id, item_id, board_role, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: InsertRunningTurn :one
INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, created_at, started_at)
VALUES (?, ?, ?, ?, 'running', ?, ?, ?, ?)
RETURNING *;

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
INSERT INTO routines (id, bot_id, name, prompt, schedule, enabled, next_run_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
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

-- name: AddRoomMember :exec
INSERT INTO room_members (room_id, bot_id) VALUES (?, ?)
ON CONFLICT (room_id, bot_id) DO NOTHING;

-- name: RemoveRoomMember :exec
DELETE FROM room_members WHERE room_id = ? AND bot_id = ?;

-- name: ListRoomMembers :many
SELECT rm.room_id, rm.bot_id, rm.claude_session_id, rm.last_seen_seq, b.name
FROM room_members rm JOIN bots b ON b.id = rm.bot_id
WHERE rm.room_id = ?
ORDER BY b.position, b.created_at, b.rowid;

-- name: ListAllRoomMembers :many
SELECT rm.room_id, rm.bot_id, b.name
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

-- name: ListBotsAwaitingReply :many
SELECT e.bot_id FROM entries e
WHERE e.kind = 'bot'
  AND json_extract(e.body, '$.needsReply') = 1
  AND e.seq = (SELECT MAX(x.seq) FROM entries x WHERE x.bot_id = e.bot_id AND x.kind IN ('user', 'bot'));

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
INSERT INTO teams (team_id, lead_bot_id, agreement, goal_budget) VALUES (?, ?, ?, ?);

-- name: GetTeam :one
SELECT * FROM teams WHERE team_id = ?;

-- name: ListTeams :many
SELECT * FROM teams;

-- name: UpdateTeam :exec
UPDATE teams SET lead_bot_id = ?, agreement = ?, goal_budget = ? WHERE team_id = ?;

-- name: InsertGoal :one
INSERT INTO goals (id, team_id, text, state, budget, created_at)
VALUES (?, ?, ?, 'active', ?, ?)
RETURNING *;

-- name: GetGoal :one
SELECT * FROM goals WHERE id = ?;

-- name: GetLatestGoal :one
SELECT * FROM goals WHERE team_id = ?
ORDER BY state IN ('active', 'delivered', 'paused') DESC, created_at DESC, rowid DESC
LIMIT 1;

-- name: ListOpenGoals :many
SELECT * FROM goals WHERE state IN ('active', 'delivered', 'paused') ORDER BY created_at, rowid;

-- name: SetGoalState :execrows
UPDATE goals SET state = sqlc.arg(state)
WHERE id = sqlc.arg(id) AND state = sqlc.arg(from_state);

-- name: SetGoalBudget :exec
UPDATE goals SET budget = ? WHERE id = ?;

-- name: PauseGoalAtBudget :execrows
UPDATE goals SET state = 'paused'
WHERE id = sqlc.arg(id) AND state = 'active' AND budget <= sqlc.arg(used);

-- name: CountGoalTurns :one
SELECT COUNT(*) FROM turns
WHERE goal_id = ? AND started_at IS NOT NULL AND board_role <> 'retro';

-- name: CancelQueuedGoalTurns :exec
UPDATE turns SET status = 'interrupted', error = 'the goal ended', finished_at = ?
WHERE goal_id = ? AND status = 'queued';

-- name: InsertItem :one
INSERT INTO items (id, team_id, goal_id, title, brief, acceptance, state, owner_id, reviewer_id, depends_on, created_by, position, created_at, updated_at)
VALUES (
  sqlc.arg(id), sqlc.arg(team_id), sqlc.arg(goal_id), sqlc.arg(title), sqlc.arg(brief), sqlc.arg(acceptance), 'todo',
  sqlc.arg(owner_id), sqlc.arg(reviewer_id), sqlc.arg(depends_on), sqlc.arg(created_by),
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
    review_rounds = sqlc.arg(review_rounds), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND state = sqlc.arg(from_state);

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
SELECT COUNT(*) FROM turns WHERE goal_id = ? AND status IN ('queued', 'running');

-- name: LastGoalTurnStatus :one
SELECT status FROM turns WHERE goal_id = ? AND status NOT IN ('queued', 'running')
ORDER BY created_at DESC, rowid DESC LIMIT 1;

-- name: CountOpenTurnsOfBotInRoom :one
SELECT COUNT(*) FROM turns WHERE bot_id = ? AND room_id = ? AND status IN ('queued', 'running');

-- name: CancelTurn :exec
UPDATE turns SET status = 'interrupted', error = ?, finished_at = ? WHERE id = ? AND status = 'queued';

-- name: CancelQueuedItemTurns :exec
UPDATE turns SET status = 'interrupted', error = 'the item changed', finished_at = ?
WHERE item_id = ? AND status = 'queued';

-- name: CountOpenItemTurns :one
SELECT COUNT(*) FROM turns
WHERE item_id = ? AND bot_id = ? AND status IN ('queued', 'running');

-- name: CountQueuedLeadTurns :one
SELECT COUNT(*) FROM turns
WHERE bot_id = ? AND room_id = ? AND item_id = '' AND status = 'queued'
  AND (goal_id = ? OR goal_id = '') AND board_role <> 'retro';

-- name: ListTeamsWithPausedGoals :many
SELECT team_id FROM goals WHERE state = 'paused';

-- name: ListTeamRequests :many
SELECT sqlc.embed(i), g.team_id AS from_team_id FROM items i JOIN goals g ON g.id = i.goal_id
WHERE i.team_id = sqlc.arg(team_id) AND g.team_id <> sqlc.arg(team_id) AND g.state IN ('active', 'delivered', 'paused')
ORDER BY i.created_at, i.rowid;
