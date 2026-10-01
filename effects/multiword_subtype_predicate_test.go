package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestMultiWordSubtypePredicate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	tardis := corpusObject(t, reg, g, "TARDIS")
	withTypes := func(types ...string) *state.Object {
		o := corpusObject(t, reg, g, "TARDIS")
		cardCopy := *o.Card
		faceCopy := *o.Card.Faces[0]
		faceCopy.Types = types
		cardCopy.Faces = []*cards.Face{&faceCopy}
		o.SetCard(&cardCopy, o.FaceIdx)
		if o.Zone != state.ZBattlefield || o.Controller != 0 {
			t.Fatal("predicate fixture must be controlled on the battlefield")
		}
		return o
	}
	lord := withTypes("Creature", "Time", "Lord")
	onlyTime := withTypes("Creature", "Time")
	onlyLord := withTypes("Creature", "Lord")
	if tardis.Zone != state.ZBattlefield || tardis.Controller != 0 ||
		hasType(tardis, "Time") || hasType(tardis, "Lord") ||
		!predicateTypeWords["Time"] || !predicateTypeWords["Creature"] ||
		!hasType(lord, "Time") || !hasType(lord, "Lord") || !hasType(lord, "Creature") ||
		!hasType(onlyTime, "Time") || hasType(onlyTime, "Lord") ||
		!hasType(onlyLord, "Lord") || hasType(onlyLord, "Time") {
		t.Fatal("fixtures must differ in their constituent type words on controlled battlefield objects")
	}
	spec := "Card.Time Lord+YouCtrl"
	if un := UnknownPredicates(spec); len(un) != 0 {
		t.Fatalf("UnknownPredicates(%q) = %v, want none", spec, un)
	}
	if !MatchesObjectCtx(g, spec, lord, SpecContext{You: 0}) {
		t.Fatal("Card.Time Lord+YouCtrl must match a controlled Time Lord")
	}
	for _, o := range []*state.Object{onlyTime, onlyLord, tardis} {
		if MatchesObjectCtx(g, spec, o, SpecContext{You: 0}) {
			t.Fatalf("Card.Time Lord+YouCtrl must reject types %v", o.Face().Types)
		}
	}
	unsupported := "Card.Time Zorb+YouCtrl"
	if MatchesObjectCtx(g, unsupported, lord, SpecContext{You: 0}) {
		t.Fatal("unsupported multiword predicate must fail closed")
	}
	if un := UnknownPredicates(unsupported); len(un) != 1 || un[0] != "Time Zorb" {
		t.Fatalf("UnknownPredicates(%q) = %v, want [Time Zorb]", unsupported, un)
	}
	// Both words are individually known, but their combination is not a subtype.
	for _, token := range []string{"Time Creature", "!Time Creature"} {
		s := "Card." + token + "+YouCtrl"
		if MatchesObjectCtx(g, s, lord, SpecContext{You: 0}) ||
			MatchesObjectCtx(g, s, onlyLord, SpecContext{You: 0}) {
			t.Errorf("%s must fail closed even when negated", s)
		}
		if un := UnknownPredicates(s); len(un) != 1 || un[0] != token {
			t.Errorf("UnknownPredicates(%q) = %v, want [%s]", s, un, token)
		}
	}
	if !MatchesObjectCtx(g, "Creature.Time Lord+YouCtrl", lord, SpecContext{You: 0}) {
		t.Fatal("TARDIS-shaped IsPresent filter must match controlled Time Lord")
	}
}
