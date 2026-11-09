Stream-json output of Claude Code 2.1.319, recorded on 2026-11-09 with

    echo '{"type":"user","message":{"role":"user","content":"Reply with just: ok"}}' |
      claude -p --input-format stream-json --output-format stream-json --verbose \
        --setting-sources project --strict-mcp-config <flags>

- haiku-effort-low.jsonl: `--model haiku --effort low`
- haiku-effort-max.jsonl: `--model haiku --effort max`
- anthropic-model-sonnet-flag-haiku.jsonl: `ANTHROPIC_MODEL=sonnet` in the environment and `--model haiku`

Session ids, uuids, request ids, paths, and the lists of tools, skills,
plugins and MCP servers were replaced; the rate_limit_event keeps only its
type. All keys of the init event are kept.
