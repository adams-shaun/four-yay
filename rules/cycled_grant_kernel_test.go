package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestGrantedTypeCyclingCrossSeatActivationFiresCycledTrigger is the
// TypeCycling + cross-seat half: Homing Sliver's
// `AddKeyword$ TypeCycling:Sliver:3` grant (Affected$ Sliver, no controller
// qualifier) gives a SLIVER card in EACH player's hand slivercycling -- here
// seat 1's, while seat 0 controls the grantor -- and activating it discards
// with the TypeCycling provenance, firing the sliver's own Cycled trigger.
func TestGrantedTypeCyclingCrossSeatActivationFiresCycledTrigger(t *testing.T) {
	t.Parallel()
	homing := mshCorpusCard(t, "Homing Sliver")
	const sliver = "Name:Test Sliver\nManaCost:1 G\nTypes:Creature Sliver\nPT:1/1\n" +
		"T:Mode$ Cycled | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ When you cycle CARDNAME, draw a card.\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\nOracle:x\n"
	e := handEngine(t)
	onBoardCard(t, e, 0, homing)
	sliverCard := card(t, sliver)
	o := e.G.AddObject(sliverCard, 1)
	o.Zone = state.ZHand
	sliverID := o.ID
	e.G.SetZone(state.ZHand, 1, []state.ObjID{sliverID})
	// A second Sliver sits BELOW the library top: the slivercycling search
	// needs a findable Sliver in seat 1's library (the fixture deck is
	// Mountains), and the Cycled trigger's draw (which resolves first, CR
	// 117.5) must not take it -- hence not the top card.
	const deepSliver = "Name:Deep Sliver\nManaCost:1 G\nTypes:Creature Sliver\nPT:1/1\nOracle:x\n"
	deep := e.G.AddObject(card(t, deepSliver), 1)
	deep.Zone = state.ZLibrary
	lib := e.G.Zone(state.ZLibrary, 1)
	e.G.SetZone(state.ZLibrary, 1, append([]state.ObjID{lib[0], deep.ID}, lib[1:]...))
	if o := e.G.Obj(sliverID); o == nil || o.Zone != state.ZHand || o.Controller != 1 {
		t.Fatalf("precondition: seat 1's sliver = %+v, want in seat 1's hand", o)
	}
	if sf := e.G.Obj(sliverID).Face(); sf.HasKeyword("TypeCycling") || sf.HasKeyword("Cycling") {
		t.Fatal("precondition: the sliver must print no cycling of its own, else the offered ability is not the granted one")
	}
	if param, ok := e.derivedKeywordParam(sliverID, "TypeCycling"); !ok || param != "Sliver:3" {
		t.Fatalf("precondition: derived slivercycling grant = (%q, %v), want (\"Sliver:3\", true)", param, ok)
	}

	addMana(t, e, 1, "CCC")
	opt, ok := grantedCyclingOption(e, 1, sliverID)
	if !ok {
		t.Fatal("seat 1's granted slivercycling was never offered: the grant (Affected$ Sliver, no controller qualifier) must reach every player's hand")
	}
	if opt.Keyword != "TypeCycling:Sliver:3" {
		t.Fatalf("granted option keyword = %q, want \"TypeCycling:Sliver:3\"", opt.Keyword)
	}

	e.beginActivation(1, opt)
	submitChoices(t, e, 0) // the cost's Discard<1/CARDNAME>: the sliver itself
	if got := e.G.Obj(sliverID).Zone; got != state.ZGraveyard {
		t.Fatalf("the granted slivercycling's discard left the sliver in %s, want graveyard", got)
	}
	for _, ev := range e.L.Events {
		if ev.Obj != sliverID || !events.IsDiscardCost(ev) {
			continue
		}
		kw, ok := events.IsCyclingDiscard(ev)
		if !ok || kw != "TypeCycling" {
			t.Fatalf("the granted slivercycling's cost discard was not tagged TypeCycling (kw=%q ok=%v)", kw, ok)
		}
	}
	if n := countKind(e.L.Events, events.KeywordAbilityPush, sliverID); n != 1 {
		t.Fatalf("KeywordAbilityPush count = %d, want 1", n)
	}
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack depth after the granted slivercycling = %d, want 2 (ability + the sliver's own Cycled trigger)", len(e.G.Stack))
	}
	libTop := e.G.Zone(state.ZLibrary, 1)[0]
	e.pending = nil
	e.resolveTop() // the sliver's Cycled trigger (its draw takes the mountain on top)
	e.pending = nil
	e.resolveTop() // the slivercycling search poses its found-card ask
	if e.G.Obj(libTop).Zone != state.ZHand {
		t.Fatal("the sliver's Cycled trigger did not draw")
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("the granted slivercycling search posed no found-card ask")
	}
	choice := -1
	for i, o := range d.Options {
		if o.Obj == deep.ID {
			choice = i
		}
	}
	if choice < 0 {
		t.Fatalf("the granted slivercycling search did not offer the library Sliver: %+v", d)
	}
	submitChoices(t, e, choice)
	if got := e.G.Obj(deep.ID).Zone; got != state.ZHand {
		t.Fatalf("the granted slivercycling search left the library Sliver in %s, want hand (revealed and put into hand, CR 702.28d)", got)
	}
}
