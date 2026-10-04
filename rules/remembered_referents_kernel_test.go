package rules

// Kernel-era restorations of the tests W3 removed from remembered_referents_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestMyrkulsEdictChosenGreatestPowerIsSacrificed drives Myrkul's Edict's
// mode-20 chain on its compiled SVar: RepeatEach per opponent binds the
// opponent as Remembered, DBChooseCard offers ONLY the creatures with the
// greatest power among that player's (greatestPowerControlledByRemembered --
// ties offered, a lesser one never offered), RememberChosen$ binds the
// answer, and DBSacAll's SacrificeAll | ValidCards$ Card.IsRemembered
// sacrifices exactly the chosen card.
func TestMyrkulsEdictChosenGreatestPowerIsSacrificedKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	edict := mustCorpusCard(t, reg, "Myrkul's Edict")
	big := mustCorpusCard(t, reg, "Hill Giant")
	small := mustCorpusCard(t, reg, "Grizzly Bears")
	e := New(Config{Seed: 11, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(edict, 0)
	bigObj := e.G.AddObject(big, 1)
	smallObj := e.G.AddObject(small, 1)
	for _, id := range []state.ObjID{src.ID, bigObj.ID, smallObj.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	// The mode-20 chain head: RepeatEach per opponent binds the opponent as
	// Remembered, poses the DBChooseCard ask per opponent, and chains
	// DBSacAll off the loop's SubAbility$ -- so the whole SVar must run, not
	// DBChooseCard alone (the sacrifice rider lives on SacTopPower, not on
	// DBChooseCard, which carries no SubAbility$).
	sa := cards.ResolveSVar(src.Face().SVars, "SacTopPower")
	if sa == nil {
		t.Fatal("Myrkul's Edict has no SacTopPower SVar")
	}
	// The whole chain runs under a kernel probe: the ChooseCard ask inside
	// the RepeatEach poses, and the answering Submit re-executes the chain
	// from its checkpoint with the answer served, through DBSacAll.
	e.probe(func() {
		effects.Resolve(e, &effects.Ctx{Source: src.ID, Controller: 0, SVars: src.Face().SVars}, sa)
	})

	// The choice ask went to the remembered player, and the pool holds ONLY
	// the greatest-power creature: the 3-power Hill Giant, never the 2-power
	// Bear (a lesser creature is not "the greatest").
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the ChooseCard ask, got %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("chooser = seat %d, want the remembered player seat 1", d.Player)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bigObj.ID {
		t.Fatalf("greatest-power pool = %+v, want only the Hill Giant", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)

	// The follow-up SacrificeAll Card.IsRemembered hit exactly the chosen
	// card: Hill Giant in the graveyard, Bear untouched on the battlefield.
	if z := e.G.Obj(bigObj.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("chosen greatest-power creature zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(smallObj.ID).Zone; z != state.ZBattlefield {
		t.Fatalf("the lesser creature must be untouched, zone %s", z)
	}
}

// TestMyrkulsEdictGreatestPowerTiesAreAllOffered pins the tie half of Forge's
// greatestPower: every creature at the maximum power matches, so two 3-power
// creatures are BOTH offered and the answer picks one of them.
func TestMyrkulsEdictGreatestPowerTiesAreAllOfferedKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	edict := mustCorpusCard(t, reg, "Myrkul's Edict")
	giant := mustCorpusCard(t, reg, "Hill Giant")
	e := New(Config{Seed: 12, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(edict, 0)
	first := e.G.AddObject(giant, 1)
	second := e.G.AddObject(giant, 1)
	for _, id := range []state.ObjID{src.ID, first.ID, second.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	sa := cards.ResolveSVar(src.Face().SVars, "SacTopPower")
	if sa == nil {
		t.Fatal("Myrkul's Edict has no SacTopPower SVar")
	}
	// The whole chain runs under a kernel probe: the ChooseCard ask inside
	// the RepeatEach poses, and the answering Submit re-executes the chain
	// from its checkpoint with the answer served, through DBSacAll.
	e.probe(func() {
		effects.Resolve(e, &effects.Ctx{Source: src.ID, Controller: 0, SVars: src.Face().SVars}, sa)
	})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the ChooseCard ask, got %+v", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("two tied greatest-power creatures must both be offered, got %+v", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(d.Options[0].Obj).Zone != state.ZGraveyard {
		t.Fatal("the answered greatest-power creature must be sacrificed")
	}
	other := d.Options[1].Obj
	if e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("the un-chosen tied creature must survive")
	}
}
