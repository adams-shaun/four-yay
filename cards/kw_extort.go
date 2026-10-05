// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

func kwExtort(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// CR 702.100: "Whenever you cast a spell, you may pay {W/B}. If you
	// do, each opponent loses 1 life and you gain that much life." A
	// SpellCast trigger on the controller; the optional {W/B} payment is
	// efectively asked in effExtort (the mid-resolution KModes ask) and
	// the drain runs per spell cast.
	f.addKeywordTrigger(head, k, "Mode$ SpellCast | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | TriggerDescription$ Extort",
		"DB$ Extort", has)
}

func init() { registerKeyword(kwExtort, "Extort") }
