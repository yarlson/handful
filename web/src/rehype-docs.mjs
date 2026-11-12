// Makes the repo's Markdown fit the site: links between the docs go to the site's pages, links
// to files that are not published go to GitHub, a paragraph that links a hidden page is left out,
// README's title becomes the page's, and tables scroll on their own. It reads no file; the
// pages come from the Markdown at build time.
import path from "node:path";
import { docsHref, hidden, isExternal, targetFile } from "./docs-links.mjs";

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
				if (n.type === "element" && n.tagName === "a" && typeof n.properties?.href === "string" && !isExternal(n.properties.href)) {
					found ||= hidden.has(targetFile(from, n.properties.href));
				}
			});
			return found;
		};
		walk(tree, (node) => {
			node.children = node.children?.filter((child) => !(child.type === "element" && child.tagName === "p" && linksHidden(child)));
		});
		walk(tree, (node, parent) => {
			if (node.type !== "element") return;
			if (node.tagName === "a" && typeof node.properties?.href === "string" && !isExternal(node.properties.href)) {
				node.properties.href = docsHref(from, node.properties.href);
			}
			// Astro writes an image's alt text into HTML and escapes an apostrophe twice.
			if (node.tagName === "img" && typeof node.properties?.alt === "string") {
				node.properties.alt = node.properties.alt.replaceAll("'", "’");
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
