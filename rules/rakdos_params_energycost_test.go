package rules

// The Rakdos-params brief, gaps 2 and 3: the PayEnergy<X> and
// Return<N/Spec> cost tokens. Chthonian Nightmare is the brief's named
// corpus card: "Pay X {E}, Sacrifice a creature, Return Chthonian Nightmare
// to its owner's hand: Return target creature card with mana value X from
// your graveyard to the battlefield." Before this work ParseCost degraded
// both tokens to one generic mana apiece and neither payment ever happened;
// its ETB "you get {E}{E}{E}" also silently dropped the player counters.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// nightmareBoard builds a hand engine with Chthonian Nightmare on seat 0's
// battlefield (its own ETB trigger grants 3 energy through the real
// PutCounter player-counter path), a sac fodder creature, and a
// mana-value-2 creature card in the graveyard.
func nightmareBoard(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "Chthonian Nightmare"))
	nmCard := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: nmCard, From: state.ZHand, To: state.ZBattlefield})
	fodder := e.G.AddObject(card(t, "Name:Fodder\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: fodder.ID, From: state.ZLibrary, To: state.ZBattlefield})
	target := e.G.AddObject(card(t, "Name:Scrap Heap\nManaCost:1 B\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: target.ID, From: state.ZLibrary, To: state.ZGraveyard})
	// Drive the Nightmare's own "you get {E}{E}{E}" ETB trigger through the
	// real queue/drain (its stack entry holds sorcery speed, and the grant is
	// the real PutCounter player-counter path) and leave a fresh priority
	// decision pending.
	addMana(t, e, 0, "")
	passUntilStackEmpty(t, e, 20)
	return e, target.ID, fodder.ID
}

func nightmareID(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Chthonian Nightmare" {
			return id
		}
	}
	t.Fatal("Chthonian Nightmare not on seat 0's battlefield")
	return 0
}

func activateIndex(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id {
			return o.Index
		}
	}
	t.Fatalf("no ability option for %d: %+v", id, d.Options)
	return -1
}

func TestChthonianNightmarePaysEnergySacsAndReturns(t *testing.T) {
	t.Parallel()
	e, target, fodder := nightmareBoard(t)
	nm := nightmareID(t, e)
	if n := e.G.Players[0].Counter("ENERGY"); n != 3 {
		t.Fatalf("energy after the ETB trigger=%d, want 3 (PutCounter on a player)", n)
	}
	submitChoices(t, e, activateIndex(t, e, nm))

	// The PayEnergy<X> ask: X bounds are the payer's 3 energy; choose 2.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("PayEnergy<X> did not announce an X ask: %+v", d)
	}
	submitChoices(t, e, 2)
	// The Sac<1/Creature> ask: the fodder creature.
	d = e.Pending()
	if d == nil || len(d.Options) == 0 || d.Options[0].Kind != "sacrifice" {
		t.Fatalf("sacrifice ask missing: %+v", d)
	}
	submitChoices(t, e, 0)
	// Return<1/CARDNAME> is the source itself: no ask (the singleton rule).
	// The target ask follows: the MV-2 creature card in the graveyard.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask missing: %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == target {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no option for the MV-2 graveyard card (cmcEQX with X=2): %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)

	if n := e.G.Players[0].Counter("ENERGY"); n != 1 {
		t.Fatalf("payer energy=%d, want 1 (3 - X 2)", n)
	}
	if o := e.G.Obj(target); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("reanimated card zone/controller wrong: %+v", o)
	}
	if nmo := e.G.Obj(nm); nmo == nil || nmo.Zone != state.ZHand || nmo.Owner != 0 {
		t.Fatalf("Chthonian Nightmare zone=%+v, want hand (Return<1/CARDNAME>)", nmo)
	}
	if e.G.Obj(fodder).Zone != state.ZGraveyard {
		t.Fatal("sacrificed fodder did not reach the graveyard")
	}
}

func TestChthonianNightmareXBoundedByEnergy(t *testing.T) {
	t.Parallel()
	e, _, _ := nightmareBoard(t)
	nm := nightmareID(t, e)
	submitChoices(t, e, activateIndex(t, e, nm))
	d := e.Pending()
	if d == nil {
		t.Fatal("no X ask")
	}
	maxX := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount > maxX {
			maxX = o.Amount
		}
	}
	if maxX != 3 {
		t.Fatalf("X options max=%d, want 3 (the payer's energy total)", maxX)
	}
}

// Whirler Virtuoso is the corpus's fixed-PayEnergy shape: "Pay {3}: Create a
// 1/1 colorless Thopter artifact creature token with flying." A fixed
// PayEnergy<N> IS gated at offer time (Forge CostPayEnergy.canPay), unlike
// the X form whose value is announced later.
func TestWhirlerVirtuosoFixedEnergyCostGatesAndPays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, corpusAlternativeCard(t, "Whirler Virtuoso"))
	if thopter := reg.Tokens["c_1_1_a_thopter_flying"]; thopter != nil {
		e.G.Tokens = fixtureTokenMap(e.G.Tokens)
		setFixtureToken(e, "c_1_1_a_thopter_flying", thopter)
	}
	virt := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: virt, From: state.ZHand, To: state.ZBattlefield})
	addMana(t, e, 0, "")
	passUntilStackEmpty(t, e, 20)
	// The card's own ETB trigger granted 3 energy; drain it to zero through
	// the real player-counter event to reach the 0-energy board.
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: -3})
	addMana(t, e, 0, "")
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == virt {
			t.Fatalf("PayEnergy<3> ability offered with zero energy: %+v", o)
		}
	}
	// Grant exactly 3 energy through the real player-counter event.
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 3})
	addMana(t, e, 0, "")
	d = e.Pending()
	act := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == virt {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatalf("PayEnergy<3> ability not offered with 3 energy: %+v", d.Options)
	}
	thopterAvailable := reg.Tokens["c_1_1_a_thopter_flying"] != nil
	subTokens := len(tokenNames(t, e))
	submitChoices(t, e, act)
	passUntilStackEmpty(t, e, 20)
	if n := e.G.Players[0].Counter("ENERGY"); n != 0 {
		t.Fatalf("energy after activation=%d, want 0", n)
	}
	if got := len(tokenNames(t, e)); thopterAvailable && got != subTokens+1 {
		t.Fatalf("thopter token not created (tokens %d -> %d)", subTokens, got)
	}
}

func tokenNames(t *testing.T, e *Engine) []string {
	t.Helper()
	var out []string
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.IsToken && o.Zone == state.ZBattlefield && o.Face() != nil {
			out = append(out, o.Face().Name)
		}
	}
	return out
}
