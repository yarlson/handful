// The pages of /docs/, built from the repo's own Markdown. A file that is not listed here
// is not published, and a link to it goes to GitHub.
export const pages = [
	{ slug: "first-steps", file: "README.md", title: "First steps", description: "Install Handful, start Builder, and see what a bot keeps, how rooms and teams work, and what to know about security." },
	{ slug: "bots", file: "docs/bots.md", title: "Bots and rooms", description: "Builder, chats, approvals, memory, routines, skills, artifacts and rooms." },
	{ slug: "teams", file: "docs/teams.md", title: "Teams", description: "How a team takes a task: the lead decides, members do the steps, another member checks, and you approve." },
	{ slug: "mac-app", file: "docs/mac-app.md", title: "macOS app", description: "The Mac app runs the server from login, keeps itself up to date, and can go back to an older version." },
	{ slug: "configuration", file: "docs/configuration.md", title: "Configuration", description: "Flags, config.json, the data folder, model and effort, Handful's key, global instructions and phone access." },
	{ slug: "connectors", file: "docs/connectors.md", title: "Connectors", description: "Give bots the tools of an external MCP server, such as Slack or a wiki." },
	{ slug: "development", file: "docs/development.md", title: "Development", description: "How to build, check and release Handful, and how it works inside." },
] as const;

export type DocPage = (typeof pages)[number];

const modules = import.meta.glob<{
	Content: any;
	rawContent: () => string;
	getHeadings: () => { depth: number; slug: string; text: string }[];
}>(["../../README.md", "../../docs/*.md"], { eager: true });

export function docModule(page: DocPage) {
	const mod = modules[`../../${page.file}`];
	if (!mod) throw new Error(`docs page ${page.slug}: ${page.file} not found`);
	return mod;
}

export { updated } from "./docs-git.mjs";

export function formatDate(iso: string) {
	return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" });
}
