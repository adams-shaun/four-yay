// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

func kwIncrement(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// Increment (CR 702.XX, task kw:Increment): "Whenever you cast a spell,
	// if the amount of mana you spent is greater than this creature's power
	// or toughness, put a +1/+1 counter on this creature." The corpus prints
	// only the bare `K:Increment` line -- the oracle reminder is the whole
	// rule -- so the expansion mints the SpellCast trigger itself, the
	// Prowess/Storm precedent: an ordinary Mode$ SpellCast trigger on the
	// source whose body counters Self.
	//
	// The event-relative spend-vs-power/toughness comparison rides the
	// trigger's Increment$ marker, read by rules' incrementAdmits at
	// fire-time (rules/trigmatch_cast.go). It is a marker rather than a
	// CheckSVar$/SVarCompare$ gate because SVarCompare$ compares an SVar
	// against a literal, not against the SOURCE's current power/toughness,
	// and because the spend is event-relative -- the mana paid for THIS
	// cast, which only the SpellCast event can name.
	//
	// ValidActivatingPlayer$ You restricts the trigger to the source's own
	// controller ("whenever YOU cast"), and the absent ValidCard$ admits
	// every spell that player casts, creature or not (Increment has no
	// spell-type restriction).
	//
	// TriggerZones$ Battlefield is EXPLICIT because the zone gate's default
	// has a SpellCast special case that admits a trigger with no explicit
	// zones when the triggering event is the source's OWN cast
	// (rules/trigmatch_zone.go: CR 601.2i's "when you cast this spell"
	// carve-out). Without it, casting the Increment creature itself fires its
	// own trigger while it is a spell on the stack and puts a counter on it
	// before it enters -- measured: Pensive Professor arrived as a 1/2. A
	// creature's Increment functions only from the battlefield (CR 113.6), so
	// the explicit zone is the authoritative gate.
	f.addKeywordTrigger(head, k,
		"Mode$ SpellCast | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Increment$ True | TriggerDescription$ Increment (Whenever you cast a spell, if the amount of mana you spent is greater than this creature's power or toughness, put a +1/+1 counter on this creature.)",
		"DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1", has)
}

func init() { registerKeyword(kwIncrement, "Increment") }
