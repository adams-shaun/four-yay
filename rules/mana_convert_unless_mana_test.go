package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestUnlessPayTapsForConvertedMana is the end-to-end test for the
// mid-resolution unless-pay mana window (the `unless_pay` ask). Real
// corpus card: Knight of the Mists' enters-the-battlefield "you may pay {U}.
// If you don't, destroy target Knight". The payer controls a Mycosynth Lattice
// ("Players may spend mana as though it were mana of any color",
// ManaConversion$ AnyType->AnyColor) and one untapped Mountain, with an EMPTY
// pool.
//
// Before this change the unless arm charged the floating pool only: with an
// empty pool the {U} was unpayable and the payer silently declined (destroying
// its own Knight). The window lets the payer tap the Mountain (R) and pay the
// {U} pip with the converted red mana, so the Knight survives.
func TestUnlessPayTapsForConvertedMana(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		manaConvertCard(t, reg, "Knight of the Mists"),
		manaConvertCard(t, reg, "Mycosynth Lattice"),
		manaConvertCard(t, reg, "Mountain"),
	}, nil)
	moveByName(t, e, 0, "Mycosynth Lattice", state.ZBattlefield)
	mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	e.G.Players[0].Pool = state.Mana{}
	knight := moveByName(t, e, 0, "Knight of the Mists", state.ZBattlefield)

	// Preconditions: the tapped source exists and is untapped, the pool is
	// empty, the Knight is on the battlefield, and the converter is live.
	if e.G.Obj(mountain).Tapped {
		t.Fatal("precondition: the Mountain must be untapped")
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("precondition: the pool must be empty, got %v", e.G.Players[0].Pool)
	}
	if e.G.Obj(knight).Zone != state.ZBattlefield {
		t.Fatal("precondition: Knight of the Mists is not on the battlefield")
	}
	if !e.hasUntappedManaSource(0) {
		t.Fatal("precondition: no untapped mana source is available to the window")
	}

	// Drive to the unless gate, then the window. The ETB trigger asks for a
	// target Knight first; the Knight targets itself.
	var sawGate, sawWindow, sawActivate bool
	for n := 0; n < 40; n++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 {
				break
			}
			e.resolveTop()
			continue
		}
		switch {
		case d.Kind == decision.KPriority:
			passFirst(t, e)
		case d.Kind == decision.KModes && containsText(d.Prompt, "Pay U"):
			sawGate = true
			submitChoices(t, e, 0) // "Pay U"
		case d.Kind == decision.KChoose && containsText(d.Prompt, "unless cost"):
			sawWindow = true
			// First window pass: tap the Mountain. Later passes (the source
			// is now tapped) take Done.
			pick := -1
			for i, o := range d.Options {
				if o.Kind == "activate" && o.Obj == mountain {
					pick = i
				}
			}
			if pick < 0 {
				for i, o := range d.Options {
					if o.Kind == "done" {
						pick = i
					}
				}
			}
			if pick < 0 {
				t.Fatalf("window posed with no activate or done option: %+v", d.Options)
			}
			if d.Options[pick].Kind == "activate" {
				sawActivate = true
			}
			submitChoices(t, e, pick)
		case d.Kind == decision.KChoose:
			submitChoices(t, e, 0) // the target choice
		default:
			submitChoices(t, e, 0)
		}
		_ = n
	}
	if !sawGate {
		t.Fatal("precondition: Knight of the Mists' unless gate was never posed")
	}
	if !sawWindow {
		t.Fatal("the unless-pay arm never opened a mana-activation window (pool-only behaviour)")
	}
	if !sawActivate {
		t.Fatal("the window never offered/used the Mountain's activation")
	}
	if e.G.Obj(knight).Zone != state.ZBattlefield {
		t.Fatalf("the Knight was destroyed: the converted red mana did not pay the {U} unless cost (zone=%v, pool=%v)",
			e.G.Obj(knight).Zone, e.G.Players[0].Pool)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("the converted mana was not spent (pool=%v)", e.G.Players[0].Pool)
	}
}
