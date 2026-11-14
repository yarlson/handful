-- entries_messages lists a chat's user, bot and agent messages in seq order,
-- so that chat search reads only messages, not the tool calls, approvals and
-- events between them.
CREATE INDEX IF NOT EXISTS entries_messages ON entries(bot_id, seq)
WHERE kind IN ('user', 'bot', 'agent');
