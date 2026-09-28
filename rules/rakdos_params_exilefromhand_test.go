package rules

// The Rakdos-params brief, gap 4: ExileFromHand<...> in Cost$. The head was
// modelled by the earlier alternative-cost work (ParseCost's exileCost), and
// the census pin already asserts Force of Will's ExileFromHand<1/Card.Blue+Other>
// no longer degrades; this is the behaviour half on a second real corpus
// card from the audit table: Bounty of the Hunt's "You may exile a green card
// from your hand rather than pay this spell's mana cost."

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// battlefieldCreature seeds a creature straight onto seat 0's battlefield.
func battlefieldCreature(t *testing.T, e *Engine, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return o.ID
}

func TestBountyOfTheHuntPaysItsExileFromHandAlternativeCost(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Bounty of the Hunt"),
		card(t, "Name:Green Fuel\nManaCost:G\nTypes:Creature Insect\nPT:1/1\nOracle:x\n"),
		card(t, "Name:Blue Fuel\nManaCost:U\nTypes:Creature\nPT:1/1\nOracle:x\n"))
	bear := battlefieldCreature(t, e, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bounty := e.G.Zone(state.ZHand, 0)[0]
	green, blue := e.G.Zone(state.ZHand, 0)[1], e.G.Zone(state.ZHand, 0)[2]
	// The spell targets a creature (PutCounter); without one the cast would
	// fizzle at the 601.2c backstop after the alternative was paid.

	addMana(t, e, 0, "")
	d := e.Pending()
	alt := -1
	base := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bounty {
			if o.AltCostIndex != 0 {
				alt = o.Index
			} else {
				base = o.Index
			}
		}
	}
	if alt < 0 {
		t.Fatalf("exile alternative not offered on an empty pool: %+v", d.Options)
	}
	if base >= 0 {
		t.Fatalf("the {3}{G}{G} cast offered with an empty pool: %+v", d.Options)
	}
	submitChoices(t, e, alt)
	// The exile ask offers the GREEN hand card only (Card.Green+Other; Other
	// excludes the spell itself).
	de := e.Pending()
	if de == nil || de.Kind != decision.KChoose || de.Options[0].Kind != "exilecost" {
		t.Fatalf("exile cost ask missing: %+v", de)
	}
	if len(de.Options) != 1 || de.Options[0].Obj != green {
		t.Fatalf("exile options %+v, want exactly the green hand card", de.Options)
	}
	submitChoices(t, e, de.Options[0].Index)
	// The spell still asks its target (DividedAsYouChoose 3) before the exile
	// is executed.
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("expected the target decision, got %+v", td)
	}
	submitChoices(t, e, td.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatalf("bear zone=%s", e.G.Obj(bear).Zone)
	}
	if e.G.Obj(green).Zone != state.ZExile {
		t.Fatalf("green card zone=%s, want exile (the alternative payment)", e.G.Obj(green).Zone)
	}
	if e.G.Obj(blue).Zone != state.ZHand {
		t.Fatalf("blue card zone=%s, want hand (wrong colour never swept)", e.G.Obj(blue).Zone)
	}
	if e.G.Obj(bounty).Zone != state.ZGraveyard {
		t.Fatalf("Bounty zone=%s, want graveyard (cast from an empty pool)", e.G.Obj(bounty).Zone)
	}
}

func TestBountyExileAlternativeNeedsAMatchingCard(t *testing.T) {
	t.Parallel()
	// With no green card in hand the alternative is withheld; the base cast
	// remains gated on the full mana cost (empty pool -> nothing offered).
	e := handEngine(t, corpusAlternativeCard(t, "Bounty of the Hunt"),
		card(t, "Name:Blue Fuel\nManaCost:U\nTypes:Creature\nPT:1/1\nOracle:x\n"))
	bear := battlefieldCreature(t, e, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	addMana(t, e, 0, "")
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "cast" && o.AltCostIndex != 0 {
			t.Fatalf("exile alternative offered with no green card: %+v", d.Options)
		}
	}
	// Fund the pool: the BASE cast is offered, and paying it never exiles
	// anything.
	addMana(t, e, 0, "GGGGG")
	d = e.Pending()
	base := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.AltCostIndex == 0 {
			base = o.Index
		}
	}
	if base < 0 {
		t.Fatalf("base cast not offered on a funded pool: %+v", d.Options)
	}
	submitChoices(t, e, base)
	td := e.Pending()
	submitChoices(t, e, td.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bear).Counter("P1P1") != 3 {
		t.Fatalf("bear +1/+1 counters=%d, want 3", e.G.Obj(bear).Counter("P1P1"))
	}
	if z := e.G.Zone(state.ZExile, 0); len(z) != 0 {
		t.Fatalf("exile zone=%v, want empty (the base cost never exiles)", z)
	}
}
