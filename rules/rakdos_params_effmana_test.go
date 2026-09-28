package rules

// The Rakdos-params brief, gap 10: effMana Amount$ X where X is the
// sacrificed card's mana value (Burnt Offering: "Add X mana in any
// combination of {B} and/or {R}, where X is the sacrificed creature's mana
// value"). The amount resolves through Num's SVar fallback -- SVar:X is
// Sacrificed$CardManaCost, which reads the LKI snapshot of what the cost
// sacrificed (Ctx.Sacrificed) -- so the amount and its all-black Combo
// allocation are pinned here on the real corpus card.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func burntOfferingEngine(t *testing.T, creatureSrc string) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "Burnt Offering"))
	spells := e.G.Zone(state.ZHand, 0)
	spell := spells[len(spells)-1]
	o := e.G.AddObject(card(t, creatureSrc), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	// The spell's {B} cost pip, plus a float to observe the added mana by.
	addMana(t, e, 0, "BB")
	return e, spell, o.ID
}

func TestBurntOfferingAmountIsTheSacrificedCreatureManaValue(t *testing.T) {
	t.Parallel()
	// A 4-drop creature ({1}{R}{R}{G}): the ability adds X = 4.
	e, spell, creature := burntOfferingEngine(t,
		"Name:Big Boar\nManaCost:1 R R G\nTypes:Creature Boar\nPT:3/3\nOracle:x\n")
	// The SA is the SPELL (SP$ Mana): casting it for {B} plus the sacrifice
	// IS the activation.
	d := e.Pending()
	opt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			opt = o.Index
		}
	}
	if opt < 0 {
		t.Fatalf("Burnt Offering not castable: %+v", d.Options)
	}
	submitChoices(t, e, opt)
	ds := e.Pending()
	if ds == nil || ds.Kind != decision.KChoose || ds.Options[0].Kind != "sacrifice" || ds.Options[0].Obj != creature {
		t.Fatalf("sacrifice ask %+v (creature %d)", ds, creature)
	}
	submitChoices(t, e, ds.Options[0].Index)
	colour := passUntilNonPriority(t, e, 20)
	if colour.Kind != decision.KChoose || colour.ResumeKind != "mana_color" || colour.Min != 4 || colour.Max != 4 {
		t.Fatalf("Burnt Offering did not ask for its four-unit Combo allocation: %+v", colour)
	}
	// The colour list repeats B/R once per unit. Choose every B option to
	// isolate the sacrificed-card amount while still submitting a legal
	// allocation.
	var black []int
	for _, option := range colour.Options {
		if option.Label == "Add B" {
			black = append(black, option.Index)
		}
	}
	if len(black) != 4 {
		t.Fatalf("Burnt Offering black allocation options = %+v, want four", colour.Options)
	}
	submitChoices(t, e, black...)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(creature).Zone != state.ZGraveyard {
		t.Fatalf("creature zone=%s, want graveyard (the cost)", e.G.Obj(creature).Zone)
	}
	pool := e.G.Players[0].Pool
	// Choose B, then X = 4 is added to the one black left after paying the
	// spell, for five black total and no red.
	if pool[state.MB] != 5 || pool[state.MR] != 0 {
		t.Fatalf("pool=%+v, want 5 black and no red -- X = the sacrificed creature's mana value 4", pool)
	}
}

func TestBurntOfferingAmountTracksAOneDrop(t *testing.T) {
	t.Parallel()
	// A 1-drop creature: X = 1, so 1 of each listed colour.
	e, spell, creature := burntOfferingEngine(t,
		"Name:Small Boar\nManaCost:R\nTypes:Creature Boar\nPT:1/1\nOracle:x\n")
	_ = creature
	d := e.Pending()
	opt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			opt = o.Index
		}
	}
	submitChoices(t, e, opt)
	ds := e.Pending()
	submitChoices(t, e, ds.Options[0].Index)
	colour := passUntilNonPriority(t, e, 20)
	if colour.Kind != decision.KChoose || colour.ResumeKind != "mana_color" {
		t.Fatalf("Burnt Offering did not ask for its Combo colour: %+v", colour)
	}
	submitChoices(t, e, colour.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	pool := e.G.Players[0].Pool
	if pool[state.MB] != 2 || pool[state.MR] != 0 {
		t.Fatalf("pool=%+v, want 2 black and no red -- X = the sacrificed creature's mana value 1", pool)
	}
}
