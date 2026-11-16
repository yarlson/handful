import { macDownloadUrl } from "./release";

// What the footer says Handful is, and the page's JSON-LD repeats it word for word.
export const aboutText =
	"Handful is a free, open-source app that runs a small team of AI agents, called bots, on your own Mac or Linux computer. You give a team a task: a lead splits it into steps, the members do them, a second member can review each step, and you approve the result. Each bot keeps its own chat, memory, routines and files. Every turn runs Claude Code on your machine, on your own subscription, with no Handful account and no Handful cloud.";

export const needs = "Claude Code 2.1.319 or later, signed in";

export const facts = [
	{ label: "Price", value: "Free. MIT License." },
	{ label: "Runs on", value: "macOS (Apple silicon and Intel) and Linux." },
	{ label: "Needs", value: `${needs}.` },
	{
		label: "Is not",
		value: "A coding agent, a hosted service or a shared server. Handful runs Claude Code, which talks to Anthropic with your account, so it does not work offline. One person uses it on one computer.",
	},
];

const github = "https://github.com/yarlson/handful";

export const linkGroups = [
	{
		title: "Product",
		links: [
			{ label: "Download for macOS", href: macDownloadUrl },
			{ label: "Build from source", href: "/docs/first-steps/#from-source" },
			{ label: "Releases", href: `${github}/releases` },
			{ label: "Security", href: "/security/" },
			{ label: "How Handful builds Handful", href: "/built-with-handful/" },
		],
	},
	{
		title: "Docs",
		links: [
			{ label: "First steps", href: "/docs/first-steps/" },
			{ label: "Bots", href: "/docs/bots/" },
			{ label: "Teams", href: "/docs/teams/" },
			{ label: "Mac app", href: "/docs/mac-app/" },
			{ label: "Configuration", href: "/docs/configuration/" },
			{ label: "Connectors", href: "/docs/connectors/" },
		],
	},
	{
		title: "Project",
		links: [
			{ label: "Source on GitHub", href: github },
			{ label: "Issues", href: `${github}/issues` },
			{ label: "MIT License", href: `${github}/blob/main/LICENSE` },
			{ label: "Development", href: `${github}/blob/main/docs/development.md` },
			{ label: "llms.txt", href: "/llms.txt" },
		],
	},
];
