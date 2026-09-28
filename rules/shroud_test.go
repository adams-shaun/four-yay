package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task kw-shroud1: K:Shroud arrived in the derived keyword set but NO rule
// ever read it, so the CR 702.14 targeting gate was entirely absent — a
// shrouded permanent (printed, or granted by Lightning Greaves) was offered
// to every targeting spell or ability, its controller's included. The fix
// wires e.shroudBlocksTarget into the two targeting gates, candidatesFor
// (the offer census every cast/ability/trigger ask routes through) and
// legalTargets (the CR 608.2b resolution recheck), behind the same
// battlefield-zone gate protection and CantTarget use.

const shockScript = "Name:Shock\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2\nOracle:x\n"

// addShockToHand puts a Shock object into seat p's hand eventlessly (the
// handEngine decks are Mountains; no Shock exists to find) and returns its id.
func addShockToHand(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	sh := e.G.AddObject(card(t, shockScript), p)
	sh.Zone = state.ZHand
	e.G.SetZone(state.ZHand, p, append(e.G.Zone(state.ZHand, p), sh.ID))
	return sh.ID
}

// TestPrintedShroudWithholdsCastWhenSoleTarget pins leaf 1: a printed
// K:Shroud creature is not a legal target of ANY spell or ability, so an
// opposing Shock with no other legal target is not even OFFERED (the CR
// 601.2c cast-withhold arm), and the SHROUD CARD'S OWN CONTROLLER is
// withheld identically — shroud is symmetric, unlike hexproof.
func TestPrintedShroudWithholdsCastWhenSoleTarget(t *testing.T) {
	t.Parallel()
	shrouded := "Name:Shrouded Elf\nManaCost:G\nTypes:Creature Elf\nK:Shroud\nPT:2/2\nOracle:x\n"
	e := handEngine(t)
	onBoard(t, e, 1, shrouded)
	if !e.shroudBlocksTarget(e.G.Zone(state.ZBattlefield, 1)[0]) {
		t.Fatalf("printed K:Shroud did not reach the derived keyword set: %v", e.Keywords(e.G.Zone(state.ZBattlefield, 1)[0]))
	}

	// The opponent (seat 0): Shock is offered for no legal target — withheld.
	sh0 := addShockToHand(t, e, 0)
	addMana(t, e, 0, "R")
	e.askPriority(0)
	if d := e.Pending(); castOffered(e, sh0) {
		t.Fatalf("opponent's Shock offered although its only creature target has shroud: %+v", d.Options)
	}

	// The controller (seat 1): shroud binds its own spells too.
	sh1 := addShockToHand(t, e, 1)
	addMana(t, e, 1, "R")
	e.askPriority(1)
	if d := e.Pending(); castOffered(e, sh1) {
		t.Fatalf("controller's own Shock offered against its own shrouded creature: %+v", d.Options)
	}
}

// TestPrintedShroudExcludedFromTargetOptions pins the offer-level bite with
// a non-shrouded control creature on the board: the cast is offered (a legal
// target exists) but the target decision offers the plain Elf and never the
// shrouded one — for the opponent AND for the shroud card's own controller.
func TestPrintedShroudExcludedFromTargetOptions(t *testing.T) {
	t.Parallel()
	shrouded := "Name:Shrouded Elf\nManaCost:G\nTypes:Creature Elf\nK:Shroud\nPT:2/2\nOracle:x\n"
	e := handEngine(t)
	shroudedID := onBoard(t, e, 1, shrouded)
	plainID := onBoard(t, e, 1, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:2/4\nOracle:x\n")

	sh0 := addShockToHand(t, e, 0)
	addMana(t, e, 0, "R")
	e.askPriority(0)
	if d := e.Pending(); !castOffered(e, sh0) {
		t.Fatalf("Shock not offered with a legal (non-shrouded) target present: %+v", d.Options)
	}
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Shock target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == shroudedID {
			t.Errorf("shrouded creature offered as Shock target to the opponent")
		}
	}
	foundPlain := false
	for _, o := range d.Options {
		if o.Obj == plainID {
			foundPlain = true
		}
	}
	if !foundPlain {
		t.Errorf("plain control creature not offered — the gate over-bit: %+v", d.Options)
	}
	targetObject(t, e, plainID)
	passUntilStackEmpty(t, e, 8)
	if e.G.Obj(shroudedID).Zone != state.ZBattlefield {
		t.Fatal("shrouded creature left the battlefield")
	}

	// The controller's own Shock sees the same exclusion.
	sh1 := addShockToHand(t, e, 1)
	addMana(t, e, 1, "R")
	e.askPriority(1)
	if d := e.Pending(); !castOffered(e, sh1) {
		t.Fatalf("controller's Shock not offered with a legal target present: %+v", d.Options)
	}
	castFirst(t, e, "cast")
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected controller's Shock target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == shroudedID {
			t.Errorf("controller's own shrouded creature offered as its Shock target")
		}
	}
}

