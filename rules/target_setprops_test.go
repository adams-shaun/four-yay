package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file pins Forge's target-SET property constraints (rules/setprops.go):
// TargetsWithSameCardType$, TargetsWithSameCreatureType$,
// TargetsWithEqualToughness$, TargetsWithDifferentCMC$,
// TargetsWithDifferentNames$ and the per-candidate
// TargetsWithControllerProperty$ predicate. Each test asserts its own
// preconditions -- the fixture really carries the parameter and the compared
// properties really differ -- so a vacuous setup fails loudly.

// setPropSA builds a synthetic targeting SA carrying ValidTgts$ plus the
// params under test, with a permissive 0..2 bound so a positive test poses an
// ask (the set constraints cap Max themselves; a mandatory fizzle is pinned
// separately).
func setPropSA(validTgts string, extra map[string]string) *cards.SA {
	p := map[string]string{
		"ValidTgts": validTgts,
		"TargetMin": "0",
		"TargetMax": "2",
	}
	for k, v := range extra {
		p[k] = v
	}
	return &cards.SA{Params: p}
}

// putBattlefield adds a permanent under seat and moves it to the
// battlefield, asserting the move took effect.
func putBattlefield(t *testing.T, e *Engine, seat state.PlayerID, src string) state.ObjID {
	t.Helper()
	id := e.G.AddObject(card(t, src), seat).ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
		t.Fatalf("permanent %d zone = %v, want battlefield", id, z)
	}
	return id
}

// putGraveyard adds a card under seat and moves it to the graveyard.
func putGraveyard(t *testing.T, e *Engine, seat state.PlayerID, src string) state.ObjID {
	t.Helper()
	id := e.G.AddObject(card(t, src), seat).ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("card %d zone = %v, want graveyard", id, z)
	}
	return id
}

// optionFor returns the offered option index for an object, or -1.
func setPropOptionIndex(d *decision.Decision, id state.ObjID) int {
	for _, o := range d.Options {
		if o.Obj == id {
			return o.Index
		}
	}
	return -1
}

// TestTargetSetSameCardType pins TargetsWithSameCardType$ at the offer: the
// two permanents that share a printed card type are a legal pair, and the pair
// that shares none is rejected by the SAME rule Validate enforces.
func TestTargetSetSameCardType(t *testing.T) {
	sa := setPropSA("Permanent", map[string]string{"TargetsWithSameCardType": "True"})
	if sa.Params["TargetsWithSameCardType"] != "True" {
		t.Fatal("precondition: fixture lost TargetsWithSameCardType$")
	}
	e := newSeats(t, 2)
	artifactCreature := putBattlefield(t, e, 0, "Name:Servo\nTypes:Artifact Creature\nPT:2/2\nOracle:x\n")
	artifact := putBattlefield(t, e, 0, "Name:Relic\nTypes:Artifact\nOracle:x\n")
	creature := putBattlefield(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// Preconditions: the compared card-type sets really differ.
	ac := e.setPropTokens("cardtype", artifactCreature)
	ar := e.setPropTokens("cardtype", artifact)
	cr := e.setPropTokens("cardtype", creature)
	if !decision.SetPropAdmits(decision.SetPropShared, ac, ar) {
		t.Fatalf("precondition: Artifact Creature %v and Artifact %v must share a card type", ac, ar)
	}
	if decision.SetPropAdmits(decision.SetPropShared, ar, cr) {
		t.Fatalf("precondition: Artifact %v and Creature %v must share no card type", ar, cr)
	}
	e.pending = nil
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want a target decision", d)
	}
	if d.SetPropMode != decision.SetPropShared {
		t.Fatalf("SetPropMode = %q, want shared", d.SetPropMode)
	}
	if got := d.Options[setPropOptionIndex(d, artifactCreature)].SetProps; len(got) == 0 {
		t.Fatalf("option %d carries no SetProps", artifactCreature)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, artifactCreature), setPropOptionIndex(d, artifact)}}); err != nil {
		t.Fatalf("shared card type pair rejected: %v", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, artifact), setPropOptionIndex(d, creature)}}); err == nil {
		t.Fatal("a non-sharing card-type pair was accepted")
	}
}

