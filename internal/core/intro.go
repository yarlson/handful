package core

import (
	"regexp"
	"strings"
	"unicode"
)

// lowercaseWord matches text that starts with a plain lowercase word, which
// is safe to capitalize; an identifier such as "atlas#656" is left as it is.
var lowercaseWord = regexp.MustCompile(`^[a-z]+\s`)

// withoutSelfIntro removes an opening in which the sender names itself:
// "<Name> here.", "<Name>:" or "This is <Name>.". The app shows the sender
// with every message, so the opening only repeats it. Only the sender's own
// name at the very start counts, and a message with nothing else is kept.
func withoutSelfIntro(text, name string) string {
	n := regexp.QuoteMeta(strings.TrimSpace(name))
	if n == "" {
		return text
	}
	intro := regexp.MustCompile(`(?i)^\s*(?:\*\*)?(?:this is\s+` + n + `\s*[.,!]|` + n + `\s+here\s*[.,:!\-–—]|` + n + `\s*:)(?:\*\*)?\s+`)
	loc := intro.FindStringIndex(text)
	if loc == nil {
		return text
	}
	rest := strings.TrimSpace(text[loc[1]:])
	if rest == "" {
		return text
	}
	if lowercaseWord.MatchString(rest) {
		r := []rune(rest)
		r[0] = unicode.ToUpper(r[0])
		rest = string(r)
	}
	return rest
}
