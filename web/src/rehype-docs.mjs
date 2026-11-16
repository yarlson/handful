// Makes the Markdown of web/src/docs fit the site: tables scroll on their own, and an apostrophe
// in an image's alt text is written once, because Astro escapes it twice.
function walk(node, visit, parent) {
	visit(node, parent);
	for (const child of [...(node.children ?? [])]) walk(child, visit, node);
}

export default function rehypeDocs() {
	return (tree) => {
		walk(tree, (node, parent) => {
			if (node.type !== "element") return;
			if (node.tagName === "img" && typeof node.properties?.alt === "string") {
				node.properties.alt = node.properties.alt.replaceAll("'", "’");
			}
			if (node.tagName === "table" && parent) {
				const index = parent.children.indexOf(node);
				parent.children[index] = { type: "element", tagName: "div", properties: { className: ["table-scroll"] }, children: [node] };
			}
		});
	};
}
