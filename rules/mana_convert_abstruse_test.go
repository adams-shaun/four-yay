package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestAbstruseAppropriationRememberedExileConversion is the end-to-end test
// for the Effect-delivered ManaConvert static that carries an AffectedZone$
// scope and a remembered-card ValidCard$ -- Abstruse Appropriation's real
// corpus shape:
//
//	SVar:DBEffect:DB$ Effect | RememberObjects$ Remembered | StaticAbilities$ MayPlay,ManaConvert ...
//	SVar:ManaConvert:Mode$ ManaConvert | ValidPlayer$ You | ValidCard$ Card.IsRemembered
//	    | ValidSA$ Spell.MayPlaySource | ManaConversion$ C->AnyColor | AffectedZone$ Exile
//
// The card exiles a nonland permanent and grants its controller permission to
// cast it from exile, spending colourless mana as though it were mana of any
// colour. The conversion must register (the AffectedZone$ must be admitted and
// enforced against the cast's ORIGIN zone), the ValidCard$ must resolve against
// the effect's Remembered set, and the exiled card must actually be castable
// with only colourless mana in the pool.
func TestAbstruseAppropriationRememberedExileConversion(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{
			manaConvertCard(t, reg, "Abstruse Appropriation"),
			manaConvertCard(t, reg, "Grizzly Bears"),
		},
		[]*cards.Card{manaConvertCard(t, reg, "Grizzly Bears")})
	// Seat 1 owns the target: a nonland permanent Abstruse exiles.
	bear1 := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	moveByName(t, e, 0, "Abstruse Appropriation", state.ZHand)
	if e.G.Obj(bear1).Zone != state.ZBattlefield {
		t.Fatal("precondition: target Grizzly Bears is not on the battlefield")
	}

	// Cast Abstruse Appropriation targeted at the opponent's bear.
	e.G.Players[0].Pool[state.MW] = 1
	e.G.Players[0].Pool[state.MB] = 1
	e.G.Players[0].Pool[state.MC] = 6
	e.priorityRound()
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil {
		t.Fatal("precondition: no target decision after casting Abstruse Appropriation")
	}
	tgt := -1
	for i, o := range d.Options {
		if o.Obj == bear1 {
			tgt = i
		}
	}
	if tgt < 0 {
		t.Fatalf("precondition: the opponent's Grizzly Bears is not an offered target: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	// Resolve the spell and any triggers.
	for n := 0; n < 10 && len(e.G.Stack) > 0; n++ {
		if p := e.Pending(); p != nil {
			passFirst(t, e)
			continue
		}
		e.resolveTop()
	}
	if e.G.Obj(bear1).Zone != state.ZExile {
		t.Fatalf("precondition: Abstruse Appropriation did not exile the target (zone=%v)", e.G.Obj(bear1).Zone)
	}

	// The ManaConvert static must have registered, remembering the exiled
	// card, and carrying its AffectedZone$ Exile scope (not dropped).
	var registered bool
	for _, ce := range e.active() {
		if ce.CostStaticMode != "ManaConvert" {
			continue
		}
		registered = true
		if len(ce.Remembered) != 1 || ce.Remembered[0] != bear1 {
			t.Fatalf("ManaConvert effect Remembered = %v, want [%d]", ce.Remembered, bear1)
		}
		if got := ce.CostStaticParams["AffectedZone"]; got != "Exile" {
			t.Fatalf("ManaConvert effect AffectedZone$ = %q, want Exile (the real corpus value)", got)
		}
	}
	if !registered {
		t.Fatal("Abstruse Appropriation's Effect-delivered ManaConvert static did not register")
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && containsText(ev.Text, "continuous effect ManaConvert unimplemented") {
			t.Fatalf("Abstruse Appropriation's ManaConvert fell through to its unimplemented handler: %q", ev.Text)
		}
	}

	// The conversion must actually reach the payment path: with only
	// colourless mana, the exiled {1}{G} Grizzly Bears must still be castable,
	// because C->AnyColor lets a colourless unit pay the {G} pip.
	e.G.Players[0].Pool = state.Mana{}
	e.G.Players[0].Pool[state.MC] = 2
	e.priorityRound()
	d = e.Pending()
	off := -1
	for i, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bear1 {
			off = i
		}
	}
	if off < 0 {
		t.Fatalf("precondition: the exiled Grizzly Bears is not castable from colourless mana alone: %+v", optionKinds(d))
	}
	submitChoices(t, e, off)
	// Drive the cast to completion, passing priority and resolving.
	for n := 0; n < 20; n++ {
		p := e.Pending()
		if p == nil {
			if len(e.G.Stack) == 0 {
				break
			}
			e.resolveTop()
			continue
		}
		passFirst(t, e)
	}
	if e.G.Obj(bear1).Zone != state.ZBattlefield {
		t.Fatalf("the exiled Grizzly Bears was not cast with converted colourless mana (zone=%v, pool=%v)",
			e.G.Obj(bear1).Zone, e.G.Players[0].Pool)
	}
}

func containsText(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
