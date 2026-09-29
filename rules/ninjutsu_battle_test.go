package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestNinjutsuPreservesPlaneswalkerDefenderThroughEntry pins CR 702.49b's
// non-player defender branch for K:Ninjutsu: the returned attacker was
// declared at a battlefield PLANESWALKER, so the ninjutsu permanent must
// enter attacking that same object, not merely the planeswalker's
// controller's seat. The object rides pendingCast.ninjutsuDefenderObject ->
// the AbilityPush event's IDs -> the resolving Ctx's DefendingBattle ->
// effects/zone.go's Attacking$ True rider -> the TokenAttacks IDs[1] slot.
func TestNinjutsuPreservesPlaneswalkerDefenderThroughEntry(t *testing.T) {
	if !effects.Supported()["kw:Ninjutsu"] {
		t.Fatal("kw:Ninjutsu is not registered; the coverage ratchet would still report it")
	}
	reg := searchTestRegistry(t)
	ninja := searchCorpusCard(t, reg, "Walker of Secret Ways")
	if d := ninja.Link(); len(d) != 0 {
		t.Fatalf("link Walker of Secret Ways: %v", d)
	}
	if !ninja.Faces[0].HasKeyword("Ninjutsu") {
		t.Fatal("precondition: Walker of Secret Ways does not print Ninjutsu in the corpus")
	}

	e, _ := ninjutsuDeck(t, 9403, ninja)
	// A planeswalker on seat 1 (the defender). It needs loyalty or the SBA
	// (CR 704.5i) removes it before attackOffers can offer it.
	pw := onBoard(t, e, 1, combatPlaneswalker)
	e.emit(events.Event{Kind: events.CounterChange, Obj: pw, Counter: "LOYALTY", Amount: 5})
	if o := e.G.Obj(pw); o == nil || o.Zone != state.ZBattlefield || o.Counter("LOYALTY") <= 0 {
		t.Fatalf("planeswalker precondition: %+v", o)
	}

	ninjaID := searchMoveByName(t, e, "Walker of Secret Ways", state.ZHand)
	if o := e.G.Obj(ninjaID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Walker of Secret Ways not in hand: %+v", o)
	}

	// Declare the inline Bear at the planeswalker, through the real
	// KAttackers options (opt.Obj == bear && opt.Battle == pw).
	bear := putCreature(t, e, 0, ninjutsuBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Bear not on the battlefield: %+v", o)
	}
	e.priorityRound() // moveSeeded clears the pending ask; re-pose priority
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("declare attackers decision = %+v", d)
	}
	var chosen []decision.Option
	for _, opt := range d.Options {
		if opt.Obj == bear && opt.Battle == pw {
			chosen = append(chosen, opt)
		}
	}
	if len(chosen) != 1 {
		t.Fatalf("planeswalker attack option count = %d, want 1 (options: %+v)", len(chosen), d.Options)
	}
	e.finishAttackers(chosen, 0)
	driveToBlockersPriority(t, e, 0)
	if e.G.Step != state.StepDeclareBlockers || e.G.Active != 0 {
		t.Fatalf("expected seat 0's declare-blockers step, got step %s active %d", e.G.Step, e.G.Active)
	}
	// Precondition: the Bear is an UNBLOCKED attacker of the planeswalker.
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking || o.Attacking != 1 || o.AttackingBattle != pw || len(o.BlockedBy) != 0 {
		t.Fatalf("precondition: Bear should be an unblocked attacker of planeswalker %d, got %+v", pw, o)
	}

	fundPool(t, e, "CU") // Walker's ninjutsu cost is {1}{U}
	opt := abilityFor(t, e, 0, ninjaID)
	if opt == nil {
		t.Fatalf("ninjutsu ability not offered at the declare-blockers step: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "returncost" {
		t.Fatalf("ninjutsu did not ask to return an unblocked attacker: %+v", d)
	}
	returned := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			returned = o.Index
		}
	}
	if returned < 0 {
		t.Fatalf("ninjutsu return ask did not offer the unblocked Bear: %+v", d.Options)
	}
	submitChoices(t, e, returned)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the returned Bear is in %v, want its owner's hand", o)
	}
	got := e.G.Obj(ninjaID)
	if got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("Walker of Secret Ways is in %v, want the battlefield", got)
	}
	if !got.Tapped {
		t.Error("Walker of Secret Ways entered untapped, want tapped (CR 702.49a)")
	}
	if !got.IsAttacking || got.Attacking != 1 {
		t.Errorf("Walker of Secret Ways attacking=%v defender=%d, want attacking seat 1", got.IsAttacking, got.Attacking)
	}
	// The distinct assertion: the entrant attacks the planeswalker object,
	// not merely its controller's seat. Without the object carried through,
	// AttackingBattle is 0 and this fails.
	if got.AttackingBattle != pw {
		t.Errorf("Walker of Secret Ways AttackingBattle=%d, want the planeswalker %d", got.AttackingBattle, pw)
	}
}