// TestTargetSetSameCreatureType pins TargetsWithSameCreatureType$: two
// creatures sharing a creature subtype are legal, two that share none are not.
func TestTargetSetSameCreatureType(t *testing.T) {
	sa := setPropSA("Creature", map[string]string{"TargetsWithSameCreatureType": "True"})
	if sa.Params["TargetsWithSameCreatureType"] != "True" {
		t.Fatal("precondition: fixture lost TargetsWithSameCreatureType$")
	}
	e := newSeats(t, 2)
	elf := putBattlefield(t, e, 0, "Name:Elf One\nTypes:Creature Elf\nPT:2/2\nOracle:x\n")
	elfWarrior := putBattlefield(t, e, 0, "Name:Elf Two\nTypes:Creature Elf Warrior\nPT:2/2\nOracle:x\n")
	goblin := putBattlefield(t, e, 1, "Name:Goblin\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	if !decision.SetPropAdmits(decision.SetPropShared,
		e.setPropTokens("creaturetype", elf), e.setPropTokens("creaturetype", elfWarrior)) {
		t.Fatal("precondition: Elf and Elf Warrior must share the Elf creature type")
	}
	if decision.SetPropAdmits(decision.SetPropShared,
		e.setPropTokens("creaturetype", elf), e.setPropTokens("creaturetype", goblin)) {
		t.Fatal("precondition: Elf and Goblin must share no creature type")
	}
	e.pending = nil
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil || d.SetPropMode != decision.SetPropShared {
		t.Fatalf("pending = %+v, want a shared target decision", d)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, elf), setPropOptionIndex(d, elfWarrior)}}); err != nil {
		t.Fatalf("shared creature type pair rejected: %v", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, elf), setPropOptionIndex(d, goblin)}}); err == nil {
		t.Fatal("a non-sharing creature-type pair was accepted")
	}
}

// TestTargetSetEqualToughness pins TargetsWithEqualToughness$ (Vatz's "choose
// any number of target creatures with equal toughness").
func TestTargetSetEqualToughness(t *testing.T) {
	sa := setPropSA("Creature", map[string]string{"TargetsWithEqualToughness": "True"})
	if sa.Params["TargetsWithEqualToughness"] != "True" {
		t.Fatal("precondition: fixture lost TargetsWithEqualToughness$")
	}
	e := newSeats(t, 2)
	two := putBattlefield(t, e, 0, "Name:Two\nTypes:Creature\nPT:2/2\nOracle:x\n")
	three := putBattlefield(t, e, 0, "Name:Three\nTypes:Creature\nPT:4/3\nOracle:x\n")
	threeB := putBattlefield(t, e, 1, "Name:Three B\nTypes:Creature\nPT:1/3\nOracle:x\n")
	if e.Toughness(two) != 2 || e.Toughness(three) != 3 || e.Toughness(threeB) != 3 {
		t.Fatalf("precondition: toughness = %d/%d/%d, want 2/3/3",
			e.Toughness(two), e.Toughness(three), e.Toughness(threeB))
	}
	e.pending = nil
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil || d.SetPropMode != decision.SetPropShared {
		t.Fatalf("pending = %+v, want a shared target decision", d)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, three), setPropOptionIndex(d, threeB)}}); err != nil {
		t.Fatalf("equal-toughness pair rejected: %v", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, two), setPropOptionIndex(d, three)}}); err == nil {
		t.Fatal("a differing-toughness pair was accepted")
	}
}

// TestTargetSetDifferentCMC pins TargetsWithDifferentCMC$ (Long Rest's
// "return X target cards with different mana values from your graveyard").
func TestTargetSetDifferentCMC(t *testing.T) {
	sa := setPropSA("Card", map[string]string{
		"TargetsWithDifferentCMC": "True",
		"TgtZone":                 "Graveyard",
	})
	if sa.Params["TargetsWithDifferentCMC"] != "True" {
		t.Fatal("precondition: fixture lost TargetsWithDifferentCMC$")
	}
	e := newSeats(t, 2)
	one := putGraveyard(t, e, 0, "Name:One\nManaCost:U\nTypes:Instant\nOracle:x\n")
	two := putGraveyard(t, e, 0, "Name:Two\nManaCost:1 U\nTypes:Instant\nOracle:x\n")
	twoB := putGraveyard(t, e, 1, "Name:Two B\nManaCost:2\nTypes:Sorcery\nOracle:x\n")
	if e.G.Obj(one).Face().Cmc() != 1 || e.G.Obj(two).Face().Cmc() != 2 || e.G.Obj(twoB).Face().Cmc() != 2 {
		t.Fatalf("precondition: cmc = %d/%d/%d, want 1/2/2",
			e.G.Obj(one).Face().Cmc(), e.G.Obj(two).Face().Cmc(), e.G.Obj(twoB).Face().Cmc())
	}
	e.pending = nil
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil || d.SetPropMode != decision.SetPropDistinct {
		t.Fatalf("pending = %+v, want a distinct target decision", d)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, one), setPropOptionIndex(d, two)}}); err != nil {
		t.Fatalf("different-cmc pair rejected: %v", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, two), setPropOptionIndex(d, twoB)}}); err == nil {
		t.Fatal("a same-cmc pair was accepted")
	}
}

