-- position orders bots, and separately rooms, in the sidebar. Room members
-- take their turns in the same order. Existing rows keep their creation order.
ALTER TABLE bots ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
UPDATE bots SET position = (
  SELECT COUNT(*) FROM bots b2
  WHERE b2.created_at < bots.created_at OR (b2.created_at = bots.created_at AND b2.rowid <= bots.rowid)
);
