// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

func kwTraining(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// CR 702.70: "Whenever this creature attacks with another creature with
	// greater power, put a +1/+1 counter on this creature." The
	// event-relative power comparison is in trigmatch.AttacksMatches (the Dethrone
	// precedent), so the expansion is an ordinary Attacks trigger carrying
	// the Training$ marker.
	f.addKeywordTrigger(head, k, "Mode$ Attacks | ValidCard$ Card.Self | Training$ True | TriggerDescription$ Training",
		"DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1", has)
}

func init() { registerKeyword(kwTraining, "Training") }
