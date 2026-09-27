// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import "strings"

func kwOutlast(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	if has("A", k) {
		return
	}
	// CR 702.107a: "Outlast [cost]" means "[cost], {T}: Put a +1/+1
	// counter on this creature. Activate only as a sorcery." It is an
	// ordinary activated ability, so the counter placement goes through
	// the existing PutCounter primitive; SorcerySpeed$ True is the CR
	// 702.107a sorcery-window restriction and the leading T in Cost$ is
	// the {T} tap cost. The ability is repeatable (no once-per-turn
	// machinery): a second activation needs the creature untapped again,
	// exactly what the ordinary tap cost enforces. param is the cost;
	// any trailing fields after a second colon are display text, the
	// same trailing-field strip the Level up and etbCounter cases do.
	cost, _, _ := strings.Cut(param, ":")
	cost = strings.TrimSpace(cost)
	sa, _ := parseSA("", "AB$ PutCounter | Cost$ T "+cost+" | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | SorcerySpeed$ True | Keyword$ Outlast | SpellDescription$ Outlast "+cost)
	if sa != nil {
		sa.Params["KeywordLine"] = k
		f.Abilities = append(f.Abilities, sa)
	}
}

func init() { registerKeyword(kwOutlast, "Outlast") }
