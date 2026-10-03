package compliance

import "strings"

// CorpusName maps an XMage card name onto the corpus's name for the same
// card: the exact name first, then the front face of an "A // B" name (the
// corpus names a split card by both halves but a double-faced card by its
// front face; XMage writes both as "A // B").
func CorpusName(has func(string) bool, xmageName string) (string, bool) {
	if has(xmageName) {
		return xmageName, true
	}
	if front, _, ok := strings.Cut(xmageName, " // "); ok && has(front) {
		return front, true
	}
	return "", false
}
