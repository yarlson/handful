# Connectors

A connector gives bots the tools of an external MCP server, such as Slack or a wiki. List the servers in `~/.handful/connectors.json`:

```json
{
  "servers": {
    "wiki": {
      "url": "https://wiki.example/mcp",
      "bots": ["<bot id>"]
    },
    "search": {
      "url": "https://search.example/mcp",
      "headers": {"Authorization": "Bearer <token>"},
      "bots": ["<bot id>"]
    }
  }
}
```

1. Make the file private: `chmod 600 ~/.handful/connectors.json`. The file can hold tokens, so Handful does not read it otherwise.
2. Sign in to each server that has no `headers`: click **Connectors** at the bottom of the sidebar, then **Connect**, and sign in on the page that opens. Handful uses OAuth 2.1, with dynamic client registration and PKCE.

Rules:

- A server with `headers` gets them on each call. For a server that needs no sign-in, set `"headers": {}`.
- A server name uses only `a-z`, `0-9` and `-`. The name `handful` is reserved. A bot calls the server's tools as `mcp__<name>__<tool>`.
- The `url` must use https. Plain http is allowed only for `localhost`, `127.0.0.1` and `::1`.
- `bots` lists the bots that load the server. It does not hide the tokens from other bots (see [Security](../README.md#security)).
- Connector tools go through approval cards. **Always allow** applies to one tool, not to the whole server.
- Handful reads the file each time a bot starts a Claude Code process, so a change needs no restart. An invalid file fails the bot's turn with an error that names the problem.
- Handful keeps sign-in tokens in `~/.handful/oauth/<name>.json`, with mode 0600, and refreshes them when they are about to expire. **Disconnect** deletes them.
- When a server is not signed in, the bot works without it, and the chat says that the server needs sign-in.
- If you change `-addr`, connect again: the sign-in address contains the port.

A server without dynamic client registration needs a client that you register yourself:

```json
"oauth": {"clientId": "<id>", "clientSecret": "<secret, if any>", "scopes": ["<scope>"], "callbackPort": 3118}
```

All `oauth` fields are optional. With `callbackPort`, the sign-in returns to `http://localhost:<callbackPort>/callback`, as in Claude Code's `.mcp.json`.

For example, Slack's MCP server works with Slack's public client for Claude Code ([Slack docs](https://docs.slack.dev/ai/slack-mcp-server/connect-to-claude)). A Slack workspace admin must approve it first:

```json
"slack": {
  "url": "https://mcp.slack.com/mcp",
  "oauth": {"clientId": "1601185624273.8899143856786", "callbackPort": 3118},
  "bots": ["<bot id>"]
}
```
