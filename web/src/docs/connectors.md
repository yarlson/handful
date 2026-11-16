# Connectors

After this page, you can give a bot the tools of an external MCP server, such as Slack or a wiki, and sign in to it.

A connector is an MCP server that you list in `~/.handful/connectors.json`. Each server names the bots that load it.

## Add a server

1. Write the file:

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

   The bot id is the last part of the bot's chat address, `/chat/<bot id>`.

2. Make the file private: `chmod 600 ~/.handful/connectors.json`. The file can hold tokens, so Handful does not read it with any other mode.
3. Sign in to each server that has no `headers`: click **Connectors** at the bottom of the sidebar, then **Connect**, and sign in on the page that opens. In **Settings**, the section **Connectors** has **Manage connectors…**, which opens the same list.

Handful reads the file each time a bot starts a Claude Code process, so a change needs no restart.

## Sign-in

A server without `headers` signs in with OAuth 2.1, with dynamic client registration and PKCE.

- The **Connectors** list shows each server, its address, its bots and its state: "token in file", "needs sign-in", "connected" or "expired". **Reconnect** signs in again. **Disconnect** deletes the sign-in.
- Handful keeps the sign-in tokens in `~/.handful/oauth/<name>.json`, which only you can read, and refreshes them before they expire.
- While a server is not signed in, the bot works without it, and the chat says "<name> needs sign-in: open Connectors in the sidebar".
- The sign-in address contains Handful's port. If you change `-addr`, connect again.

A server without dynamic client registration needs a client that you register yourself:

```json
"oauth": {"clientId": "<id>", "clientSecret": "<secret, if any>", "scopes": ["<scope>"], "callbackPort": 3118}
```

Every `oauth` field is optional, but `clientSecret` needs `clientId`. With `callbackPort`, from 1024 to 65535, the sign-in comes back to `http://localhost:<callbackPort>/callback`, as in Claude Code's `.mcp.json`.

For example, Slack's MCP server works with Slack's public client for Claude Code ([Slack docs](https://docs.slack.dev/ai/slack-mcp-server/connect-to-claude)). A Slack workspace admin must approve it first:

```json
"slack": {
  "url": "https://mcp.slack.com/mcp",
  "oauth": {"clientId": "1601185624273.8899143856786", "callbackPort": 3118},
  "bots": ["<bot id>"]
}
```

## Rules

- A server name uses only `a-z`, `0-9` and `-`. The name `handful` is reserved. A bot calls the server's tools as `mcp__<name>__<tool>`.
- The `url` must use https. Plain http works only for `localhost`, `127.0.0.1` and `::1`.
- A server has `headers` or `oauth`, not both. A server with `headers` sends them on each call. For a server that needs no sign-in, set `"headers": {}`.
- An invalid file fails the bot's turn, with an error that names the problem.

## What a connector can do

- A bot asks you with an approval card before it calls a connector tool. "Always allow <tool>" on the card is for that one tool of the server, not for the whole server.
- `bots` chooses the bots that load a server. It does not hide the tokens from other bots. Claude Code's file tools cannot read `connectors.json` or the sign-in tokens, but every bot runs as your user, so a command that you allow can. See [Security and data](/security/).
