// The pages of /docs/, built from web/src/docs/. A file that is not listed here is not published.
export const pages = [
	{ slug: "first-steps", title: "First steps", description: "Install Handful, start Builder, make a first bot or team, and learn what an approval card asks." },
	{ slug: "software-team", title: "Run a software team", description: "Get one reviewed change from a team of three bots, from Builder's Software team start to your approval." },
	{ slug: "bots", title: "Bots and rooms", description: "Chats, approval cards, memory, routines, skills, artifacts and rooms." },
	{ slug: "teams", title: "Teams", description: "How a team takes a task from its inbox to your approval, where you see its work, and how to limit what it spends." },
	{ slug: "window", title: "The window", description: "Find any chat, message or command fast, and set up the window the way you like it." },
	{ slug: "phone", title: "Phone access", description: "Read your chats, message your bots and answer approval cards from your phone, and what that opens up." },
	{ slug: "connectors", title: "Connectors", description: "Give bots the tools of an external MCP server, such as Slack or a wiki." },
	{ slug: "configuration", title: "Configuration", description: "Flags, config.json, the data folder, model and effort, Handful's key and global instructions." },
	{ slug: "mac-app", title: "The Mac app", description: "What the Mac app does in the background, how it updates itself, and how to go back to an older version." },
] as const;

export type DocPage = (typeof pages)[number];

const modules = import.meta.glob<{
	Content: any;
	rawContent: () => string;
	getHeadings: () => { depth: number; slug: string; text: string }[];
}>("./docs/*.md", { eager: true });

export function docModule(page: DocPage) {
	const mod = modules[`./docs/${page.slug}.md`];
	if (!mod) throw new Error(`docs page ${page.slug}: web/src/docs/${page.slug}.md not found`);
	return mod;
}

export { updated } from "./docs-git.mjs";

export function formatDate(iso: string) {
	return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" });
}
