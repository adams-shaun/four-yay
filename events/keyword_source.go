package events

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// keywordTriggerBindsSource reports whether a keyword-triggered ability's own
// resolution acts on the PARTICULAR permanent incarnation that was its source
// (CR 400.7) rather than on the triggering event or object alone (CR 112.7a).
//
// resolveTop's source-incarnation gate exists for the former only: an ability
// resolves independently of its source (CR 112.7a), so "acts on the source"
// is the exception a producer must demonstrate, not the default. The stamp
// this predicate guards shipped wired to the opposite default -- every
// KeywordTriggerPush except Gift (the `if !gift` arm at Apply) -- which
// dropped a granted Ward or Afflict ability whose source was removed in
// response to its own trigger, even though both act on the triggering object
// or player and never read the source.
//
// Wherever the ability body itself names the source, that IS the
// demonstration, and the walk finds it: a synthesized or resolved body that
// says Defined$ Self acts on the source permanent (Evoke's sacrifice,
// Offspring's copy, Melee's pump), and a body that does not say Self is
// independent unless it is one of the rules-side bodies below. Nothing here
// is keyed on whether the source currently exists at resolution: a source
// that changed zones without crossing the battlefield boundary keeps its
// incarnation, and one that left and returned has a new one -- the gate's
// whole point (CR 400.7).
//
// The explicit set is exactly the keyword bodies whose source read is
// IMPLICIT in rules code and therefore invisible to the SA walk. Each is
// listed with the read that makes it source-bound; a keyword trigger not
// named here defaults to independent, which is CR 112.7a's own default.
func keywordTriggerBindsSource(counter string, sa *cards.SA) bool {
	if saReadsSource(sa) {
		return true
	}
	switch {
	case strings.HasPrefix(counter, "__kwCascade:"):
		// effects.effCascade reads the source spell's mana value
		// (g.Obj(c.Source).Face().Cmc() + src.X); it must be the same cast.
		return true
	case strings.HasPrefix(counter, "__kwCumulativeUpkeepGranted:"):
		// rules.startCumulativeUpkeep acts on the source permanent (the
		// age counter goes on it, the cost is paid by its controller) and
		// bails if it has left the battlefield.
		return true
	case counter == "__kwMadnessCast":
		// rules.askMadnessCast reads the exiled source card and only offers
		// the cast while that exact card is still in exile.
		return true
	}
	return false
}

// saReadsSource walks an ability's Sub chain for a parameter that names the
// source object. Forge spells the source referent Defined$ Self (and, for
// Attach-style bodies, Object$ Self); a *filter* over the source spells it
// as a .Self suffix on Valid$/ValidTgts$ (e.g. Valid$ Card.Self). Any of
// them means the resolution touches the source permanent and must not be
// re-pointed at a new object sharing its stable ObjID (CR 400.7).
func saReadsSource(sa *cards.SA) bool {
	for ; sa != nil; sa = sa.Sub {
		switch sa.Params["Defined"] {
		case "Self":
			return true
		}
		switch sa.Params["Object"] {
		case "Self":
			return true
		}
		for _, key := range [...]string{"Valid", "ValidTgts", "Target", "DefinedOf"} {
			if strings.HasSuffix(strings.TrimSpace(sa.Params[key]), ".Self") {
				return true
			}
		}
	}
	return false
}
