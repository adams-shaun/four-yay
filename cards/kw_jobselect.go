// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

func kwJobSelect(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// CR 702.182: "Job select (When this Equipment enters, create a 1/1
	// colorless Hero creature token, then attach this to it.)" Exactly the
	// For Mirrodin! / Living Weapon shape (an enters-the-battlefield
	// trigger on the Equipment itself, remembering the token it mints so
	// the chained Attach can name it); only the token script (c_1_1_hero)
	// and the display text differ. Forge puts no trailing parameter on
	// K:Job select.
	if has("T", k) {
		return
	}
	f.setSVar("__kwJSAttach", "DB$ Attach | Defined$ Remembered | Object$ Self")
	f.addKeywordTrigger(head, k, "Mode$ ChangesZone | Destination$ Battlefield | ValidCard$ Card.Self | TriggerDescription$ Job select",
		"DB$ Token | TokenScript$ c_1_1_hero | TokenOwner$ You | RememberTokens$ True | SubAbility$ __kwJSAttach", has)
}

func init() { registerKeyword(kwJobSelect, "Job select") }
