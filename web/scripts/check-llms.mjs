// Checks the built llms.txt against the code in the repo: every HTTP route, bot tool and board
// action that it names exists, the old workflow is gone, and the key rule is there. The routes
// and tools come from internal/httpapi and internal/agent, one level above web/.
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const root = new URL("../../", import.meta.url).pathname;
const llms = readFileSync(new URL("../dist/llms.txt", import.meta.url), "utf8");
const errors = [];
const read = (path) => readFileSync(join(root, path), "utf8");
const goFiles = (dir) => readdirSync(join(root, dir)).filter((f) => f.endsWith(".go") && !f.endsWith("_test.go")).map((f) => read(join(dir, f)));
const normal = (route) => route.replace(/\{[^}]*\}/g, "{}");

// Routes: every `METHOD /path` in the file is registered in internal/httpapi.
const http = goFiles("internal/httpapi").join("\n");
const keyPath = http.match(/keyPath\s*=\s*"([^"]+)"/)?.[1];
const registered = new Set([...http.matchAll(/"(GET|POST|PUT|PATCH|DELETE) (\/[^"]*)"/g)].map((m) => normal(`${m[1]} ${m[2]}`)));
if (keyPath) registered.add(`POST ${keyPath}`);
// A parameter may have a space inside its braces, such as {entry id}.
const named = [...llms.matchAll(/`(GET|POST|PUT|PATCH|DELETE) (\/(?:[^`\s{?]|\{[^}]*\})*)(?:\?[^`\s]*)?`/g)].map((m) => normal(`${m[1]} ${m[2]}`));
// A route with a sub-path after a slash, such as "`/reopen`", is written as the full route elsewhere.
for (const route of new Set(named)) if (!registered.has(route)) errors.push(`route not in the code: ${route}`);

// Task routes written as a short form after the first one, such as `/stop`, `/reopen`: check them too.
const short = llms.match(/`POST \/api\/tasks\/\{id\}\/stop`, (.+?):/)?.[1] ?? "";
for (const [, tail] of short.matchAll(/`\/([a-z-]+)`/g)) if (!registered.has(`POST /api/tasks/{}/${tail}`)) errors.push(`task route not in the code: /api/tasks/{id}/${tail}`);

// Tools and board actions.
const agent = goFiles("internal/agent").join("\n");
const tools = new Set([...agent.matchAll(/Name:\s*"([a-z_]+)"/g)].map((m) => m[1]));
// Builder's tool is registered under a constant of internal/core.
const setupTool = goFiles("internal/core").join("\n").match(/ToolCreateSetup\s*=\s*"(?:mcp__handful__)?([a-z_]+)"/)?.[1];
if (setupTool) tools.add(setupTool);
const toolSection = llms.slice(llms.indexOf("## Bot tools"), llms.indexOf("## Local HTTP API"));
for (const [, tool] of toolSection.matchAll(/^- `([a-z_]+)`:/gm)) if (!tools.has(tool)) errors.push(`tool not in the code: ${tool}`);
for (const tool of tools) if (tool !== "approve" && !new RegExp(`^- \`${tool}\``, "m").test(toolSection)) errors.push(`tool missing in llms.txt: ${tool}`);
const boardBlock = agent.match(/var boardActions = map\[string\][^{]*\{([\s\S]*?)\n\}/)?.[1] ?? "";
const actions = new Set([...boardBlock.matchAll(/^\s*"([a-z_]+)":/gm)].map((m) => m[1]));
const retired = new Set([...(agent.match(/var retiredActions = map\[string\]string\{([\s\S]*?)\n\}/)?.[1] ?? "").matchAll(/^\s*"([a-z_]+)":/gm)].map((m) => m[1]));
if (!actions.size) errors.push("could not read boardActions from internal/agent");
const actionLine = toolSection.match(/`action`, for tasks:([^\n]*)/)?.[1] ?? "";
const listed = new Set([...actionLine.matchAll(/`([a-z_]+)`/g)].map((m) => m[1]));
for (const action of listed) if (action !== "list" && !actions.has(action)) errors.push(`board action not in the code: ${action}`);
for (const action of actions) if (!listed.has(action)) errors.push(`board action missing in llms.txt: ${action}`);
for (const action of retired) if (new RegExp(`\`${action}\``).test(toolSection)) errors.push(`retired board action named: ${action}`);

// The old workflow is gone. `/goal` stays only in the docs' own sentence that it still works as a
// shortcut for /task (docs/teams.md).
for (const [name, pattern] of [["/goal", /\/goal/g], ["deliver", /\bdeliver\b/g], ["start_goal", /start_goal/g], ["30 turns", /30 turns/g]]) {
	for (const m of llms.matchAll(pattern)) {
		const line = llms.slice(llms.lastIndexOf("\n", m.index) + 1, llms.indexOf("\n", m.index));
		if (!(name === "/goal" && line.startsWith("`/goal` still works."))) errors.push(`old workflow in llms.txt: ${name}`);
	}
}

// What must be there.
for (const need of ["X-Handful-Key", "ui-key", "401", "/task", "inbox", "turns per day", "Needs you", "Phone access"]) {
	if (!llms.includes(need)) errors.push(`llms.txt lacks: ${need}`);
}
const about = readFileSync(new URL("../src/about.ts", import.meta.url), "utf8").match(/aboutText =\s*"([^"]+)"/)?.[1];
if (!about || !llms.includes(`> ${about}`)) errors.push("llms.txt does not start with the footer paragraph");
for (const [, href] of llms.matchAll(/\]\((\.{0,2}\/?[^)\s:]+\.md[^)\s]*|#[^)\s]*)\)/g)) errors.push(`relative link left in llms.txt: ${href}`);

if (errors.length) {
	console.error(errors.join("\n"));
	process.exit(1);
}
console.log(`check-llms: ${new Set(named).size} routes, ${listed.size - 1} board actions and every tool match the code; the old workflow is gone`);
