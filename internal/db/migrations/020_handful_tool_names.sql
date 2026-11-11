-- Handful is YarGPT's new name, and its MCP server is now "handful": the
-- tools mcp__yargpt__<name> are mcp__handful__<name>. Rewrite the exact
-- token wherever a bot or the user saved a tool name. A row whose new text
-- another row already has, or gets from a row before it, is dropped, so the
-- unique keys hold. Running it again changes nothing.
DELETE FROM tool_grants
WHERE instr(tool_name, 'mcp__yargpt__') > 0
  AND EXISTS (
    SELECT 1 FROM tool_grants g
    WHERE g.bot_id = tool_grants.bot_id
      AND replace(g.tool_name, 'mcp__yargpt__', 'mcp__handful__') = replace(tool_grants.tool_name, 'mcp__yargpt__', 'mcp__handful__')
      AND (instr(g.tool_name, 'mcp__yargpt__') = 0 OR g.rowid < tool_grants.rowid)
  );
UPDATE tool_grants SET tool_name = replace(tool_name, 'mcp__yargpt__', 'mcp__handful__')
WHERE instr(tool_name, 'mcp__yargpt__') > 0;

DELETE FROM memories
WHERE instr(fact, 'mcp__yargpt__') > 0
  AND EXISTS (
    SELECT 1 FROM memories m
    WHERE m.scope_key = memories.scope_key
      AND replace(m.fact, 'mcp__yargpt__', 'mcp__handful__') = replace(memories.fact, 'mcp__yargpt__', 'mcp__handful__')
      AND (instr(m.fact, 'mcp__yargpt__') = 0 OR m.rowid < memories.rowid)
  );
UPDATE memories SET fact = replace(fact, 'mcp__yargpt__', 'mcp__handful__')
WHERE instr(fact, 'mcp__yargpt__') > 0;

UPDATE bots SET instructions = replace(instructions, 'mcp__yargpt__', 'mcp__handful__')
WHERE instr(instructions, 'mcp__yargpt__') > 0;
UPDATE teams SET agreement = replace(agreement, 'mcp__yargpt__', 'mcp__handful__')
WHERE instr(agreement, 'mcp__yargpt__') > 0;
UPDATE routines SET prompt = replace(prompt, 'mcp__yargpt__', 'mcp__handful__')
WHERE instr(prompt, 'mcp__yargpt__') > 0;
