package rules

// The Rakdos-params brief, gap 5: Sac<X/Spec> -- the announced-count
// sacrifice cost -- and the ReduceCost static that reads the paid X. Dargo,
// the Shipwrecker is the brief's named corpus card: "As an additional cost to
// cast this spell, you may sacrifice any number of artifacts and/or
// creatures. This spell costs {2} less to cast for each permanent sacrificed
// this way and {2} less to cast for each other artifact or creature you've
// sacrificed this turn."

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func dargoEngine(t *testing.T, permanents []string, mana string) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "Dargo, the Shipwrecker"))
	spell := e.G.Zone(state.ZHand, 0)[0]
	var ids []state.ObjID
	for _, src := range permanents {
		o := e.G.AddObject(card(t, src), 0)
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		ids = append(ids, o.ID)
	}
	addMana(t, e, 0, mana)
	return e, spell, ids
}

func castOption(t *testing.T, e *Engine, spell state.ObjID) int {
	t.Helper()
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			return o.Index
		}
	}
	t.Fatalf("cast option missing: %+v", d.Options)
	return -1
}

func TestDargoAnnouncesSacrificeCountAndReduces(t *testing.T) {
	t.Parallel()
	// Three sac candidates; announce X=2, sacrifice two, pay {6}{R} - {4} =
	// {2}{R}. The offer gate priced the FULL {6}{R} (no announced X yet), so
	// the pool is funded for that worst case.
	e, spell, ids := dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
		"Name:Crab\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:Boar\nTypes:Creature\nPT:2/2\nOracle:x\n",
	}, "RRRRRRR")
	submitChoices(t, e, castOption(t, e, spell))
	// The Sac<X> ask announces the count first (CR 601.2b).
	dx := e.Pending()
	if dx == nil || dx.Kind != decision.KChoose || len(dx.Options) == 0 || dx.Options[0].Kind != "x" {
		t.Fatalf("Sac<X> did not announce a count ask: %+v", dx)
	}
	maxX := -1
	for _, o := range dx.Options {
		if o.Kind == "x" && o.Amount > maxX {
			maxX = o.Amount
		}
	}
	if maxX != 3 {
		t.Fatalf("sacrifice-count options max=%d, want 3 (the candidates on the battlefield)", maxX)
	}
	submitChoices(t, e, 2)
	// The sacrifice ask: exactly X=2 of the three candidates.
	ds := e.Pending()
	if ds == nil || ds.Kind != decision.KChoose || ds.Min != 2 || ds.Max != 2 {
		t.Fatalf("sacrifice ask %+v, want Min/Max 2", ds)
	}
	chosen := []int{}
	for _, o := range ds.Options {
		if o.Obj == ids[0] || o.Obj == ids[1] {
			chosen = append(chosen, o.Index)
		}
	}
	if len(chosen) != 2 {
		t.Fatalf("sacrifice options missing the chosen pair: %+v", ds.Options)
	}
	submitChoices(t, e, chosen...)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ids[0]).Zone != state.ZGraveyard || e.G.Obj(ids[1]).Zone != state.ZGraveyard {
		t.Fatalf("sacrificed zones %s/%s, want graveyard/graveyard", e.G.Obj(ids[0]).Zone, e.G.Obj(ids[1]).Zone)
	}
	if e.G.Obj(ids[2]).Zone != state.ZBattlefield {
		t.Fatal("the unchosen permanent was swept too")
	}
	if e.G.Obj(spell).Zone != state.ZBattlefield {
		t.Fatalf("Dargo zone=%s, want battlefield (cast resolved)", e.G.Obj(spell).Zone)
	}
	// Paid {2}{R}: 7 red in the pool minus 2 pips+generic (the {2}{R} total).
	if pool := e.G.Players[0].Pool; pool.Total() != 4 || pool[state.MR] != 4 {
		t.Fatalf("pool after payment=%+v, want 4 red ({6}{R} minus reduce {4} paid {2}{R})", pool)
	}
}

func TestDargoSacrificingNothingPaysFullCost(t *testing.T) {
	t.Parallel()
	// X=0 is legal: no sacrifice ask at all, full {6}{R} paid.
	e, spell, ids := dargoEngine(t, []string{"Name:Anvil\nTypes:Artifact\nOracle:x\n"}, "RRRRRRR")
	submitChoices(t, e, castOption(t, e, spell))
	dx := e.Pending()
	if dx == nil || dx.Kind != decision.KChoose {
		t.Fatalf("count ask %+v", dx)
	}
	submitChoices(t, e, 0)
	d := e.Pending()
	if d != nil && d.Kind != decision.KPriority {
		t.Fatalf("no sacrifice ask should follow X=0, got %+v", d)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ids[0]).Zone != state.ZBattlefield {
		t.Fatal("X=0 swept the permanent anyway")
	}
	if e.G.Obj(spell).Zone != state.ZBattlefield {
		t.Fatalf("Dargo zone=%s, want battlefield", e.G.Obj(spell).Zone)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool after payment=%+v, want 0 (full {6}{R} = 7 total paid)", pool)
	}
}