// TestTargetSetDifferentNames pins TargetsWithDifferentNames$ (Behold the
// Sinister Six / Drover of the Swine: "creature cards with different names").
func TestTargetSetDifferentNames(t *testing.T) {
	sa := setPropSA("Card", map[string]string{
		"TargetsWithDifferentNames": "True",
		"TgtZone":                   "Graveyard",
	})
	if sa.Params["TargetsWithDifferentNames"] != "True" {
		t.Fatal("precondition: fixture lost TargetsWithDifferentNames$")
	}
	e := newSeats(t, 2)
	alpha := putGraveyard(t, e, 0, "Name:Alpha\nTypes:Creature Boar\nPT:2/2\nOracle:x\n")
	beta := putGraveyard(t, e, 0, "Name:Beta\nTypes:Creature Boar\nPT:2/2\nOracle:x\n")
	betaB := putGraveyard(t, e, 1, "Name:Beta\nTypes:Creature Boar\nPT:3/3\nOracle:x\n")
	if e.G.Obj(alpha).Face().Name == e.G.Obj(beta).Face().Name ||
		e.G.Obj(beta).Face().Name != e.G.Obj(betaB).Face().Name {
		t.Fatalf("precondition: names = %q/%q/%q, want two distinct names with one duplicate",
			e.G.Obj(alpha).Face().Name, e.G.Obj(beta).Face().Name, e.G.Obj(betaB).Face().Name)
	}
	e.pending = nil
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil || d.SetPropMode != decision.SetPropDistinct {
		t.Fatalf("pending = %+v, want a distinct target decision", d)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, alpha), setPropOptionIndex(d, beta)}}); err != nil {
		t.Fatalf("different-name pair rejected: %v", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{setPropOptionIndex(d, beta), setPropOptionIndex(d, betaB)}}); err == nil {
		t.Fatal("a same-name pair was accepted")
	}
}

// TestTargetSetControllerProperty pins TargetsWithControllerProperty$
// (Drown in the Loch): the candidate's own mana value must be <= the number of
// cards in ITS CONTROLLER's graveyard. It is a per-candidate legality filter,
// so the illegal candidate is never offered and is dropped by the resolution
// recheck too.
func TestTargetSetControllerProperty(t *testing.T) {
	sa := setPropSA("Creature", map[string]string{"TargetsWithControllerProperty": "cmcLECardsInGraveyard"})
	if sa.Params["TargetsWithControllerProperty"] != "cmcLECardsInGraveyard" {
		t.Fatal("precondition: fixture lost TargetsWithControllerProperty$")
	}
	e := newSeats(t, 2)
	// Seat 0's graveyard holds two cards; the cmc-2 creature qualifies, the
	// cmc-5 creature does not.
	putGraveyard(t, e, 0, "Name:Fodder A\nTypes:Instant\nOracle:x\n")
	putGraveyard(t, e, 0, "Name:Fodder B\nTypes:Instant\nOracle:x\n")
	cheap := putBattlefield(t, e, 0, "Name:Cheap\nManaCost:1 U\nTypes:Creature\nPT:2/2\nOracle:x\n")
	dear := putBattlefield(t, e, 0, "Name:Dear\nManaCost:3 U U\nTypes:Creature\nPT:4/4\nOracle:x\n")
	if e.G.Obj(cheap).Face().Cmc() != 2 || e.G.Obj(dear).Face().Cmc() != 5 {
		t.Fatalf("precondition: cmc = %d/%d, want 2/5", e.G.Obj(cheap).Face().Cmc(), e.G.Obj(dear).Face().Cmc())
	}
	if len(e.G.Zone(state.ZGraveyard, 0)) != 2 {
		t.Fatalf("precondition: seat 0 graveyard = %d cards, want 2", len(e.G.Zone(state.ZGraveyard, 0)))
	}
	if !e.targetControllerPropertyAdmits("cmcLECardsInGraveyard", cheap) {
		t.Fatal("precondition: the cmc-2 creature must be admitted by 2 graveyard cards")
	}
	if e.targetControllerPropertyAdmits("cmcLECardsInGraveyard", dear) {
		t.Fatal("precondition: the cmc-5 creature must be refused by 2 graveyard cards")
	}
	e.pending = nil
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil {
		t.Fatal("no target decision posed")
	}
	if setPropOptionIndex(d, dear) != -1 {
		t.Fatal("the over-cost creature was offered despite TargetsWithControllerProperty$")
	}
	if setPropOptionIndex(d, cheap) == -1 {
		t.Fatal("the legal creature was not offered")
	}
	// The resolution recheck applies the same predicate: a recorded target
	// that no longer qualifies is dropped.
	kept := e.legalTargets([]state.Target{{Obj: cheap}, {Obj: dear}}, sa, targetZones(sa), 0, 0, 0)
	if len(kept) != 1 || kept[0].Obj != cheap {
		t.Fatalf("recheck = %+v, want only the legal cmc-2 creature %d", kept, cheap)
	}
}

