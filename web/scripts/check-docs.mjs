// Checks that every bold label in the docs is text that the UI shows: the label, without a
// trailing "…", is a string in a non-test file of ui/src, desktop or cmd/handful. Only labels are bold in the
// docs. A label that the UI builds at run time is quoted in the docs, not bold. The UI source is
// one level above web/.
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

const root = new URL("../../", import.meta.url).pathname;
const docsDir = new URL("../src/docs/", import.meta.url).pathname;

function sourceFiles(dir) {
	const found = [];
	for (const name of readdirSync(dir)) {
		if (name === "node_modules" || name === "dist" || name === "testdata") continue;
		const path = join(dir, name);
		if (statSync(path).isDirectory()) found.push(...sourceFiles(path));
		else if (/\.(tsx?|jsx?|go|html)$/.test(name) && !/\.(test|spec)\./.test(name) && !name.endsWith("_test.go")) found.push(path);
	}
	return found;
}

const text = ["ui/src", "desktop", "cmd/handful"].flatMap((dir) => sourceFiles(join(root, dir))).map((path) => readFileSync(path, "utf8")).join("\n");
// The UI writes the ellipsis as "…" or as "&hellip;", and some quotes as entities.
const shown = text.replace(/&hellip;/g, "…").replace(/&amp;/g, "&").replace(/\s+/g, " ");

const errors = [];
for (const file of readdirSync(docsDir).filter((f) => f.endsWith(".md"))) {
	const lines = readFileSync(join(docsDir, file), "utf8").split("\n");
	let fenced = false;
	lines.forEach((line, i) => {
		if (line.startsWith("```")) fenced = !fenced;
		if (fenced) return;
		for (const [, raw] of line.matchAll(/\*\*([^*]+)\*\*/g)) {
			const label = raw.replace(/…$/, "").trim();
			if (!shown.includes(label)) errors.push(`${file}:${i + 1}: bold label not in the UI code: ${raw}`);
		}
	});
}

if (errors.length) {
	console.error(errors.join("\n"));
	process.exit(1);
}
console.log("check-docs: every bold label in web/src/docs is in the UI code");
