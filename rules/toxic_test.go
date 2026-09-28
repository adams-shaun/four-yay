package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestToxicIxhelAddsPoisonOnCombatDamage pins CR 702.164 on the REAL corpus
// card Ixhel, Scion of Atraxa (K:Toxic:2): a player dealt combat damage by a
// source with toxic N also gets N poison counters. Ixhel is a 2/5, so an
// unblocked hit deals 2 damage AND adds exactly 2 poison -- the "also" the
// rule states. The poison counters are the player's (PlayerCounterChange), so
// the assertion reads Players[1].Counter("POISON"), not any creature state.
func TestToxicIxhelAddsPoisonOnCombatDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	ixhel := onBoardCard(t, e, 0, corpusKeywordCard(t, "Ixhel, Scion of Atraxa"))
	// PRECONDITION: the source's toxic is readable off the real K:Toxic:2
	// line and the creature is a live attacker. A zero here means the
	// keyword read failed and the test would prove nothing.
	if got := e.ToxicValue(ixhel); got != 2 {
		t.Fatalf("Ixhel toxic value = %d, want 2 (printed K:Toxic:2)", got)
	}
	if e.Power(ixhel) != 2 {
		t.Fatalf("Ixhel power = %d, want 2", e.Power(ixhel))
	}
	e.G.Obj(ixhel).IsAttacking = true
	e.G.Obj(ixhel).Attacking = 1
	e.G.Obj(ixhel).SummonSick = false

	lifeBefore := e.G.Players[1].Life
	e.dealCombatDamage()

	if got := e.G.Players[1].Life; got != lifeBefore-2 {
		t.Fatalf("defender life = %d, want %d (toxic must not change the combat damage)", got, lifeBefore-2)
	}
	if got := e.G.Players[1].Counter("POISON"); got != 2 {
		t.Fatalf("defender poison = %d, want 2 (Ixhel's toxic 2)", got)
	}
}

// TestToxicTenthCounterLosesTheGame pins that toxic poison feeds the existing
// CR 704.5b state-based action: a player who reaches ten poison counters from
// a toxic hit loses. Starting from nine, Ixhel's toxic 2 crosses the
// threshold, and the SBA runs as part of the same combat damage sequence.
func TestToxicTenthCounterLosesTheGame(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	ixhel := onBoardCard(t, e, 0, corpusKeywordCard(t, "Ixhel, Scion of Atraxa"))
	if got := e.ToxicValue(ixhel); got != 2 {
		t.Fatalf("Ixhel toxic value = %d, want 2 (printed K:Toxic:2)", got)
	}
	// PRECONDITION: the defender starts one hit short of the threshold, so
	// the toxic hit is what crosses it (not an already-lost player).
	e.G.Players[1].AddCounter("POISON", 9)
	if lost := e.G.Players[1].Lost; lost {
		t.Fatal("defender already lost before the toxic hit")
	}
	e.G.Obj(ixhel).IsAttacking = true
	e.G.Obj(ixhel).Attacking = 1
	e.G.Obj(ixhel).SummonSick = false

	e.dealCombatDamage()
	e.checkStateBased()

	if got := e.G.Players[1].Counter("POISON"); got != 11 {
		t.Fatalf("defender poison = %d, want 11 (9 + toxic 2)", got)
	}
	if !e.G.Players[1].Lost {
		t.Fatal("player at eleven poison counters did not lose per CR 704.5b")
	}
}

