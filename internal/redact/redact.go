// Package redact masks literal values in SQL query texts so that sensitive
// data (emails, ids, payloads) never leaves the tool — applied at ingestion,
// before a snapshot reaches the ring buffer, persistent history, the live
// stream, or the audit log (spec §7).
//
// The masker is a small lexer, not a SQL parser: it replaces string literals,
// dollar-quoted strings, numeric literals, and comment bodies with `?`,
// preserving keywords, identifiers, and structure so the query stays readable:
//
//	UPDATE accounts SET email = 'bob@x.io' WHERE id = 42
//	→ UPDATE accounts SET email = ? WHERE id = ?
package redact

import "strings"

// Mask returns q with literal values replaced by `?`.
func Mask(q string) string {
	var b strings.Builder
	b.Grow(len(q))
	r := []rune(q)
	n := len(r)

	isWord := func(c rune) bool {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
	}

	for i := 0; i < n; {
		c := r[i]
		switch {
		// Line comment: keep the marker, mask the body.
		case c == '-' && i+1 < n && r[i+1] == '-':
			b.WriteString("--?")
			for i < n && r[i] != '\n' {
				i++
			}

		// Block comment: keep the markers, mask the body.
		case c == '/' && i+1 < n && r[i+1] == '*':
			b.WriteString("/*?*/")
			i += 2
			for i+1 < n && !(r[i] == '*' && r[i+1] == '/') {
				i++
			}
			i += 2
			if i > n {
				i = n
			}

		// Standard string literal '...' with '' escapes. An immediately
		// preceding E/B/X prefix has already been emitted as part of a word —
		// acceptable: the value itself is still masked.
		case c == '\'':
			b.WriteRune('?')
			i++
			for i < n {
				if r[i] == '\'' {
					if i+1 < n && r[i+1] == '\'' { // escaped quote
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}

		// Dollar-quoted string: $$...$$ or $tag$...$tag$.
		case c == '$':
			// Find the closing $ of the opening tag.
			j := i + 1
			for j < n && r[j] != '$' && isWord(r[j]) {
				j++
			}
			if j < n && r[j] == '$' {
				tag := string(r[i : j+1]) // "$tag$" or "$$"
				rest := string(r[j+1:])
				if end := strings.Index(rest, tag); end >= 0 {
					b.WriteRune('?')
					i = j + 1 + end + len([]rune(tag))
					break
				}
				// Unterminated: mask the remainder.
				b.WriteRune('?')
				i = n
				break
			}
			// Not a dollar-quote: keep as-is, including a positional param's
			// digits ($1, $2, ...), which are placeholders, not values.
			b.WriteRune(c)
			i++
			for i < n && r[i] >= '0' && r[i] <= '9' {
				b.WriteRune(r[i])
				i++
			}

		// Quoted identifier "..." — identifiers are not sensitive; keep.
		case c == '"':
			b.WriteRune(c)
			i++
			for i < n {
				b.WriteRune(r[i])
				if r[i] == '"' {
					i++
					break
				}
				i++
			}

		// Numeric literal — only when it starts a token (the previous rune is
		// not part of a word), so identifiers like t1 or col2 are untouched.
		case c >= '0' && c <= '9' && (i == 0 || !isWord(r[i-1])):
			b.WriteRune('?')
			for i < n && (r[i] == '.' || r[i] == 'e' || r[i] == 'E' ||
				r[i] == '+' || r[i] == '-' || (r[i] >= '0' && r[i] <= '9')) {
				// Stop +/- unless directly after an exponent marker.
				if (r[i] == '+' || r[i] == '-') && !(i > 0 && (r[i-1] == 'e' || r[i-1] == 'E')) {
					break
				}
				i++
			}

		default:
			b.WriteRune(c)
			i++
		}
	}
	return b.String()
}
