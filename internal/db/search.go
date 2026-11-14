package db

import (
	"bytes"
	"database/sql/driver"
	"sync"
	"unicode"
	"unicode/utf8"

	"modernc.org/sqlite"
)

// contains_fold(text, query) is 1 when text contains query with no case, by
// unicode.ToLower on each, and 0 otherwise or when text is NULL. SQLite's
// LIKE ignores the case of ASCII letters only.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("contains_fold", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		text, _ := args[0].(string)
		query, _ := args[1].(string)
		if containsFold(text, query) {
			return int64(1), nil
		}
		return int64(0), nil
	})
}

// lowered holds the buffers that containsFold lowers into, so that a search
// through many entries does not allocate for each one.
var lowered = sync.Pool{New: func() any { return new([]byte) }}

func containsFold(text, query string) bool {
	t, q := lowered.Get().(*[]byte), lowered.Get().(*[]byte)
	defer lowered.Put(t)
	defer lowered.Put(q)
	*t, *q = appendLower((*t)[:0], text), appendLower((*q)[:0], query)
	return bytes.Contains(*t, *q)
}

func appendLower(dst []byte, s string) []byte {
	for i := 0; i < len(s); {
		if c := s[i]; c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			dst = append(dst, c)
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		dst = utf8.AppendRune(dst, unicode.ToLower(r))
		i += size
	}
	return dst
}
