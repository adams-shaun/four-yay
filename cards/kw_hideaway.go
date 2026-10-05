// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import "strings"

func kwHideaway(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	if has("R", k) {
		return
	}
	// Hideaway is an enters-the-battlefield triggered ability. Keep the
	// keyword parameter as data so its varying N is not lost.
	n := strings.TrimSpace(param)
	if n == "" {
		n = "4"
	}
	f.addKeywordTrigger("Hideaway", k,
		"Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | TriggerDescription$ Hideaway",
		"DB$ Hideaway | Amount$ "+n, has)
}

func init() { registerKeyword(kwHideaway, "Hideaway") }
