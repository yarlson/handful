Stream-json output of Claude Code 2.1.325, recorded on 2026-11-14 with

    claude -p --input-format stream-json --output-format stream-json --verbose \
      --model haiku --setting-sources project --strict-mcp-config \
      --tools Read,Edit,Write --allowedTools Read,Edit,Write

- edit-and-write.jsonl: in a folder with a.txt ("one\ntwo\nthree\n") and b.txt ("old first\nold
  second", no newline at the end), the message "1) Read a.txt and b.txt. 2) Edit a.txt: replace the
  line two with TWO. 3) Write a new file c.txt with the content: alpha, newline, beta, newline.
  4) Write b.txt with the content: new first, newline, old second (no trailing newline)." Each
  tool_result comes in its own user event, whose tool_use_result holds the Edit's and the update's
  structuredPatch and the created file's content.

The message's uuid is COMMAND_UUID, which the fake claude replaces with the turn's. Session ids,
uuids, request ids and paths were replaced; the lists of the init event are emptied; the
rate_limit_event keeps only its type.

- haiku-helper.jsonl: a turn whose bot starts an executor helper that runs two shell commands,
  recorded on 2026-11-16 with a parent at haiku. Its rate_limit_event is whole: the weekly window
  at 0.78 with a warning at 0.75. Only the message's uuid was replaced.
