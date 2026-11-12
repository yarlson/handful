## Bot tools

Each `claude` process connects to Handful's MCP endpoint, `/mcp`, with a bearer token that is valid only for its turn. The bots use these tools. They are not for external clients.

- `send_to_user`: send a message to the user. This is the only output the user sees. In a room turn, it posts into the room.
  - `text`: markdown. It can be empty when files are attached.
  - `files`: up to 4 paths of images or videos.
  - `needs_reply`: true when the bot cannot continue without an answer. The app then shows that the bot waits for you.
- `send_to_agent`: message another bot, or post into a room that the bot is in. In a team turn, the target is the team itself, or a room that the bot is in when the post wakes no one (a post wakes only the members it @mentions).
  - `target_id`: the id of a bot, or of a room.
  - `message`: at most 8000 characters.
  - `priority`: true wakes the bot now. False delivers at its next turn. Room posts always land at once.
  - `files`: room posts only, up to 4.
- `read_room`: read the latest messages of a room that the bot is in, oldest first.
  - `room_id`.
  - `limit`: 1 to 50, default 20.
  - `before_seq`: page back from the oldest `#seq` that the bot has.
  - Reading does not change what the bot's next room turn shows as new.
- `recall_memory`: search the bot's memory and the shared user memory.
  - `query`: facts that share more words rank first.
  - `scope`: `agent`, `user`, `team` (only in a team turn) or `all` (default). In a team turn, `all` includes the team's facts.
  - `limit`: 1 to 50, default 20.
- `update_memory`: write or forget a fact.
  - `action`: `write` or `forget`.
  - `fact`: for forget, the exact recorded text.
  - `scope`: `agent` (default), `user` or `team` (only in a team turn).
  - `tier`: `profile`, `log` (default) or `note`.
  - `replaces`: the exact text of a fact that the new fact replaces.
- `update_routine`: manage the bot's own routines.
  - `action`: `create`, `update`, `pause`, `resume`, `delete`, `run_now` or `list`.
  - `id`: required except for create and list.
  - `name`, `prompt`, `schedule`: required for create.
- `update_skill`: manage the skills that all bots share.
  - `action`: `write`, `delete` or `list`.
  - `name`, `description`, `body`: required for write.
  - `id`: required for delete.
- `find_artifacts`: list the artifacts, newest updated first, without their content.
  - `kind`: `plan`, `task`, `issue`, `review`, `report` or `note`.
  - `author`: `me`, `user` or a bot id.
  - `tag`: one exact tag.
  - `query`: text in the title or content, ignoring case.
  - `limit`: 1 to 50, default 20.
- `read_artifact`: read one artifact by `id`, with its content inside an `<artifact>` wrapper.
- `update_artifact`: save, change or delete an artifact.
  - `action`: `write` or `delete`.
  - `id`: leave it out to create an artifact. Required to change or delete one, which only its author can do.
  - `kind`, `title`, `content`: required to create. When the bot changes an artifact, a field that it leaves out stays the same.
  - `tags`, `links`: replace the old tags or links.