// TestLightningGreavesGrantedShroudBlocksTargeting pins leaf 2 on a REAL
// corpus card: Lightning Greaves' "Haste & Shroud" static. Seat 0's Grizzly
// Bears is targetable by seat 1's Shock BEFORE the equip (engine A resolves
// one and the bear dies), and after the equip the bearer is offered to
// NEITHER seat's Shock (engine B).
func TestLightningGreavesGrantedShroudBlocksTargeting(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Engine A — pre-equip attribution: the bear is targetable and dies.
	eA, _ := linkBoard(t, reg, []string{"Lightning Greaves", "Grizzly Bears"}, []string{"Hill Giant"})
	bearA := findOnBoard(t, eA, 0, "Grizzly Bears")
	shA := eA.G.AddObject(mustCorpusCard(t, reg, "Shock"), 1)
	shA.Zone = state.ZHand
	eA.G.SetZone(state.ZHand, 1, append(eA.G.Zone(state.ZHand, 1), shA.ID))
	addMana(t, eA, 1, "R")
	eA.askPriority(1)
	if !castOffered(eA, shA.ID) {
		t.Fatalf("engine A: Shock not offered at the unequipped bear: %+v", eA.Pending().Options)
	}
	castFirst(t, eA, "cast")
	dA := eA.Pending()
	if dA == nil || dA.Kind != decision.KTarget {
		t.Fatalf("engine A: expected Shock target decision, got %+v", dA)
	}
	targetObject(t, eA, bearA)
	passUntilStackEmpty(t, eA, 20)
	if eA.G.Obj(bearA).Zone == state.ZBattlefield {
		t.Fatal("engine A: bear survived a resolved Shock — pre-equip targetability not proven")
	}

	// Engine B — equip first, then the same Shock finds no bear target.
	eB, _ := linkBoard(t, reg, []string{"Lightning Greaves", "Grizzly Bears"}, []string{"Hill Giant"})
	eq := findOnBoard(t, eB, 0, "Lightning Greaves")
	bear := findOnBoard(t, eB, 0, "Grizzly Bears")
	giant := findOnBoard(t, eB, 1, "Hill Giant")
	equipGrants(t, eB, eq, bear, "")
	assertGrants(t, eB, bear, []string{"Shroud"})
	if !eB.shroudBlocksTarget(bear) {
		t.Fatal("engine B: granted Shroud not visible to the targeting gate helper")
	}

	shB := eB.G.AddObject(mustCorpusCard(t, reg, "Shock"), 1)
	shB.Zone = state.ZHand
	eB.G.SetZone(state.ZHand, 1, append(eB.G.Zone(state.ZHand, 1), shB.ID))
	addMana(t, eB, 1, "R")
	eB.askPriority(1)
	if !castOffered(eB, shB.ID) {
		t.Fatalf("engine B: Shock not offered although Hill Giant is a legal target: %+v", eB.Pending().Options)
	}
	castFirst(t, eB, "cast")
	dB := eB.Pending()
	if dB == nil || dB.Kind != decision.KTarget {
		t.Fatalf("engine B: expected Shock target decision, got %+v", dB)
	}
	for _, o := range dB.Options {
		if o.Obj == bear {
			t.Errorf("engine B: equipped bearer offered to the opponent's Shock")
		}
	}
	targetObject(t, eB, giant)
	passUntilStackEmpty(t, eB, 20)
	if eB.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("engine B: bearer left the battlefield")
	}

	// Symmetry: seat 0 (the bearer's own controller) cannot target it either.
	sh0 := eB.G.AddObject(mustCorpusCard(t, reg, "Shock"), 0)
	sh0.Zone = state.ZHand
	eB.G.SetZone(state.ZHand, 0, append(eB.G.Zone(state.ZHand, 0), sh0.ID))
	addMana(t, eB, 0, "R")
	eB.askPriority(0)
	castFirst(t, eB, "cast")
	d := eB.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("engine B: expected controller's Shock target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == bear {
			t.Errorf("engine B: bearer offered to its OWN controller's Shock")
		}
	}
}

