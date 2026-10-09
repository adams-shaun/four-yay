package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The prepared spell-cast trigger names a predicate the ordinary probe cast
// from p0's hand can never satisfy: Card.prepared is the CR 722.3c
// prepared-copy cast provenance (the pay-time FlagPreparedCopy bit), which
// only the prepared designation's exile copy carries when it is cast. The
// engine's trigger matcher already evaluates the predicate
// (effects/filter.go); only the CAUSE was missing, so the row skipped with
// "spell-cast filter predicate gorge does not implement (prepared)"
// (spellCastNarrowSkip) before the predicate landed. The cause below
// produces the cast the predicate needs, and gorge's own matcher decides
// whether it fires. Row: Codie, Ravenous Codex, the one corpus consumer of
// ValidCard$ Card.prepared.
//
// preparedCastCarrier is the corpus card whose ETB replacement demonstrates
// the grant the trigger watches ("While it's prepared, you may cast a copy
// of its spell"): placed in setup, its K:ETBReplacement enters it prepared
// and the grant mints the exile copy of its prepare-spell face, which the
// runner addresses as p0:<prepare face>. It is also the engine's own
// prepared-copy regression carrier (rules/testdata/oracle/conditional-static/
// whiplash-wordsmith.json), so the flow the cause replays is the tested one.
const preparedCastCarrier = "Whiplash Wordsmith"

func spellCastPreparedCause(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []triggerCause {
	filter := strings.ToLower(t.ParamStr(cards.PKValidCard))
	if !filterNamesToken(filter, "prepared") {
		return nil
	}
	carrier, ok := reg.Lookup(preparedCastCarrier)
	if !ok || len(carrier.Faces) < 2 {
		return nil
	}
	prepare := carrier.Faces[1]
	if prepare.SpellAbility() == nil || !oraclegen.XMageKnown(prepare.Name) {
		return nil
	}
	pool, why := oraclegen.PoolFor(prepare.ManaCost)
	if why != "" {
		return nil
	}
	st := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + prepare.Name, Mana: pool, CastMode: "prepared_copy"}
	if oraclegenPlayerTargetHead(strings.ToLower(prepare.SpellAbility().ParamStr(cards.PKValidTgts))) {
		st.Targets = []string{"p1"}
	}
	return []triggerCause{{battlefield: []string{preparedCastCarrier}, steps: []oraclegen.Step{st}}}
}

// filterNamesToken reports whether the comma/plus/dot-separated filter
// carries pred as its own token. It keeps a bare predicate ("prepared")
// from being mistaken for a longer word sharing the stem ("Unprepared").
func filterNamesToken(filter, pred string) bool {
	for _, seg := range strings.FieldsFunc(filter, func(r rune) bool {
		return r == '.' || r == '+' || r == ','
	}) {
		if seg == pred {
			return true
		}
	}
	return false
}

// oraclegenPlayerTargetHead is gen.go's playerTargetHead, which is
// unexported there: the first comma alternative's head names a player.
func oraclegenPlayerTargetHead(filter string) bool {
	head := strings.ToLower(strings.SplitN(strings.Split(filter, ",")[0], ".", 2)[0])
	return head == "player" || head == "opponent"
}
