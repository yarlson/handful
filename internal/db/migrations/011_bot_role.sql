-- role marks a built-in bot. 'builder' is the bot that sets up the user's
-- bots, teams and rooms; it alone gets the create_setup tool.
ALTER TABLE bots ADD COLUMN role TEXT NOT NULL DEFAULT '' CHECK (role IN ('', 'builder'));