- `update_profile`: change the bot's own `name` or `instructions`. A field that the bot leaves out stays the same.
- `create_setup`: Builder only. Create new bots, teams and rooms after the user confirms them on a card. See the Builder part of Bots and rooms.
  - `bots`: each with `name` and `instructions`, and optionally `model` and `effort`.
  - `teams`: each with `name`, `purpose`, `members` (bot names), `lead` (a member's name; the first member when empty), `agreement`, and optionally `default_model` and `default_effort`.
  - `rooms`: each with `name`, `purpose` and `members`.
- `update_board`: work on the team's board, only in a turn in the team. See the Teams part of the docs above.
  - `action`, for tasks: `send_task`, `take`, `order`, `ask`, `answer_task`, `later`, `pass_on`, `push_back`, `propose`, `make_task`, `close`, `approve`, `stop` and `withdraw`. For steps: `create`, `assign`, `submit`, `block`, `unblock`, `drop`, `pass` and `changes`. Also `list`.
  - `goal_id`: the task, for every task action except `send_task`, `propose` and `make_task`.
  - `item_id`: the step, for every step action except `create`.
  - `team_id`: for `send_task` and `pass_on`, the other team.
  - `text`: the task in a line, the result, a question, an answer, a reason or a note. `brief`, `title`, `acceptance` and `depends_on` are for `send_task`, `propose`, `make_task` and `create`.
  - `position`: for `take`, the task's place in the plan. `order`: for `order`, the ids of every task in the plan.
  - `until`: for `later`, when the task returns to the inbox, at most 7 days ahead.
  - `waits_for`: for `send_task`, the id of your own step that waits on the task. `artifact_ids`: artifacts the other team needs.
  - `turn_ids`: for `make_task`, the ids of the team's turns that were on no task.
  - `owner_id`, `reviewer_id`: for `create` and `assign`, by the lead only.
- `approve`: internal. Claude Code calls it through `--permission-prompt-tool` to show an approval card.

## Local HTTP API

The web UI uses this API. It is local and not versioned, so it can change between releases.

Rules for every request:

- The server answers only on a loopback address. A request whose `Host` is not loopback gets 403, to block DNS rebinding.
- A request with an `Origin` header from another origin gets 403, to block cross-site request forgery.
- Every `POST` under `/api/` must have `Content-Type: application/json`, or it gets 415.
- A request body has at most 1 MiB. A message body may also carry its files, up to 100 MB before base64.
- Every write under `/api/`, that is any method other than `GET`, `HEAD` or `OPTIONS`, must carry Handful's key, or it gets 401 with `{"error": "This needs Handful's key."}`. The key is the content of the file `ui-key` in the data folder (`~/.handful/ui-key`, or `<data>/ui-key` with `-data`). A program sends it in the header `X-Handful-Key`. A browser sends it in the cookie `handful_key`, which a browser gets after you paste the key once on the key page, and which Handful.app sets itself. Reads need no key. The one write without the key is `POST /api/key`, which checks a pasted key and sets that cookie.
- An error is JSON: `{"error": "<message>"}`. Status 400 means invalid input, 404 not found, and 409 a conflict, for example an approval that is no longer pending.

For example, to send a message to a bot from a script:

```sh
curl -s -X POST http://127.0.0.1:7777/api/bots/<bot id>/messages \
  -H "X-Handful-Key: $(cat ~/.handful/ui-key)" \
  -H 'Content-Type: application/json' \
  -d '{"text": "Hello", "clientNonce": "<unique id>"}'
```

Bots, rooms and teams are the same resource. `kind` is `agent` for a bot and `room` for a room or a team. A team also has `team`.

Bots:

- `GET /api/bots`: all bots, rooms and teams. Each has `id`, `kind`, `name`, `instructions`, `role` (`builder` for Builder; absent for other bots), `members` (rooms), `team` (teams: `leadId`, `agreement`, `decisionMinutes`, `dailyBudget` and the other team settings), `attention` (`approval` or `question`; the UI also uses `goal` for a paused task and `model` for a model that needs a look), `running`, `tasks`, `createdAt` and `updatedAt`.
- `POST /api/bots` with `{"name", "instructions"}`: create a bot. Returns 201 and the bot.
- `GET /api/bots/{id}`: one bot or room.
- `PATCH /api/bots/{id}` with `{"name"?, "instructions"?}`: change a bot.
- `DELETE /api/bots/{id}`: stop the bot's running turn, then delete it. Returns 204.
- `PUT /api/bots/order` with `{"ids": [...]}`: set the order of one sidebar section: all bots, all teams or all plain rooms. Room members answer in the order of the bots section.

Chats:

- `GET /api/bots/{id}/entries?after=<rev>`: the entries of a chat that changed after `rev`, in `rev` order, at most 1000 at a time. Start with `after=0`.
- `POST /api/bots/{id}/messages` with `{"text", "clientNonce", "media"?}`: send a message to a bot or a room.
  - `clientNonce` is required, 1 to 100 characters, and unique for each message.
  - `media` is a list of base64-encoded files.
  - The response is 202 with `{"turnId", "duplicate": false}`. A repeated `clientNonce` starts no second turn: it returns 200 with the first turn's id and `"duplicate": true`. The same `clientNonce` with other text returns 409.
- `GET /api/media/{name}`: a file of a message.
- `POST /api/entries/{entry id}/reactions` with `{"emoji", "on"}`: put a reaction on a `user`, `bot` or `agent` entry (`"on": true`) or take it off (`"on": false`). `emoji` is one of 👍 👎 ❤️ 😂 👀 ✅. Returns 200 and the entry. Setting a reaction that is already on, or removing one that is off, changes nothing. Returns 400 for another emoji or another kind of entry, and 404 for an unknown entry. A reaction wakes no bot.
- `POST /api/bots/{id}/interrupt`: stop the running turn. Returns `{"stopped": true|false}`.
- `POST /api/bots/{id}/clear`: start a fresh context. Returns 204, or 409 while the bot works or has background tasks.
- `POST /api/bots/{id}/compact`: queue a compaction after the current turn. Returns 202 with `{"queued"}`.

Entries:

- Each entry has `id`, `botId`, `seq` (its place in the chat), `rev`, `kind`, `turnId`, `body` and `createdAt`.
- An entry can change after it is created, for example when an approval is answered. Each change gives it a new, higher `rev`, so update entries by `id`.
- Kinds:
  - `user`: your message. `body` has `text` and `media` (file names for `GET /api/media/{name}`).
  - `bot`: a bot's message. `body` has `text`, `fallback`, `needsReply` and `media`. In a room, it also has `speakerId` and `speakerName`.
  - `agent`: a direct message between bots. `body` has `direction` (`in` or `out`), `peerId`, `peerName`, `text`, `priority`, and for `in`, `held`.
  - The bodies of `user`, `bot` and `agent` entries can also have `reactions`, your emoji on the message, in the order 👍 👎 ❤️ 😂 👀 ✅. The key is absent when the message has no reactions.
  - `approval`: an approval card. `body` has `tool`, `input`, `status` (`pending`, `allowed`, `denied` or `expired`), `decision`, and in a room `bot`.
  - `event` and `activity`: turn events and the tools that a bot used.

Rooms:

- `POST /api/rooms` with `{"name", "instructions", "memberIds"}`: create a room. Returns 201 and the room.
- `PUT /api/rooms/{id}/members` with `{"memberIds"}`: set the members.
- Send a message to a room with `POST /api/bots/{room id}/messages`.

Teams and tasks:

- `POST /api/teams` with `{"name", "instructions", "memberIds", "leadId"?, "agreement"?}`: create a team. `instructions` is its purpose. Without `leadId`, the first member leads.
- `PATCH /api/teams/{id}` with any of `{"leadId", "agreement", "defaultModel", "defaultEffort", "decisionMinutes", "dailyBudget", "inboxMax", "inboxPerSender"}`: change a team. The lead must be a member. `dailyBudget` is the turns the team may start in a day; 0 is no limit.
- Set the members with `PUT /api/rooms/{team id}/members`, and change the name or the purpose with `PATCH /api/bots/{team id}`.
- `GET /api/teams/{id}/board`: the team's work: `goal` (the Now task, or the latest one; null when there is none), `items` (its steps), `inbox`, `plan`, `done`, `stopped`, `handedOver` (tasks the team sent to other teams), `offTask` (turns on no task in the last hour), `budget` (the team's turns today) and `teams` (names of the teams that appear).
- `POST /api/teams/{id}/tasks` with `{"text", "brief"?, "artifactIds"?, "startNow"?}`: send the team a task as the user. It lands in the team's inbox, or at the top of the plan with `startNow`.
- `POST /api/teams/{id}/tasks/from-turns` with `{"text", "brief"?, "turnIds"}`: make a task of 1 to 50 of the team's turns that were on no task.
- `GET /api/teams/{id}/off-task`: the team's turns on no task. `POST /api/teams/{id}/off-task/dismiss` puts them away.
- `POST /api/teams/{id}/plan` with `{"order"}`: set the order of the plan.
- `POST /api/teams/{id}/budget/wait`: wait until midnight at the team's daily limit.
- `POST /api/tasks/{id}/take` with `{"position"?}`: take a task from the inbox into the plan.
- `POST /api/tasks/{id}/push-back` with `{"text"}`, `POST /api/tasks/{id}/answer` with `{"text"}` and `POST /api/tasks/{id}/changes` with `{"text"}`: push a task back with a reason, answer a lead's question, or ask for changes to a done task.
- `POST /api/tasks/{id}/pin` and `DELETE /api/tasks/{id}/pin`: pin a task to its place in the plan, or release it.
- `POST /api/tasks/{id}/stop`, `/reopen`, `/withdraw`, `/dismiss` and `/approve`: stop a task, reopen a stopped one, take back a task that the team sent, put away a pushed-back task, or approve a done one.
- `GET /api/tasks/{id}/events` and `GET /api/items/{id}/events`: the history of a task and of a step.
- `GET /api/needs-you`: what only the user can settle: results to approve, decisions that a lead missed, tasks that are stuck, open questions and budgets that ran out.

