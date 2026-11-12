#!/bin/sh
# Mutation-tests, with Stryker, the lines of ui/ that changed since $BASE and
# that the pathspecs given as arguments select. Fails when a mutant on a
# changed line survives the tests or no test runs it.
set -eu

ranges=$(git diff --merge-base "$BASE" --unified=0 --no-color -- "$@" | awk '
	/^\+\+\+ / { file = ""; if (substr($0, 5, 5) == "b/ui/") file = substr($0, 10); next }
	/^@@/ && file != "" {
		split(substr($3, 2), added, ",")
		count = (added[2] == "") ? 1 : added[2]
		if (count > 0) printf "%s%s:%d-%d", sep, file, added[1], added[1] + count - 1
		sep = ","
	}')
if [ -z "$ranges" ]; then
	echo "no changed lines to mutation-test in ui/ since $BASE"
	exit 0
fi

cd ui
rm -f reports/mutation/mutation.json
npx stryker run --mutate "$ranges" --reporters clear-text,progress,json
jq -e '[.files[].mutants[].status] | all(. != "Survived" and . != "NoCoverage")' reports/mutation/mutation.json >/dev/null || {
	echo "a ui/ line changed since $BASE has a mutant that survived or that no test runs: add a test that kills it" >&2
	exit 1
}
