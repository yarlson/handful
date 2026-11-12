// Makes the repo's Markdown fit the site: links between the docs go to the site's pages, links
// to files that are not published go to GitHub, README's title becomes the page's, and tables
// scroll on their own. It reads no file; the pages come from the Markdown at build time.
import path from "node:path";

const repo = "https://github.com/yarlson/handful/blob/main/";
const published = new Map([
	["README.md", "/docs/first-steps/"],
	["docs/bots.md", "/docs/bots/"],
	["docs/teams.md", "/docs/teams/"],
	["docs/mac-app.md", "/docs/mac-app/"],
	["docs/configuration.md", "/docs/configuration/"],
	["docs/connectors.md", "/docs/connectors/"],
	["docs/development.md", "/docs/development/"],
]);

// Pages that exist in the repo but that the site never shows. A paragraph that links to one is
// left out of the site, so the repo's docs keep the link and the site stays free of the old name.
const hidden = new Set(["docs/moving-from-yargpt.md"]);

function walk(node, visit, parent) {
	visit(node, parent);
	for (const child of [...(node.children ?? [])]) walk(child, visit, node);
}

export default function rehypeDocs() {
	return (tree, file) => {
		const root = new URL("../../", import.meta.url).pathname;
		const from = path.relative(root, file.path);
		const linksHidden = (node) => {
			let found = false;
			walk(node, (n) => {
				if (n.type === "element" && n.tagName === "a" && typeof n.properties?.href === "string" && !/^([a-z][a-z0-9+.-]*:|#|\/)/i.test(n.properties.href)) {
					found ||= hidden.has(path.normalize(path.join(path.dirname(from), n.properties.href.split("#")[0])));
				}
			});
			return found;
		};
		walk(tree, (node) => {
			node.children = node.children?.filter((child) => !(child.type === "element" && child.tagName === "p" && linksHidden(child)));
		});
		walk(tree, (node, parent) => {
			if (node.type !== "element") return;
			if (node.tagName === "a" && typeof node.properties?.href === "string") {
				const href = node.properties.href;
				if (/^([a-z][a-z0-9+.-]*:|#|\/)/i.test(href)) return;
				const [target, anchor] = href.split("#");
				const rel = path.normalize(path.join(path.dirname(from), target));
				const site = published.get(rel);
				node.properties.href = site ? site + (anchor ? `#${anchor}` : "") : repo + rel + (anchor ? `#${anchor}` : "");
			}
			// Astro writes an image's alt text into HTML and escapes an apostrophe twice.
			if (node.tagName === "img" && typeof node.properties?.alt === "string") {
				node.properties.alt = node.properties.alt.replaceAll("'", "\u2019");
			}
			if (node.tagName === "table" && parent) {
				const index = parent.children.indexOf(node);
				parent.children[index] = { type: "element", tagName: "div", properties: { className: ["table-scroll"] }, children: [node] };
			}
		});
		if (from === "README.md") {
			const h1 = tree.children.find((n) => n.type === "element" && n.tagName === "h1");
			if (h1) h1.children = [{ type: "text", value: "First steps" }];
		}
	};
}