// TestToxicDoesNotPoisonOnCreatureDamage pins the player-only half of
// CR 702.164: a toxic creature dealing combat damage to a CREATURE (a blocked
// attacker hitting its blocker) poisons nobody and its damage is unchanged.
// This is the discriminator against infect, which would also change the
// creature damage to -1/-1 counters.
func TestToxicDoesNotPoisonOnCreatureDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	ixhel := onBoardCard(t, e, 0, corpusKeywordCard(t, "Ixhel, Scion of Atraxa"))
	if got := e.ToxicValue(ixhel); got != 2 {
		t.Fatalf("Ixhel toxic value = %d, want 2 (printed K:Toxic:2)", got)
	}
	blocker := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/5\nOracle:x\n")
	if e.Toughness(blocker) != 5 {
		t.Fatalf("blocker toughness = %d, want 5", e.Toughness(blocker))
	}
	e.G.Obj(ixhel).IsAttacking = true
	e.G.Obj(ixhel).Attacking = 1
	e.G.Obj(ixhel).SummonSick = false
	e.G.Obj(ixhel).BlockedBy = []state.ObjID{blocker}

	lifeBefore := e.G.Players[1].Life
	e.dealCombatDamage()

	if got := e.G.Obj(blocker).Damage; got != 2 {
		t.Fatalf("blocker damage = %d, want 2 (toxic must NOT change creature damage)", got)
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("defender poison = %d, want 0 (toxic is player-only)", got)
	}
	if got := e.G.Players[1].Life; got != lifeBefore {
		t.Fatalf("defender life = %d, want %d (no damage reached the player)", got, lifeBefore)
	}
}

// TestToxicGrantedByLayerCountsToo pins the derived-keyword read: a layer-6
// `AddKeyword$ Toxic:1` grant -- the real corpus Equipment Prosthetic
// Injector (`Affected$ Creature.EquippedBy | AddKeyword$ Toxic:1`) -- poisons
// on combat damage exactly like a printed K:Toxic line. Reading only the
// printed face would miss it, so this test would fail against a face-only
// implementation while the printed-toxic tests above would still pass.
func TestToxicGrantedByLayerCountsToo(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// PRECONDITION: the plain bearer has no toxic until the grant lands, so
	// a zero after the attach would mean the layer read (not the fixture)
	// failed.
	if got := e.ToxicValue(bear); got != 0 {
		t.Fatalf("plain bear toxic value = %d, want 0 before the grant", got)
	}
	injector := onBoardCard(t, e, 0, corpusKeywordCard(t, "Prosthetic Injector"))
	if e.G.Obj(injector).Zone != state.ZBattlefield {
		t.Fatal("Prosthetic Injector not on the battlefield")
	}
	e.G.Obj(injector).AttachedTo = bear

	if got := e.ToxicValue(bear); got != 1 {
		t.Fatalf("equipped bear toxic = %d, want 1 (Prosthetic Injector's granted toxic 1)", got)
	}
	e.G.Obj(bear).IsAttacking = true
	e.G.Obj(bear).Attacking = 1
	e.G.Obj(bear).SummonSick = false
	e.dealCombatDamage()

	if got := e.G.Players[1].Counter("POISON"); got != 1 {
		t.Fatalf("granted-toxic defender poison = %d, want 1", got)
	}
}

// TestToxicInstancesCumulate pins CR 702.164c on real corpus cards: toxic
// instances are cumulative, so printed Toxic 2 (Ixhel, Scion of Atraxa)
// equipped by Prosthetic Injector's AddKeyword$ Toxic:1 grant is toxic 3 --
// the read must SUM every derived Toxic entry, not stop at the first. A
// first-entry read reports 2 and this test fails while the single-instance
// tests above still pass.
func TestToxicInstancesCumulate(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	ixhel := onBoardCard(t, e, 0, corpusKeywordCard(t, "Ixhel, Scion of Atraxa"))
	if got := e.ToxicValue(ixhel); got != 2 {
		t.Fatalf("Ixhel toxic value = %d, want 2 (printed K:Toxic:2)", got)
	}
	injector := onBoardCard(t, e, 0, corpusKeywordCard(t, "Prosthetic Injector"))
	if e.G.Obj(injector).Zone != state.ZBattlefield {
		t.Fatal("Prosthetic Injector not on the battlefield")
	}
	e.G.Obj(injector).AttachedTo = ixhel
	if got := e.ToxicValue(ixhel); got != 3 {
		t.Fatalf("Ixhel + Injector toxic = %d, want 3 (CR 702.164c cumulative)", got)
	}
	e.G.Obj(ixhel).IsAttacking = true
	e.G.Obj(ixhel).Attacking = 1
	e.G.Obj(ixhel).SummonSick = false
	e.dealCombatDamage()
	if got := e.G.Players[1].Counter("POISON"); got != 3 {
		t.Fatalf("defender poison = %d, want 3 (toxic 2 printed + toxic 1 granted)", got)
	}
}

