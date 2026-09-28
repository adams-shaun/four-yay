package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 707.2: the nonlegendary characteristic of the Sixth Doctor's spell copy
// is itself copiable. A second CopySpellAbility with no NonLegendary$ True
// must not restore the printed Legendary supertype.
func TestSixthDoctorNonLegendaryCopyOfCopy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "absent"},
		{name: "false", value: "False"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, _ := newFixtureDeck(t, 991, sixthDoctorSrc, legendaryBearSrc)
			doc := moveSeeded(t, e, 0, sixthDoctorSrc, state.ZBattlefield)
			spell := moveSeeded(t, e, 0, legendaryBearSrc, state.ZStack)
			if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || !derivedHasType(e, spell, "Legendary") {
				t.Fatal("precondition: original legendary spell must be on the stack")
			}
			firstSA := cards.ResolveSVar(e.G.Obj(doc).Face().SVars, "TrigCopy")
			if firstSA == nil || firstSA.API != "CopySpellAbility" || firstSA.Params["Defined"] != "TriggeredSpellAbility" || firstSA.Params["NonLegendary"] != "True" {
				t.Fatalf("precondition: Sixth Doctor's compiled TrigCopy missing: %+v", firstSA)
			}
			before := len(e.G.Objs)
			effects.Resolve(e, &effects.Ctx{Source: doc, Controller: 0, Remembered: []state.Target{{Obj: spell}}}, firstSA)
			if len(e.G.Objs) != before+1 {
				t.Fatalf("precondition: Sixth Doctor minted %d objects, want one", len(e.G.Objs)-before)
			}
			first := e.G.Objs[before].ID
			if o := e.G.Obj(first); o.Zone != state.ZStack || !o.IsCopy || derivedHasType(e, first, "Legendary") {
				t.Fatalf("precondition: first copy not a nonlegendary stack spell: %+v types %v", o, e.Derived(first).Types)
			}

			params := map[string]string{"Defined": "TriggeredSpellAbility"}
			if tc.value != "" {
				params["NonLegendary"] = tc.value
			}
			secondSA := &cards.SA{Kind: "DB", API: "CopySpellAbility", Params: params}
			before = len(e.G.Objs)
			effects.Resolve(e, &effects.Ctx{Source: doc, Controller: 0, Remembered: []state.Target{{Obj: first}}}, secondSA)
			if len(e.G.Objs) != before+1 {
				t.Fatalf("precondition: second CopySpellAbility minted %d objects, want one", len(e.G.Objs)-before)
			}
			second := e.G.Objs[before].ID
			if second == first || second == spell || e.G.Obj(second).Zone != state.ZStack || !e.G.Obj(second).IsCopy {
				t.Fatalf("precondition: second copy %d not a distinct stack copy of %d", second, first)
			}
			var lastCopy *events.Event
			for i := range e.L.Events {
				if ev := &e.L.Events[i]; ev.Kind == events.StackCopy && ev.Obj == first {
					lastCopy = ev
				}
			}
			if lastCopy == nil || lastCopy.Counter != "" {
				t.Fatalf("precondition: second copy must come from first WITHOUT a NonLegendary rider; event = %+v", lastCopy)
			}
			if derivedHasType(e, second, "Legendary") {
				t.Fatalf("copy of nonlegendary copy %d derived Legendary types %v; want inherited nonlegendary characteristics", second, e.Derived(second).Types)
			}
			// This fixture placed the spell on the stack directly (without a
			// priority decision), so resolve each copy and the original, then
			// run the same SBA check that ordinarily follows resolution.
			for len(e.G.Stack) > 0 {
				e.resolveTop()
				e.checkStateBased()
				if d := e.Pending(); d != nil {
					t.Fatalf("unexpected decision while checking legend SBAs: %+v", d)
				}
			}
			for _, id := range []state.ObjID{spell, first, second} {
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("obj %d zone = %v, want battlefield after legend SBA", id, o.Zone)
				}
			}
			if !derivedHasType(e, spell, "Legendary") || derivedHasType(e, first, "Legendary") || derivedHasType(e, second, "Legendary") {
				t.Fatalf("derived types after SBA: original=%v first=%v second=%v", e.Derived(spell).Types, e.Derived(first).Types, e.Derived(second).Types)
			}
			replayCheck(t, e, cfg)
		})
	}
}
