package effects

import "testing"

// TestCompiledPredicateSpellBaseHonoursAsStack: the compiled sidecar must not
// answer a definite No for a Spell base the textual matcher accepts under the
// derived AsStack override. A printed `ValidAfterStack$ Spell.<...>` may-play
// grant (Glarb, Calamity's Augur's `Spell.cmcGE4`; Haakon, Serra Paragon,
// Lurrus, Zask, Thundermane Dragon) evaluates the card still in its origin
// zone as the spell it would become, so a sidecar that answers "not on the
// stack" refused every such grant in a real game while a hand-built engine,
// which has no sidecar, offered it. The precondition asserts the object is
// not on the stack, and that without AsStack the sidecar still says No.
func TestCompiledPredicateSpellBaseHonoursAsStack(t *testing.T) {
	g, ids := board(t)
	bear := g.Obj(ids["myBear"])
	if bear.Zone.String() == "stack" {
		t.Fatalf("precondition: myBear is on the stack")
	}
	ps := CompilePredicatePrograms([]string{"Spell", "Spell.YouCtrl", "SpellAbility"})
	for _, spec := range []string{"Spell", "Spell.YouCtrl"} {
		if got := ps.Evaluate(spec, g, bear, SpecContext{You: 0, Source: ids["myBear"]}); got != PredicateNo {
			t.Fatalf("Evaluate(%q) off the stack without AsStack = %v, want PredicateNo", spec, got)
		}
		got := ps.Evaluate(spec, g, bear, SpecContext{You: 0, Source: ids["myBear"], AsStack: true})
		if got != PredicateYes {
			t.Errorf("Evaluate(%q) with AsStack = %v, want PredicateYes", spec, got)
		}
		if !MatchesSpecCtx(g, spec, ids["myBear"], SpecContext{You: 0, Source: ids["myBear"], AsStack: true}) {
			t.Errorf("textual MatchesSpecCtx(%q) with AsStack = false: the sidecar's Yes has no oracle", spec)
		}
	}
	// SpellAbility is an ability on the stack, never a derived spell.
	if got := ps.Evaluate("SpellAbility", g, bear, SpecContext{You: 0, Source: ids["myBear"], AsStack: true}); got != PredicateNo {
		t.Errorf("Evaluate(SpellAbility) with AsStack = %v, want PredicateNo", got)
	}
}
