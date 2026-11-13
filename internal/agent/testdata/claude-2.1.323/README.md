Stream-json output of Claude Code 2.1.323, recorded on 2026-11-13 with

    claude -p --input-format stream-json --output-format stream-json --verbose \
      --model haiku --setting-sources project --strict-mcp-config \
      --tools Read,Bash --allowedTools Read,Bash [--resume <session>]

- cold-first-turn.jsonl: a new session's first message, "Read a.txt with the Read tool, then run
  `ls` with Bash, then answer in one word." Its first call writes most of its context to the cache.
- warm-resumed-turn.jsonl: the next message, "Run `ls` with Bash again, then answer in one word.",
  on the resumed session; its calls read the context from the cache.

The message's uuid is COMMAND_UUID, which the fake claude replaces with the turn's. Session ids,
uuids, request ids, thinking signatures and paths were replaced; the lists of the init event are
emptied; the rate_limit_event keeps only its type.
