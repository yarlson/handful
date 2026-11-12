import type { APIRoute } from "astro";
import { aboutText } from "../about";
import { docModule, pages } from "../docs";
import { docsHref, hidden, isExternal, targetFile } from "../docs-links.mjs";
import reference from "../llms/reference.md?raw";

// llms.txt is made at build from the same sources as the site: the summary of the footer, the
// docs pages (the repo's README.md and docs/*.md, with their links made absolute), and
// src/llms/reference.md for what the docs leave out, the bot tools and the local API.
// scripts/check-llms.mjs checks the reference against the code.

const linkPattern = /\]\(([^)\s]+)\)/g;

// One docs page as text: images out, a paragraph that links a hidden page out, the title
// dropped, the other headings one level lower, and relative links made absolute.
function pageText(page: (typeof pages)[number], origin: string) {
	const body = docModule(page).rawContent().split("\n");
	const text = body
		.join("\n")
		.split(/\n\s*\n/)
		.filter((block) => {
			if (/^!\[[^\]]*\]\([^)]*\)\s*$/.test(block.trim())) return false;
			return ![...block.matchAll(linkPattern)].some(([, href]) => !isExternal(href) && hidden.has(targetFile(page.file, href)));
		})
		.join("\n\n");
	let inFence = false;
	return text
		.split("\n")
		.flatMap((line) => {
			if (line.startsWith("```")) inFence = !inFence;
			if (inFence || line.startsWith("```")) return [line];
			if (/^# /.test(line)) return [];
			if (/^#{2,5} /.test(line)) return [`#${line}`];
			return [
				line.replace(linkPattern, (whole, href: string) => {
					// An anchor in the page itself needs its page in the one file.
					if (href.startsWith("#")) return `](${origin}/docs/${page.slug}/${href})`;
					return isExternal(href) ? whole : `](${docsHref(page.file, href, origin)})`;
				}),
			];
		})
		.join("\n")
		.trim();
}

export const GET: APIRoute = ({ site }) => {
	const origin = site?.origin ?? "https://handful.dev";
	const index = pages.map((page) => `- [${page.title}](${origin}/docs/${page.slug}/): ${page.description}`).join("\n");
	const docs = pages.map((page) => `## ${page.title}\n\n${pageText(page, origin)}`).join("\n\n");
	const body = `# Handful\n\n> ${aboutText}\n\n## Docs\n\nThe same pages are on the web at ${origin}/docs/. This file has their text.\n\n${index}\n\n${docs}\n\n${reference.trim()}\n`;
	return new Response(body, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
};
