// An image ../assets/<name>.png in web/src/docs shows four files: <name>-light, <name>-dark,
// <name>-m-light and <name>-m-dark. docs.css shows the one that fits the colour mode and the width.
// It runs as a remark plugin, before Astro turns image paths into imports.
function walk(node, visit) {
	for (const child of [...(node.children ?? [])]) {
		visit(child, node);
		if (child.type !== "image") walk(child, visit);
	}
}

const variants = [
	["light", "d", "-light"],
	["dark", "d", "-dark"],
	["light", "m", "-m-light"],
	["dark", "m", "-m-dark"],
];

export default function remarkDocsShots() {
	return (tree) => {
		walk(tree, (node, parent) => {
			const shot = node.type === "image" && /^(.*\/assets\/[a-z-]+)\.png$/.exec(node.url);
			if (!shot) return;
			const copies = variants.map(([mode, size, suffix]) => ({
				...node,
				url: `${shot[1]}${suffix}.png`,
				data: { hProperties: { "data-mode": mode, "data-size": size } },
			}));
			parent.children.splice(parent.children.indexOf(node), 1, ...copies);
		});
	};
}
