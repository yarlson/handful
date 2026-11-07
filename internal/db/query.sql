-- name: CreateBot :one
INSERT INTO bots (id, name, instructions, kind, position, created_at, updated_at)
VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(b.position), 0) + 1 FROM bots b), ?, ?)
RETURNING *;

-- name: GetBot :one
SELECT * FROM bots WHERE id = ?;

-- name: ListBots :many
SELECT * FROM bots ORDER BY position, created_at, rowid;

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
INSERT INTO turns (id, bot_id, source, client_nonce, prompt, status, room_id, hops, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: InsertRunningTurn :one
INSERT INTO turns (id, bot_id, source, prompt, status, room_id, created_at, started_at)
VALUES (?, ?, ?, ?, 'running', ?, ?, ?)
RETURNING *;

-- name: GetTurnByNonce :one
SELECT * FROM turns WHERE bot_id = ? AND client_nonce = ?;

-- name: ListQueuedTurns :many
SELECT * FROM turns WHERE status = 'queued' ORDER BY created_at, rowid LIMIT ?;

-- name: StartTurn :exec
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
WHERE scope_key IN (sqlc.arg(bot_id), 'user')
ORDER BY created_at DESC;

-- name: DeleteAgentMemories :exec
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
