// Fails the build when the old product name reaches the built site, whatever file it comes
// from: the pages, the docs read from the repo's Markdown, llms.txt or the sitemap.
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const name = ["yarg", "pt"].join("");
const dist = new URL("../dist/", import.meta.url).pathname;
const found = [];

function scan(dir) {
	for (const entry of readdirSync(dir, { withFileTypes: true })) {
		const path = join(dir, entry.name);
		if (entry.isDirectory()) scan(path);
		else if (/\.(html|txt|xml|json|css|js|svg|webmanifest)$/i.test(entry.name)) {
			readFileSync(path, "utf8").split("\n").forEach((line, i) => {
				if (line.toLowerCase().includes(name)) found.push(`${path.slice(dist.length)}:${i + 1}`);
			});
		}
	}
}

scan(dist);
if (found.length) {
	console.error(`The old product name is in the built site:\n${found.join("\n")}\nChange it where it comes from: a page, README.md or a file in docs/.`);
	process.exit(1);
}
console.log("check-dist: no old product name in the built site");