// TestTargetSetControllerPropertyPower pins the power-bearing sibling of the
// cmc predicate (Neural Network's `powerLECardsInGraveyard`): the candidate's
// own power must be <= the number of cards in ITS CONTROLLER's graveyard. Its
// cmc is deliberately 0 so a cmc comparison could not stand in for the power
// one -- the precondition rejects that substitution.
func TestTargetSetControllerPropertyPower(t *testing.T) {
	sa := setPropSA("Creature", map[string]string{"TargetsWithControllerProperty": "powerLECardsInGraveyard"})
	if sa.Params["TargetsWithControllerProperty"] != "powerLECardsInGraveyard" {
		t.Fatal("precondition: fixture lost TargetsWithControllerProperty$ power")
	}
	e := newSeats(t, 2)
	putGraveyard(t, e, 0, "Name:Fodder A\nTypes:Instant\nOracle:x\n")
	putGraveyard(t, e, 0, "Name:Fodder B\nTypes:Instant\nOracle:x\n")
	small := putBattlefield(t, e, 0, "Name:Small\nTypes:Creature\nPT:2/2\nOracle:x\n")
	big := putBattlefield(t, e, 0, "Name:Big\nTypes:Creature\nPT:3/3\nOracle:x\n")
	if e.Power(small) != 2 || e.Power(big) != 3 {
		t.Fatalf("precondition: power = %d/%d, want 2/3", e.Power(small), e.Power(big))
	}
	// The compared quantity is POWER, not mana value: both cards have cmc 0,
	// so a cmc comparison would admit neither of these preconditions.
	if e.G.Obj(small).Face() == nil || e.G.Obj(small).Face().Cmc() != 0 || e.G.Obj(big).Face().Cmc() != 0 {
		t.Fatalf("precondition: cmc = %d/%d, want 0/0 so only power can distinguish them",
			e.G.Obj(small).Face().Cmc(), e.G.Obj(big).Face().Cmc())
	}
	if len(e.G.Zone(state.ZGraveyard, 0)) != 2 {
		t.Fatalf("precondition: seat 0 graveyard = %d cards, want 2", len(e.G.Zone(state.ZGraveyard, 0)))
	}
	if !e.targetControllerPropertyAdmits("powerLECardsInGraveyard", small) {
		t.Fatal("precondition: the power-2 creature must be admitted by 2 graveyard cards")
	}
	if e.targetControllerPropertyAdmits("powerLECardsInGraveyard", big) {
		t.Fatal("precondition: the power-3 creature must be refused by 2 graveyard cards")
	}
	e.pending = nil
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil {
		t.Fatal("no target decision posed")
	}
	if setPropOptionIndex(d, big) != -1 {
		t.Fatal("the power-3 creature was offered despite TargetsWithControllerProperty$")
	}
	if setPropOptionIndex(d, small) == -1 {
		t.Fatal("the power-2 creature was not offered")
	}
	kept := e.legalTargets([]state.Target{{Obj: small}, {Obj: big}}, sa, targetZones(sa), 0, 0, 0)
	if len(kept) != 1 || kept[0].Obj != small {
		t.Fatalf("recheck = %+v, want only the power-2 creature %d", kept, small)
	}
}

