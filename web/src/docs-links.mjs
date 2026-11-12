import path from "node:path";

// The pages of /docs/ by their file in the repo, and the pages that exist in the repo but that
// the site never shows. A link to a published page goes to the site, a link to any other file goes
// to GitHub, and a paragraph that links a hidden page is left out of the site, so the repo's docs
// keep the link and the site stays free of the old name.
export const published = new Map([
	["README.md", "/docs/first-steps/"],
	["docs/bots.md", "/docs/bots/"],
	["docs/teams.md", "/docs/teams/"],
	["docs/mac-app.md", "/docs/mac-app/"],
	["docs/configuration.md", "/docs/configuration/"],
	["docs/connectors.md", "/docs/connectors/"],
	["docs/development.md", "/docs/development/"],
]);
export const hidden = new Set(["docs/moving-from-yargpt.md"]);

const githubBlob = "https://github.com/yarlson/handful/blob/main/";
export const isExternal = (href) => /^([a-z][a-z0-9+.-]*:|#|\/)/i.test(href);

// The file a relative link in `from` points to, from the repo root, without the anchor.
export const targetFile = (from, href) => path.normalize(path.join(path.dirname(from), href.split("#")[0]));

// Where a relative link in `from` goes on the site; `base` is the site's origin, or "" for a path.
export function docsHref(from, href, base = "") {
	const [, anchor] = href.split("#");
	const file = targetFile(from, href);
	const site = published.get(file);
	return (site ? base + site : githubBlob + file) + (anchor ? `#${anchor}` : "");
}
