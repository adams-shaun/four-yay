package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestChooseTapAimsAReflectedSourceAtANeededPip pins the tap-to-pay half of
// the api:ManaReflected ticket. A reflected source now reaches the Board as a
// conditional production with cards.ManaProduction.Reflected set
// (cards.ManaProduction.addReflected), and the need-aware tap gate must treat
// it as able to answer a needed coloured pip: the engine asks for the colour
// when the source is actually tapped, so it can pay any pip. Before the fix a
// reflected source projected as producing nothing, so the satisfiability
// filter found the colour unpayable and chooseTap returned -1 (pass) instead
// of tapping it.
func TestChooseTapAimsAReflectedSourceAtANeededPip(t *testing.T) {
	const (
		spell  = state.ObjID(1)
		decoy  = state.ObjID(2)
		refSrc = state.ObjID(3)
	)
	var decoyProd cards.ManaProduction
	decoyProd.Colour[0] = 1 // a plain white source
	var refProd cards.ManaProduction
	refProd.Reflected = true
	refProd.Any = true
	refProd.Colour[5] = 1 // the reflected/conditional source

	b := Board{IsMain: true, Pool: state.Mana{},
		Cards: TableOf(map[state.ObjID]Card{
			spell:  {Castable: true, CMC: 1, ManaCost: "U"},
			decoy:  {OnBattlefield: true, Produces: decoyProd},
			refSrc: {OnBattlefield: true, Produces: refProd},
		})}
	// Precondition: the spell really needs a U pip the empty pool lacks, and
	// the reflected source is NOT already a known U producer (it is honest
	// only through the Reflected flag).
	if pips := colourPips("U"); pips[state.MU] != 1 || b.Pool[state.MU] != 0 {
		t.Fatalf("precondition failed: pips=%v poolU=%d", pips, b.Pool[state.MU])
	}
	if refProd.ProducesColour(state.MU) {
		t.Fatal("precondition failed: the reflected source must not claim a guaranteed U slot")
	}

	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: decoy},
			{Index: 1, Kind: "activate", Obj: refSrc},
			{Index: 2, Kind: "pass"},
		}}
	if got := b.chooseTap(d); got != 1 {
		t.Fatalf("chooseTap = %d, want the reflected source (option 1) aimed at the U pip", got)
	}
}

// TestChooseTapAimsPlainAnySourceAtANeededPip checks conditional any-colour production.
func TestChooseTapAimsPlainAnySourceAtANeededPip(t *testing.T) {
	const (
		spell  = state.ObjID(1)
		anySrc = state.ObjID(2)
	)
	var anyProd cards.ManaProduction
	anyProd.Any = true // a Cavern-of-Souls-shaped conditional source
	anyProd.Colour[5] = 1

	b := Board{IsMain: true, Pool: state.Mana{},
		Cards: TableOf(map[state.ObjID]Card{
			spell:  {Castable: true, CMC: 1, ManaCost: "U"},
			anySrc: {OnBattlefield: true, Produces: anyProd},
		})}
	// Precondition: the spell needs a U pip, and the plain Any source claims
	// no guaranteed colour and is not reflected.
	if pips := colourPips("U"); pips[state.MU] != 1 || b.Pool[state.MU] != 0 {
		t.Fatalf("precondition failed: pips=%v poolU=%d", pips, b.Pool[state.MU])
	}
	if !anyProd.Any || anyProd.Reflected || anyProd.ProducesColour(state.MU) {
		t.Fatalf("precondition failed: want a plain Any (non-reflected) source: %+v", anyProd)
	}

	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: anySrc},
			{Index: 1, Kind: "pass"},
		}}
	if got := b.chooseTap(d); got != 0 {
		t.Fatalf("chooseTap = %d, want the plain Any source (option 0) aimed at the U pip", got)
	}
}
