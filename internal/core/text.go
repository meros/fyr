package core

import "unicode/utf8"

// ellipsis marks where TruncHead and TruncTail cut.
const ellipsis = "…"

// TruncHead keeps the start of s in at most limit bytes, the "…" it ends with
// included. It cuts between characters, never inside one.
func TruncHead(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	n := limit - len(ellipsis)
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:max(n, 0)] + ellipsis
}

// TruncTail keeps the end of s in at most limit bytes, the "…" it starts with
// included. It cuts between characters, never inside one.
func TruncTail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	i := len(s) - limit + len(ellipsis)
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return ellipsis + s[min(i, len(s)):]
}

// keepLast keeps the newest n items of a list that grows at its end.
func keepLast[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