// TestToxicTwoGrantsCumulate pins the grant side alone: a plain creature
// carrying BOTH the Equipment grant (Prosthetic Injector, toxic 1) and the
// Aura grant (Necrogen Communion, AddKeyword$ Toxic:2) is toxic 3, proving
// the sum walks past a single granted instance too.
func TestToxicTwoGrantsCumulate(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if got := e.ToxicValue(bear); got != 0 {
		t.Fatalf("plain bear toxic value = %d, want 0 before any grant", got)
	}
	injector := onBoardCard(t, e, 0, corpusKeywordCard(t, "Prosthetic Injector"))
	e.G.Obj(injector).AttachedTo = bear
	communion := onBoardCard(t, e, 0, corpusKeywordCard(t, "Necrogen Communion"))
	e.G.Obj(communion).AttachedTo = bear
	if got := e.ToxicValue(bear); got != 3 {
		t.Fatalf("bear + Injector + Communion toxic = %d, want 3 (two grants sum)", got)
	}
}

// TestToxicSurvivesParkedReplacement pins the parked CR 616.1 path: a
// player-targeted combat hit from a toxic source whose replacement
// competition is POSED (Battletide Alchemist's optional "you may prevent X"
// ask, a real corpus carrier) never reaches runCombatAssignments'
// synchronous toxic emit, so the landed event must place the cached poison
// in finishChosenDamage. Both arms are pinned: the DECLINE arm (the ask is
// answered "do not apply") and the APPLY arm (0 Clerics prevents 0, damage
// still lands) -- each was a 0-poison defect before the replChoice toxic
// rider existed.
func TestToxicSurvivesParkedReplacement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deck0 := mountainDeck(t, 40)
	ix, ok := reg.Lookup("Ixhel, Scion of Atraxa")
	if !ok {
		t.Fatalf("corpus fixture: Ixhel, Scion of Atraxa missing")
	}
	deck0 = append(deck0, ix)
	deck1 := mountainDeck(t, 41)
	batt, ok := reg.Lookup("Battletide Alchemist")
	if !ok {
		t.Fatalf("corpus fixture: Battletide Alchemist missing")
	}
	deck1 = append(deck1, batt)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck0, deck1}}))
	e.Advance()
	ixhel := crAbortMove(t, e, 0, "Ixhel, Scion of Atraxa", state.ZBattlefield)
	battletide := crAbortMove(t, e, 1, "Battletide Alchemist", state.ZBattlefield)
	_ = battletide
	e.pending = nil
	if got := e.ToxicValue(ixhel); got != 2 {
		t.Fatalf("Ixhel toxic value = %d, want 2", got)
	}
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("defender life = %d, want 20 before any damage", life)
	}

	// The assignment parks on the optional prevention ask (posed to seat 1,
	// Battletide's controller) and poison is 0 until the answer lands it.
	e.combatRound.assignments = []assignment{{toPlayer: 1, amount: 2, from: ixhel}}
	e.runCombatAssignments()
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 1 {
		t.Fatalf("pending = %+v, want Battletide's optional-prevention ask to seat 1", d)
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("defender poison = %d before the answer, want 0 (event still parked)", got)
	}

	// DECLINE: the damage lands through finishChosenDamage -- life drops 2
	// AND Ixhel's toxic 2 places 2 poison.
	submitChoices(t, e, len(d.Options)-1)
	e.pending = nil
	e.pendingTriggers = nil
	if got := e.G.Players[1].Counter("POISON"); got != 2 {
		t.Fatalf("poison after declined parked replacement = %d, want 2", got)
	}

	// APPLY arm on a fresh turn: choosing the 0-Cleric prevention prevents 0,
	// the damage still lands through the same finishChosenDamage path, and
	// the toxic rider must fire there too.
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	e.pending = nil
	e.pendingTriggers = nil
	e.combatRound.assignments = []assignment{{toPlayer: 1, amount: 2, from: ixhel}}
	e.runCombatAssignments()
	d = e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("pending = %+v, want the optional-prevention ask again", d)
	}
	submitChoices(t, e, 0)
	e.pendingTriggers = nil
	if got := e.G.Players[1].Counter("POISON"); got != 4 {
		t.Fatalf("poison after applied parked replacement = %d, want 4 (2 + 2 on the fresh turn)", got)
	}
}
