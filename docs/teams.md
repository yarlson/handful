# Teams

A team is a group of bots that work together for you. It has a lead, a working agreement and a shared folder.

Use a team for work that needs several bots, for example a change that one bot writes and another checks. If you only want a conversation with several bots, use a [room](bots.md#rooms).

## Create a team

Click **+** next to **Teams**. Give the team a name and a purpose, choose its members and its lead, and write the working agreement.

The working agreement holds your rules for how the team works and when work is done, for example "Tests pass, and another member checks each change." Inside the team, it overrides each member's own instructions on how work is done and checked.

To change the lead or the agreement later, open the team panel, then **Settings**. When the lead leaves the team, the first member leads.

## Talk to a team

- A message that mentions no one goes to the lead. The lead answers it, or @mentions the member who should do the work.
- `@Name` wakes one member. `@here` wakes every member.

## What the members share

- **A folder:** `~/.handful/teams/<team id>/workspace`.
- **Team memory:** members save what the whole team needs there. To read it or add to it, open the team panel, then **Memory**. Members see team memory only in the team's turns.

A bot can be a member of several teams. It keeps a separate Claude Code session for each one.

In a team turn, a member talks only to its team. It does not message other bots, and it does not see the direct messages that other bots sent it. Those wait for its next turn outside the team.

There is one exception. A member can post to a room that it belongs to, as long as the post @mentions no one, so that no bot wakes. A lead's report is an example. Handful refuses a post that would wake someone, and names the bots it would wake.

## Tasks

For work with several steps, send the team a task. Type `/task` and what the team should do in the team's chat:

```
/task Add CSV export
```

`/goal` still works. **New task…** in **Across teams** does the same for any team. Its **Start now** box puts the task at the top of the team's plan.

### 1. The lead decides

Every task lands in the team's inbox first, whether it comes from you, from another team or from a member. The lead decides on each one. It can:

- take the task, at a place in the plan;
- ask the sender a question;
- defer it to a date;
- pass it on to another team;
- push it back, with a reason.

The lead has the team's **decision time** for each task: 30 minutes by default. The clock counts only the time when the lead can act. The inbox shows the clock as text, for example "decide by 14:30" or "clock paused: usage limit".

The task comes to you when the time runs out, or when the leads have asked about it or passed it on 3 times. Then you **Take** it at a place in the plan, or **Push back…** with a reason.

### 2. The members do the steps

The task that the team works on is the **Now** task. The lead splits it into steps. Each step has a brief, acceptance criteria and one owner. When the working agreement asks for a check, the step also has a reviewer, who is another member.

- A step starts when it is ready. A step that depends on others waits for them. Independent steps run at the same time.
- The owner submits the result. The reviewer passes it or asks for changes. Every third request for changes blocks the step, and the lead decides what to do.
- An owner or a reviewer that cannot go on blocks the step with a question. The lead answers, and the step goes back to its owner or reviewer, or to another member. The lead can also ask you in the chat.

While a bot reviews a step, Handful denies it Edit and Write in the team, even when you always allow them. Bash can still change files, so do not always allow Bash for a bot that reviews.

Two things can leave a step without anyone on it:

- **A member stops without handing its step on**, for example after a failed turn or a restart. The step is blocked, and the lead decides. If the task then has nothing in flight, the lead wakes.
- **A member leaves the team, or you delete it.** Its steps wait for a new owner. A review that it did not finish is blocked for the lead.

### 3. Whoever asked approves

When every step is done or dropped, the lead closes the task with a result, and the next task in the plan starts at once.

The done task waits for approval by whoever asked for it: you for your tasks, the sending team's lead for its tasks. **Approve** it, or **Ask for changes…** With changes, the task comes back as Now, first in the plan.

After you approve a task, the lead looks back on it once. It is asked to save up to 5 lessons in team memory. It can also suggest a change to the working agreement, but only you change the agreement.

### Stop or pause a task

- **Stop…** on the Now task, in the **Work** tab, stops the task. Running turns finish, its steps stay, and the next task in the plan starts. The stopped task moves to **Earlier**. **Reopen** there puts it back in the inbox.
- **Stop** in the team's chat pauses the task. The Now card then shows **Paused** and **Resume**, which goes on with it.

## Where you see the work

### The Work tab

The **Work** tab of the team panel shows, from top to bottom:

| Section | What it shows |
|---|---|
| **Now** | The task in work: its steps, how many are done, the turns it used, when it started, **History** and **Stop…**. |
| **Needs you** | What only you can settle in this team. It shows only when there is something. |
| **Next** | The planned tasks in the lead's order, and, when the inbox has tasks, a line that opens it. It shows only when there is something. Point at a planned task, or tab to it, to move it with the arrows, pin it so the lead can't move it, or **Withdraw…** it. |
| **Waiting on other teams** | The open tasks that this team sent. With more than one team, **Send a task…** is here. |
| **Earlier** | Stopped tasks, with **Reopen**, sent tasks that are over, and pushed-back tasks you dismissed. It is closed until you open it. |
| The bottom | A note on turns that were on no task, when there are any, then **Today**, with the day's turns. |

When the panel is closed, a **Now** chip in the chat header shows the Now task and how many things need you. Click it to open the Work tab.

### Across teams

**Across teams**, at the top of the sidebar, shows every team at once: its Now task, how many tasks are next and in its inbox, its turns on no task, today's turns, and whether it is paused.

Its **Needs you** list holds what only you settle:

- results to approve;
- decisions that a lead did not make in time;
- tasks that were asked about or passed on 3 times;
- budgets at 80% or used up, with **Raise…**, and **Wait until midnight** once used up;
- open questions.

## The budget

A team has one budget of turns per day.

- Every turn of its members counts, chat and @mentions included. The lead's decisions on the inbox do not count.
- The team that does a task pays for it.
- Set **Turns per day** in the team's **Settings**. 0 is no limit, and a new team starts with no limit.
- The Work tab shows the day's count under **Today**. After a week it suggests a limit, and **Use** sets it.
- At 80%, **Needs you** warns you. At the limit, the team pauses until midnight or until you raise the limit. Running turns finish, no new turn starts, and the lead's decisions still run.
- Each task shows its turns so far.

**Turns on no task.** The Work tab notes the team's turns of the last hour that were on no task: who, when and the first line. **Make a task…** turns them into a task that the team takes at once. **Dismiss** puts the note away. The lead sees the same count and can make the task too.

With `-max-parallel` above 1, a team's task turns leave one slot free, so your other bots still answer while a team works.

## Teams that work together

Teams pass work to each other as tasks. A task can be a whole piece of work, for example a bug for a Dev team or an approved spec to build. It can also be a part that a step of the sending team's Now task waits on, for example a guide that a Release team needs from a Docs team.

The receiving team works on the task from its own budget. The sending team needs no task of its own.

- **Send.** A lead sends a task with a brief and any artifacts. You can too: **Send a task…** in a team's **Work** tab, or **Send to a team…** on an artifact in the **Artifacts** tab.
- **Receive.** The task lands in the receiving team's inbox, and the sidebar shows a blue count on the team. Its lead decides on it, as on any task.
- **Follow.** The sending team's **Work** tab lists the tasks it sent in the last 30 days. The open ones are under **Waiting on other teams**, each with its state: in the other team's inbox, later, its place in the other team's plan, or being done, with its turns ("paid by" the receiving team). A task the receiving lead asked about, finished, or pushed back moves to **Needs you**. The ones that are over (done, stopped or withdrawn) are under **Earlier**.
- **Act.** In **Needs you**: **Answer…** replies to the receiver's question, **Approve** or **Ask for changes…** settles a task done at the other team, and **Dismiss** puts away a task that was pushed back, after you read the reason; it then moves to **Earlier**. Under **Waiting on other teams**, **Withdraw…** takes back a task that hasn't started.

When a step of the sender's task waits on the sent task, the receiving lead sees "<team>'s task waits on this". If the task is pushed back, that step is blocked with the reason, and the sending lead plans again.

Limits:

- A team's inbox holds at most 10 tasks, and at most 3 from one team. Change both, and the decision time, in the team's **Settings**.
- Your own tasks always land.
- Work that came through 3 teams needs you to send it on.
