-- The share of a team's member turns that are on no task (spec 9cf81412,
-- acceptance criterion 21), per team, for turns that started in [:from, :to)
-- (ms since the epoch).
--
-- Numerator: member turns in team rooms with no task, after inheritance
-- (goal_id = ''). Denominator: all member turns in team rooms.
-- Excluded from both:
--   * direct chats with the user, and turns in plain rooms (the room is no team);
--   * turns woken by a user post to the lead: the lead's room turns at hop 0
--     with no board role, which are the lead's own talk with the user;
--   * the lead's board turns (board_role 'lead'): its decisions and its other
--     wakes, such as hearing an answer or a result, which are board work.
-- Background-task report turns (source 'task') count, with the task they inherit.
--
-- Run by hand on a copy of the database, never on the live one:
--   sqlite3 -readonly copy.db
--   .param set :from 1794200000000
--   .param set :to   1794804800000
--   .read docs/measures/off-task-share.sql
--
-- The team's lead is the one the user chose while it is a member, else the
-- first member, as core.Bot.Lead decides. internal/db/query.sql holds the
-- same SELECT as OffTaskShare; a test keeps them equal.
SELECT tm.team_id,
  CAST(COALESCE(SUM(t.goal_id = ''), 0) AS INTEGER) AS off_task,
  COUNT(t.id) AS total
FROM teams tm JOIN turns t ON t.room_id = tm.team_id
WHERE t.started_at >= :from AND t.started_at < :to AND t.board_role <> 'lead'
  AND NOT (t.source = 'room' AND t.hops = 0 AND t.board_role = '' AND t.bot_id = COALESCE(
    (SELECT rm.bot_id FROM room_members rm WHERE rm.room_id = tm.team_id AND rm.bot_id = tm.lead_bot_id),
    (SELECT rm.bot_id FROM room_members rm JOIN bots b ON b.id = rm.bot_id WHERE rm.room_id = tm.team_id
     ORDER BY b.position, b.created_at, b.rowid LIMIT 1),
    ''))
GROUP BY tm.team_id
ORDER BY tm.team_id;
