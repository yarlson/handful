import { execFileSync } from "node:child_process";

// The build runs in web/, which is inside the repo; this file is bundled, so its own location says nothing.
function git(...args) {
	return execFileSync("git", args, { cwd: process.cwd(), encoding: "utf8", timeout: 120_000, stdio: ["ignore", "pipe", "pipe"] }).trim();
}

let history;

// The date of the last commit that changed the file, or null when git can't say. In a shallow
// clone every file would get the date of the newest commit, so the history is fetched first,
// and when that fails no date is shown: a wrong date is worse than none.
export function updated(page) {
	try {
		if (!history) {
			if (git("rev-parse", "--is-shallow-repository") === "true") {
				try {
					git("fetch", "--unshallow", "--quiet");
				} catch {
					// still shallow
				}
			}
			history = git("rev-parse", "--is-shallow-repository") === "true" ? "shallow" : "full";
		}
		if (history === "shallow") return null;
		const top = git("rev-parse", "--show-toplevel");
		return git("-C", top, "log", "-1", "--format=%cI", "--", page.file) || null;
	} catch {
		return null;
	}
}