// TestTargetSetMandatorySharedCapacityFizzles pins that an unsatisfiable
// mandatory shared constraint fizzles (CR 608.2b's counter/fizzle exit) rather
// than posing a decision no answer can satisfy -- the same contract
// sameControllerCapacity uses.
func TestTargetSetMandatorySharedCapacityFizzles(t *testing.T) {
	sa := setPropSA("Permanent", map[string]string{
		"TargetsWithSameCardType": "True",
		"TargetMin":               "2",
		"TargetMax":               "2",
	})
	if sa.Params["TargetMin"] != "2" {
		t.Fatal("precondition: fixture lost the mandatory Min 2")
	}
	e := newSeats(t, 2)
	artifact := putBattlefield(t, e, 0, "Name:Relic\nTypes:Artifact\nOracle:x\n")
	creature := putBattlefield(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if decision.SetPropAdmits(decision.SetPropShared,
		e.setPropTokens("cardtype", artifact), e.setPropTokens("cardtype", creature)) {
		t.Fatal("precondition: the two candidates must share no card type")
	}
	e.pending = nil
	e.askTarget(0, 0, sa)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("an unsatisfiable shared target decision was posed: %+v", d)
	}
	if !hasEventText(e, "countered: no legal targets") {
		t.Fatal("mandatory shared ask did not fizzle")
	}
}

// TestTargetSetDistinctClampNeverLivelocks pins the one-home contract: the
// bot's own repair (botpolicy.Clamp, driven from the same decision rule as
// Validate) must return an answer Validate accepts even when the bot's
// preferred picks violate the distinct constraint. This is the livelock guard
// -- the deterministic bot re-submitting a rejected answer forever.
func TestTargetSetDistinctClampNeverLivelocks(t *testing.T) {
	d := &decision.Decision{
		Seq: 3, Player: 0, Kind: decision.KTarget, Min: 2, Max: 2,
		SetPropMode: decision.SetPropDistinct,
		Options: []decision.Option{
			{Index: 0, SetProps: []string{"2"}},
			{Index: 1, SetProps: []string{"2"}},
			{Index: 2, SetProps: []string{"3"}},
		},
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 2}}); err != nil {
		t.Fatalf("precondition: a distinct pair must be legal: %v", err)
	}
	clamped := botpolicy.Clamp(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}})
	if err := d.Validate(clamped); err != nil {
		t.Fatalf("Clamp returned an invalid distinct answer: %v (%+v)", err, clamped)
	}
	if len(clamped.Choices) != 2 {
		t.Fatalf("Clamp = %v, want two distinct picks", clamped.Choices)
	}
}

// TestTargetSetSharedClampNeverLivelocks is the shared-constraint twin of the
// distinct clamp test.
func TestTargetSetSharedClampNeverLivelocks(t *testing.T) {
	d := &decision.Decision{
		Seq: 4, Player: 0, Kind: decision.KTarget, Min: 2, Max: 2,
		SetPropMode: decision.SetPropShared,
		Options: []decision.Option{
			{Index: 0, SetProps: []string{"artifact"}},
			{Index: 1, SetProps: []string{"creature"}},
			{Index: 2, SetProps: []string{"artifact"}},
		},
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 2}}); err != nil {
		t.Fatalf("precondition: a sharing pair must be legal: %v", err)
	}
	clamped := botpolicy.Clamp(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}})
	if err := d.Validate(clamped); err != nil {
		t.Fatalf("Clamp returned an invalid shared answer: %v (%+v)", err, clamped)
	}
	if len(clamped.Choices) != 2 {
		t.Fatalf("Clamp = %v, want two sharing picks", clamped.Choices)
	}
}

// TestTargetSetResolutionRecheckNarrows pins the CR 608.2b recheck for the
// set constraints: a recorded set that no longer shares a property (or has
// lost its distinctness) is narrowed in recorded order, and a conforming set
// passes through untouched. It is the set-property sibling of
// narrowSameController/narrowDifferentControllers.
func TestTargetSetResolutionRecheckNarrows(t *testing.T) {
	e := newSeats(t, 2)
	elf := putBattlefield(t, e, 0, "Name:Elf\nTypes:Creature Elf\nPT:2/2\nOracle:x\n")
	goblin := putBattlefield(t, e, 0, "Name:Goblin\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	if decision.SetPropAdmits(decision.SetPropShared,
		e.setPropTokens("creaturetype", elf), e.setPropTokens("creaturetype", goblin)) {
		t.Fatal("precondition: the two creatures must share no creature type")
	}
	sa := setPropSA("Creature", map[string]string{"TargetsWithSameCreatureType": "True"})
	got := e.narrowSetProps(sa, []state.Target{{Obj: elf}, {Obj: goblin}})
	if len(got) != 1 || got[0].Obj != elf {
		t.Fatalf("recheck = %+v, want only the first recorded target %d", got, elf)
	}
	// A conforming set is untouched.
	same := e.narrowSetProps(sa, []state.Target{{Obj: elf}})
	if len(same) != 1 {
		t.Fatalf("single-target set = %+v, want it kept", same)
	}
}