Memory, routines, approvals and permissions:

- `GET /api/bots/{id}/memory`: the facts that the bot sees outside teams, or for a team id its team facts and the user facts. Each has `id`, `scope`, `tier`, `fact`, `learnedBy` (`user` or a bot id) and `createdAt`.
- `POST /api/bots/{id}/memory` with `{"scope", "tier", "fact"}`: add a fact as the user. Returns 204. For a team, `scope` is `team` or `user`.
- `DELETE /api/memory/{id}`: delete a fact.
- `GET /api/bots/{id}/routines`: the bot's routines. Each has `id`, `botId`, `name`, `prompt`, `schedule`, `enabled`, `nextRunAt`, `lastRunAt`, `lastRunBy` and `createdAt`.
- `POST /api/bots/{id}/routines` with `{"name", "prompt", "schedule"}`: create a routine. Returns 201.
- `PATCH /api/routines/{id}` with `{"name"?, "prompt"?, "schedule"?, "enabled"?}`: change, pause or resume a routine.
- `POST /api/routines/{id}/run`: run a routine now. Returns 202 with `{"turnId"}`.
- `DELETE /api/routines/{id}`: delete a routine.
- `POST /api/approvals/{entry id}` with `{"decision": "once" | "always" | "deny"}`: answer an approval card. Returns 204, or 409 when the card is no longer pending. `always` on a card of `create_setup` returns 400.
- `GET /api/bots/{id}/grants`: the tools that the bot may use without asking.
- `DELETE /api/bots/{id}/grants/{tool}`: remove one of them.

