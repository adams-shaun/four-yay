package state

import "strings"

// caseVariantCounterKinds maps the corpus's minority spellings of a counter
// kind to the one every reader uses. Forge resolves counter kinds through
// CounterEnumType case-insensitively, so `CounterType$ Stun` (72 PutCounter
// lines, e.g. Fear of Sleep Paralysis) IS the STUN counter that the untap
// replacement (effects/untap.go) and `ValidCounterType$ STUN` read; likewise
// Overseer of Vault 76's `Quest` (paid back as RemoveAnyCounter<3/QUEST>) and
// Lost Isle Calling's `Verse` (read back as CardCounters.VERSE). Shadows'
// Lair's `SubCounter<1/Dread>` is the one COST-side minority spelling: its
// front face places `CounterType$ DREAD`. A blanket upper-casing would be
// wrong: keyword counters (Flying, Deathtouch, ...) and the engine's own
// Shield marker are case-significant here.
var caseVariantCounterKinds = map[string]string{
	"Stun":  "STUN",
	"Quest": "QUEST",
	"Verse": "VERSE",
	"Dread": "DREAD",
}

// CanonicalCounterKind folds a counter kind (or each entry of a comma list)
// onto its canonical spelling; anything else is returned unchanged. It is the
// ONE home of the fold: the PutCounter parameter reader (effects) and the
// SubCounter / RemoveAnyCounter cost parser (rules/cost) both call it, so a
// kind cannot be placed under one spelling and paid back under another.
func CanonicalCounterKind(kind string) string {
	if !strings.Contains(kind, ",") {
		if k, ok := caseVariantCounterKinds[strings.TrimSpace(kind)]; ok {
			return k
		}
		return kind
	}
	parts := strings.Split(kind, ",")
	for i, p := range parts {
		if k, ok := caseVariantCounterKinds[strings.TrimSpace(p)]; ok {
			parts[i] = k
		}
	}
	return strings.Join(parts, ",")
}
