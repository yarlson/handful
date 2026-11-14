-- hues is the bot palette, in the order that breaks a tie: a new bot gets the
-- hue that the fewest bots have, the first one in this order on a tie. The UI
-- maps each name to its colours (ui/src/lib/bot-color.ts).
CREATE TABLE IF NOT EXISTS hues (
  name TEXT PRIMARY KEY,
  position INTEGER NOT NULL UNIQUE
) STRICT;
INSERT OR IGNORE INTO hues (name, position) VALUES
  ('sky', 0), ('emerald', 1), ('violet', 2), ('fuchsia', 3), ('lime', 4);

-- bot_hues holds the hue of each bot; rooms and teams have none. A table, not
-- a column, so running the migration again changes nothing.
CREATE TABLE IF NOT EXISTS bot_hues (
  bot_id TEXT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
  hue TEXT NOT NULL REFERENCES hues(name)
) STRICT;

-- Existing bots get a hue in creation order. From no hues, "the least used,
-- ties in palette order" deals the hues round: the Nth bot gets hue N mod 5.
-- A bot that has a hue keeps it, so a second run changes nothing, and a run
-- after a partial one gives the rest the hues a full run would.
INSERT INTO bot_hues (bot_id, hue)
SELECT a.id, h.name
FROM (
  SELECT id, ROW_NUMBER() OVER (ORDER BY created_at, rowid) - 1 AS n
  FROM bots WHERE kind = 'agent'
) a
JOIN hues h ON h.position = a.n % (SELECT COUNT(*) FROM hues)
WHERE NOT EXISTS (SELECT 1 FROM bot_hues bh WHERE bh.bot_id = a.id);