Artifacts:

- `GET /api/artifacts?kind=&author=&tag=&q=&limit=`: the artifacts that match every filter that is set, newest updated first, without `content`. Each has `id`, `kind`, `title`, `authorId` (`user` or a bot id), `tags`, `links`, `createdAt` and `updatedAt`. `q` matches text in the title or content, ignoring case. `limit` is 1 to 50, default 20; a larger limit returns at most 50.
- `GET /api/artifacts/{id}`: one artifact, with its `content`.
- `POST /api/artifacts` with `{"kind", "title", "content", "tags"?, "links"?}`: create an artifact as the user. Returns 201 and the artifact.
- `PATCH /api/artifacts/{id}` with any of `{"kind", "title", "content", "tags", "links"}`: change an artifact, whoever wrote it. Fields that you leave out stay the same.
- `DELETE /api/artifacts/{id}`: delete an artifact, whoever wrote it. Returns 204.

Skills and connectors:

- `GET /api/skills`: the skills, with `id`, `name` and `description`.
- `GET /api/skills/{id}`: one skill, with its `body`.
- `DELETE /api/skills/{id}`: delete a skill.
- `GET /api/connectors`: the connectors from `connectors.json`.
- `POST /api/connectors/{name}/connect`: start a sign-in. Returns `{"url"}` to open in a browser. The sign-in returns to `/oauth/callback`.
- `DELETE /api/connectors/{name}/token`: sign out and delete the tokens.

Settings, session and phone access:

- `GET /api/version`: the server's version. `GET /api/defaults`: the default model and effort.
- `GET /api/settings` and `PATCH /api/settings`: the settings of `config.json`. `PUT /api/settings/file` replaces a broken file. `POST /api/restart` restarts the server when it is idle, and `DELETE /api/restart` cancels that.
- `POST /api/drain` and `DELETE /api/drain`: stop and resume the start of new turns, for an update.
- `GET /api/session`: whether the request carries Handful's key, and the key file's path when it does not. `POST /api/key` with `{"key"}` checks a pasted key and gives the browser the key's cookie.
- `GET /api/phone`, `POST /api/phone/on` and `POST /api/phone/off`: phone access, which is off by default.
- `POST /api/phone/pairing`, `PATCH /api/phone/pairing` and `POST /api/phone/pairing/{id}`: start a pairing, set its scope, and approve or deny it.
- `GET /api/phone/sessions`, `DELETE /api/phone/sessions` and `DELETE /api/phone/sessions/{id}`: list the paired phones, revoke all of them, or revoke one.

Event stream:

- `GET /api/events` is a server-sent event stream of every change.
- It starts with `event: hello`. If new turns wait for a usage limit, a `pause` event follows.
- Every other event is a `data:` line with a JSON object. Its `type` is one of:
  - `entry`: an entry was added or changed. It has `botId` and `entry`.
  - `state`: a bot started or stopped working. It has `botId` and `running`.
  - `tasks`: a bot's background tasks changed. It has `botId` and `tasks`.
  - `bots`: the list of bots and rooms changed. Fetch `GET /api/bots` again.
  - `pause`: `pausedUntil` is when turns start again. It is absent when the pause ended.
- A comment line, `: ping`, comes every 20 seconds.
- A client that falls behind is disconnected. After each connect, fetch the entries after the highest `rev` that you have.

Examples:

```sh
curl -s http://127.0.0.1:7777/api/bots

curl -s -X POST http://127.0.0.1:7777/api/bots/<bot id>/messages \
  -H "X-Handful-Key: $(cat ~/.handful/ui-key)" \
  -H 'Content-Type: application/json' \
  -d '{"text": "@here what is the status?", "clientNonce": "<unique id>"}'

curl -s 'http://127.0.0.1:7777/api/bots/<bot id>/entries?after=0'

curl -N http://127.0.0.1:7777/api/events
```