// TestShroudRecheckDropsTargetChosenBeforeTheGrant pins leaf 3: a target
// chosen while the bearer was legal, then shroud granted before resolution —
// the CR 608.2b recheck drops the target and the Shock fizzles (its only
// chosen target is illegal; the recheck never retargets). The grant rides an
// instant-speed KW$ Shroud pump (a real engine grant path —
// effects/combatfx.go's registerPumpEffects), because equip is sorcery-speed
// and cannot be activated while the Shock is on the stack.
func TestShroudRecheckDropsTargetChosenBeforeTheGrant(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Grizzly Bears"}, []string{"Hill Giant"})
	bear := findOnBoard(t, e, 0, "Grizzly Bears")
	giant := findOnBoard(t, e, 1, "Hill Giant")

	sh := e.G.AddObject(mustCorpusCard(t, reg, "Shock"), 1)
	sh.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), sh.ID))
	addMana(t, e, 1, "R")
	e.askPriority(1)
	castFirst(t, e, "cast")
	targetObject(t, e, bear) // bear is legal at announcement — Shock on the stack

	// In response, seat 0 casts an instant pump granting KW$ Shroud.
	pump := e.G.AddObject(card(t, "Name:Shrouding Blessing\nManaCost:G\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | KW$ Shroud\nOracle:x\n"), 0)
	pump.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), pump.ID))
	addMana(t, e, 0, "G")
	e.askPriority(0)
	castFirst(t, e, "cast")
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)

	if !e.shroudBlocksTarget(bear) {
		t.Fatalf("pump's KW$ Shroud grant did not reach the bearer's derived keywords: %v", e.Keywords(bear))
	}
	if e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("Shock resolved onto a target that gained shroud before resolution")
	}
	if e.G.Obj(giant).Zone != state.ZBattlefield {
		t.Fatal("Hill Giant unexpectedly affected")
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not empty after the fizzle: %v", e.G.Stack)
	}
}

// TestAffectedCensusIgnoresShroud pins leaf 4: candidatesFor's targeting
// flag is load-bearing — the Overload-style affected census
// (affectedCandidates, targeting=false) still admits a shrouded permanent,
// while the same spec through legalTargetCandidates (targeting=true)
// withholds it.
func TestAffectedCensusIgnoresShroud(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	shroudedID := onBoard(t, e, 1, "Name:Shrouded Elf\nManaCost:G\nTypes:Creature Elf\nK:Shroud\nPT:2/2\nOracle:x\n")
	sa := card(t, "Name:Overload shape\nManaCost:2 R\nTypes:Instant\n"+
		"A:SP$ DamageAll | ValidTgts$ Creature | NumDmg$ 2\nOracle:x\n").Faces[0].Abilities[0]

	affected := e.affectedCandidates(0, 0, 0, sa)
	sawAffected := false
	for _, c := range affected {
		if c.obj == shroudedID {
			sawAffected = true
		}
	}
	if !sawAffected {
		t.Errorf("affected census (targeting=false) withheld the shrouded permanent: %+v", affected)
	}

	targeting := e.legalTargetCandidates(0, 0, 0, sa)
	for _, c := range targeting {
		if c.obj == shroudedID {
			t.Errorf("target census (targeting=true) offered the shrouded permanent: %+v", targeting)
		}
	}
}

// TestShroudDoesNotBlockNonBattlefieldZones pins the CR 604.3/702.14a zone
// gate: a shroud keyword on a card NOT on the battlefield (a K:Shroud face in
// a TgtZone$ Graveyard spec) stays targetable — shroud functions only while
// the object is a battlefield permanent.
func TestShroudDoesNotBlockNonBattlefieldZones(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	cardScript := "Name:Shrouded Revenant\nManaCost:2 B\nTypes:Creature Zombie\nK:Shroud\nPT:2/2\nOracle:x\n"
	o := e.G.AddObject(card(t, cardScript), 1)
	o.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 1, append(e.G.Zone(state.ZGraveyard, 1), o.ID))

	sa := card(t, "Name:Reanimate shape\nManaCost:B\nTypes:Sorcery\n"+
		"A:SP$ ChangeZone | Origin$ Graveyard | DestinationZone$ Battlefield | ValidTgts$ Creature | TgtZone$ Graveyard\nOracle:x\n").Faces[0].Abilities[0]
	if sa.Params["TgtZone"] != "Graveyard" {
		t.Fatalf("fixture must target the graveyard, got %q", sa.Params["TgtZone"])
	}
	cands := e.legalTargetCandidates(0, 0, 0, sa)
	found := false
	for _, c := range cands {
		if c.obj == o.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("graveyard card with printed K:Shroud withheld from a TgtZone$ Graveyard target offer: %v", cands)
	}
}

// guard: the helper must not see players (players have no ObjID — a zero id
// is never shrouded, matching protectedFrom's contract).
func TestShroudBlocksTargetZeroId(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	if e.shroudBlocksTarget(0) {
		t.Fatal("zero id reported as shrouded")
	}
}
