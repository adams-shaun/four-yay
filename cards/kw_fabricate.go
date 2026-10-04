// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import "strconv"

func kwFabricate(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// CR 702.121a: "When this permanent enters the battlefield, you may
	// put N +1/+1 counters on it or create N 1/1 colorless Servo
	// artifact creature tokens." The counters-or-servos election is a
	// modal choice, so the expansion is one ChangesZone ETB trigger
	// whose effect is a DB$ Charm over the two mode sub-abilities --
	// the Knight of Autumn shape (DB$ Charm | Choices$ DBPump,...). No
	// new decision kind is needed: effCharm already poses the KModes
	// ask and its answer is applied in place.
	//
	// The param is a bare literal count on every corpus line (measured:
	// 16 files, values 1/2/3, no trailing fields), so it is spliced in
	// verbatim as CounterNum$/TokenAmount$. Both mode SVar names are
	// minted from i so two Fabricate lines on one face cannot collide,
	// and addKeywordTrigger guards idempotency via the KeywordLine tag,
	// so a second Link() of a cached face adds nothing.
	n := param
	if n == "" {
		n = "1"
	}
	sv := "__kwFabricate" + strconv.Itoa(i)
	pv := sv + "Counter"
	tv := sv + "Token"
	f.setSVar(pv, "DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ "+n+
		" | SpellDescription$ Put "+n+" +1/+1 counters on CARDNAME.")
	f.setSVar(tv, "DB$ Token | TokenScript$ c_1_1_a_servo | TokenAmount$ "+n+
		" | SpellDescription$ Create "+n+" 1/1 colorless Servo artifact creature tokens.")
	f.addKeywordTrigger(head, k, "Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | TriggerDescription$ Fabricate",
		"DB$ Charm | Choices$ "+pv+","+tv, has)
}

func init() { registerKeyword(kwFabricate, "Fabricate") }
