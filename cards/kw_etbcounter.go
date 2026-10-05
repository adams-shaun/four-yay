// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import (
	"strconv"
	"strings"
)

func kwEtbCounter(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	if has("R", k) {
		return
	}
	// param is "<KIND>:<N>", occasionally followed by further
	// colon-separated fields real Forge reads for its own
	// bookkeeping (a CheckSVar$ condition, a human-readable
	// description) -- those are not part of <N> and are dropped
	// rather than spliced into CounterNum$ or the Repl body (some
	// contain their own "|", which would otherwise inject a
	// spurious param into both).
	kind, rest, _ := strings.Cut(param, ":")
	n, extra, _ := strings.Cut(rest, ":")
	sv := "__kwEtbCounter" + strconv.Itoa(i)
	f.setSVar(sv, "DB$ PutCounter | Defined$ Self | CounterType$ "+kind+" | CounterNum$ "+n+" | ETB$ True")
	p := parseParams("Event$ Moved | Destination$ Battlefield | ValidCard$ Card.Self | ReplacementResult$ Updated | ReplaceWith$ " + sv +
		" | Keyword$ etbCounter | Description$ CARDNAME enters with " + n + " " + kind + " counters.")
	// The FIRST extra colon field may be a condition gate: either a
	// bare `CheckSVar$ <name>` (Lupine Harbingers' "... since it was
	// foretold", Myojin of Night's Reach's "if you cast it from your
	// hand") or a `CheckSVar$ <name> | SVarCompare$ <op><N>` pair
	// (Hotheaded Giant, Freestrider Commando, Steel Exemplar). Split
	// the ` | `-separated tokens through to the shared
	// rules/replacementConditionHolds read (CheckSVar + optional
	// SVarCompare) instead of stuffing the whole field into one param:
	// a whole-stuffed CheckSVar resolves no SVar, the gate fails
	// closed, and those carriers' counters silently un-apply (the
	// round-2 review's measured regression). Only these two condition
	// params are passed through: a gate field can also carry real
	// match params (the Myojin-family lines carry `ValidCard$ ...`
	// here) that the replacement matcher honours, and passing those
	// through would widen every carrier's match. Everything else stays
	// dropped, exactly as before -- the later colon fields remain
	// display metadata.
	if first, _, _ := strings.Cut(extra, ":"); strings.Contains(first, "$") {
		for part := range strings.SplitSeq(first, " | ") {
			name, val, ok := strings.Cut(strings.TrimSpace(part), "$")
			if !ok {
				continue
			}
			switch strings.TrimSpace(name) {
			case "CheckSVar", "SVarCompare", "IsPresent":
				if val = strings.TrimSpace(val); val != "" {
					p[strings.TrimSpace(name)] = val
				}
			case "Revolt":
				if val = strings.TrimSpace(val); strings.EqualFold(val, "True") {
					p["Revolt"] = val
				}
			case "Adamant":
				// Adamant is the keyword action-word for spending at least three
				// mana of a specified color. Route it through the common Count$
				// gate so the replacement uses the same captured payment as other
				// count consumers.
				color := strings.TrimSpace(val)
				if color != "" {
					p["CheckSVar"] = "Count$Adamant_3." + color + ".1.0"
				}
			case "ValidCard":
				// A gate field's ValidCard$ is a real match param the
				// replacement matcher honours -- epochrasite's
				// `Card.Self+!wasCastFromYourHandByYou` (task castprov1)
				// and the escape-counter family's `Card.Self+escaped`,
				// all 11 raw carriers spelled `Card.Self+<preds>`. It
				// replaces the default `Card.Self` ONLY when the gate's
				// own spec still constrains Self (a Self-less fragment
				// would widen the default's match, the pre-existing
				// drop's reason); a spec the filter fails closed on
				// (wasCastByYou's unknown predicate) keeps failing
				// closed. Measured: no carrier is in any repo deck.
				if val = strings.TrimSpace(val); val != "" && strings.Contains(val, "Self") {
					p["ValidCard"] = val
				}
			}
		}
	}
	p["KeywordLine"] = k
	f.Repls = append(f.Repls, Repl{Event: "Moved", Params: p})
}

func init() { registerKeyword(kwEtbCounter, "etbCounter") }
