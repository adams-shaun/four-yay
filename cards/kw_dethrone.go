// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

func kwDethrone(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// CR 702.105's event-relative life comparison is in trigmatch.AttacksMatches.
	f.addKeywordTrigger(head, k, "Mode$ Attacks | ValidCard$ Card.Self | Dethrone$ True | TriggerDescription$ Dethrone",
		"DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1", has)
}

func init() { registerKeyword(kwDethrone, "Dethrone") }
