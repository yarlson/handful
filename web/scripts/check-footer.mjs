// Checks the built footer: the JSON-LD description is the visible paragraph word for word,
// no footer text is hidden from people, and every internal footer link and anchor resolves.
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const dist = new URL("../dist/", import.meta.url).pathname;
const errors = [];

function pages(dir) {
	return readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? pages(join(dir, e.name)) : e.name.endsWith(".html") ? [join(dir, e.name)] : []));
}
const decode = (s) => s.replace(/&#(\d+);/g, (_, n) => String.fromCharCode(n)).replace(/&#x([0-9a-f]+);/gi, (_, n) => String.fromCharCode(parseInt(n, 16))).replace(/&quot;/g, '"').replace(/&amp;/g, "&").replace(/&lt;/g, "<").replace(/&gt;/g, ">");
const text = (html) => decode(html.replace(/<[^>]+>/g, "")).replace(/\s+/g, " ").trim();
const ids = (html) => new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]));

const all = pages(dist);
const byFile = new Map(all.map((f) => [f, readFileSync(f, "utf8")]));
let footers = 0;
for (const [file, html] of byFile) {
	const footer = html.match(/<footer[\s\S]*?<\/footer>/)?.[0];
	if (!footer) continue;
	footers++;
	const where = file.slice(dist.length);
	if (/aria-hidden|display:\s*none|\bhidden\b|sr-only|<details|<summary|visibility:\s*hidden|font-size:\s*0|opacity:\s*0/i.test(footer)) errors.push(`${where}: the footer has hidden or collapsible text`);
	for (const [, href] of footer.matchAll(/<a [^>]*href="([^"]+)"/g)) {
		if (/^https?:/.test(href)) continue;
		const [path, anchor] = href.split("#");
		const target = path ? [join(dist, path), join(dist, path, "index.html")].find((p) => existsSync(p) && !p.endsWith("/") && (p.endsWith(".html") || p.endsWith(".txt"))) : file;
		if (!target) errors.push(`${where}: footer link ${href} does not resolve`);
		else if (anchor && target.endsWith(".html") && !ids(byFile.get(target) ?? "").has(anchor)) errors.push(`${where}: footer link ${href} has no such anchor`);
	}
}

const home = byFile.get(join(dist, "index.html")) ?? "";
const paragraph = home.match(/<p id="about-text"[^>]*>([\s\S]*?)<\/p>/)?.[1];
const ld = home.match(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/)?.[1];
if (!paragraph) errors.push("index.html: the footer paragraph (#about-text) is missing");
if (!ld) errors.push("index.html: the JSON-LD is missing");
if (paragraph && ld) {
	const app = JSON.parse(ld)["@graph"].find((n) => n["@type"] === "SoftwareApplication");
	if (app?.description !== text(paragraph)) errors.push("index.html: the JSON-LD description is not the visible paragraph");
}

if (errors.length) {
	console.error(errors.join("\n"));
	process.exit(1);
}
console.log(`check-footer: ${footers} footers, JSON-LD description equals the visible paragraph, no hidden text, every footer link resolves`);
