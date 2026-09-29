package rules

// tappowervalue_crew_test.go -- the Crew/Saddle half of stat:TapPowerValue.
//
// The Station mechanism (rules/tappowervalue_test.go) wired Station's reads
// through Engine.tapPowerValue. This file covers the other eight corpus
// carriers: the Pilot family that "crews Vehicles / saddles Mounts as though
// its power were 2 greater" (Value$ 2) or "using its toughness rather than
// its power" (Value$ Toughness), every one of them scoped to the
// Activated.Crew/Saddle action kinds.
//
// The one structural piece is that the tap-cost machinery now names the
// activated-action kind off the ability being activated (tapCostSAKind over
// the SA's Keyword$ tag) and passes it to Engine.tapPowerValue at every one
// of the five reads: the offer gate (nonManaCastable), the tap election's
// Option.Value and the total-power floor it re-checks (tapPermanentCostAsk),
// and the two sum helpers. That is what keeps a Station-scoped static out of
// a Crew action and a Crew-scoped one out of Station.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// crewPVVehicleSrc is a freely-authored Crew 3 Vehicle with no enters-the-
// battlefield ability, so the test reaches the tap election on the first
// priority round. The K:Crew:3 line runs the real keyword expansion
// (cards/kw_crew.go) -- the ability under test is the minted one, tagged
// Keyword$ Crew, exactly as a corpus Vehicle's is.
const crewPVVehicleSrc = "Name:Test Hauler\nManaCost:3\nTypes:Artifact Vehicle\nPT:4/4\n" +
	"K:Crew:3\nOracle:Crew 3\n"

// saddlePVMountSrc is a freely-authored Mount whose Saddle cost is spelled
// the way the Saddle keyword spells it now that it lands (CR 702.171,
// cards/kw_saddle.go): the corpus's own tap-any-number group-predicate cost,
// tagged Keyword$ Saddle. The Saddle keyword is implemented, but this fixture
// hand-writes the SA rather than printing K:Saddle:2 so the ability-level
// threading is pinned independently of the keyword expansion (Saddle is
// registered, so this file exercises only tapCostSAKind).
const saddlePVMountSrc = "Name:Test Saddler\nManaCost:2\nTypes:Creature Beast Mount\nPT:2/2\n" +
	"A:AB$ Animate | Cost$ tapXType<Any/Creature.Other+withTotalPowerGE2> | Defined$ Self | Types$ Artifact,Creature | Keyword$ Saddle | SpellDescription$ Saddle 2\n"

// crewPVEngine builds a two-seat engine whose battlefield carries a freshly
// minted Crew 3 Vehicle plus the named corpus creatures, all untapped and
// controlled by seat 0, and returns the engine, the Vehicle id, and the ids
// of the named creatures in order. It re-asks priority so the Vehicle's crew
// ability is live in the pending decision.
func crewPVEngine(t *testing.T, reg *cards.Registry, vehicle *cards.Card, names ...string) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	extras := []*cards.Card{vehicle}
	for _, name := range names {
		extras = append(extras, lookup(t, reg, name))
	}
	e := corpusEngine(t, reg, extras, nil)
	vehicleID := moveByName(t, e, 0, vehicle.Faces[0].Name, state.ZBattlefield)
	if o := e.G.Obj(vehicleID); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: %s zone %v tapped %v, want battlefield untapped", vehicle.Faces[0].Name, o.Zone, o.Tapped)
	}
	var ids []state.ObjID
	for _, name := range names {
		ids = append(ids, moveByName(t, e, 0, name, state.ZBattlefield))
	}
	e.pending = nil
	e.priorityRound()
	return e, vehicleID, ids
}

// crewPVElection submits the Crew 3 ability and returns the tap election.
// The Vehicle's ability index 0 is the minted crew ability (the synthetic
// Vehicle carries no other ability).
func crewPVElection(t *testing.T, e *Engine, vehicle state.ObjID) *decision.Decision {
	t.Helper()
	submitChoices(t, e, abilityOption(t, e, vehicle, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending decision %+v, want the crew tap election (KChoose)", d)
	}
	return d
}

// TestTapPowerValueCrewUsesToughnessOnGiantOx is the focused Crew mechanism
// test: Giant Ox is a 0/6 whose TapPowerValue static makes it CREW USING ITS
// TOUGHNESS. With the static unread the offer gate sums only its power (0),
// the Crew 3 ability is not even offered, and its election Value is 0 -- so
// a 0/6 is read as worthless. With the static wired through the SA's own
// Crew kind, the floor is met (6 >= 3), the election carries 6, and a lone
// Giant Ox pays it.
func TestTapPowerValueCrewUsesToughnessOnGiantOx(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	vehicle := card(t, crewPVVehicleSrc)
	e, vehicleID, ids := crewPVEngine(t, reg, vehicle, "Giant Ox")
	ox := ids[0]

	// Precondition: the values the assertions discriminate between differ
	// (power 0, toughness 6) and the creature is really on the battlefield.
	if p, tough := e.Power(ox), e.Toughness(ox); p != 0 || tough != 6 {
		t.Fatalf("precondition: Giant Ox is %d/%d, want 0/6", p, tough)
	}
	if o := e.G.Obj(ox); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: Giant Ox zone %v tapped %v", o.Zone, o.Tapped)
	}
	if e.IsCreature(vehicleID) {
		t.Fatal("precondition: the Vehicle is already animated")
	}

	// The offer gate must have admitted Crew 3 on the strength of Giant Ox's
	// TOUGHNESS alone; with power read plain (0) the ability is withheld.
	d := crewPVElection(t, e, vehicleID)
	if d.MinSum != 3 {
		t.Fatalf("crew tap election MinSum = %d, want 3", d.MinSum)
	}
	oxIdx := -1
	oxValue := -1
	for _, o := range d.Options {
		if o.Obj == ox {
			oxIdx, oxValue = o.Index, o.Value
		}
	}
	if oxIdx < 0 {
		t.Fatalf("Giant Ox was not offered as a crew candidate: %+v", d.Options)
	}
	if oxValue != 6 {
		t.Fatalf("Giant Ox's crew Option.Value = %d, want 6 (its toughness, not its 0 power)", oxValue)
	}

	// A lone Giant Ox pays the Crew 3 floor (6 >= 3).
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{oxIdx}}); err != nil {
		t.Fatalf("submitting Giant Ox's crew election: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(ox).Tapped {
		t.Fatal("Giant Ox was not tapped as the crew cost")
	}
	if !e.IsCreature(vehicleID) {
		t.Fatal("the crew activation did not animate the Vehicle")
	}
}

// TestTapPowerValueCrewAndStationScopesDoNotLeak is the mirror of
// TestTapPowerValueStationMechanism's "Giant Ox is Crew-scoped, not Station"
// case, in the other direction: a Station-scoped static (Tapestry Warden,
// which makes toughness>power creatures station using their toughness) must
// NOT raise a Crew action's total. The only other creature is a 0/4 wall,
// whose toughness would pay Crew 3 if the Station static leaked; without the
// leak the wall contributes its power (0) and Crew 3 is withheld.
func TestTapPowerValueCrewAndStationScopesDoNotLeak(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	vehicle := card(t, crewPVVehicleSrc)
	wall := card(t, "Name:PV Wall\nManaCost:2\nTypes:Creature Wall\nPT:0/4\nOracle:x\n")
	extras := []*cards.Card{vehicle, wall, lookup(t, reg, "Tapestry Warden")}
	e := corpusEngine(t, reg, extras, nil)
	vehicleID := moveByName(t, e, 0, vehicle.Faces[0].Name, state.ZBattlefield)
	moveByName(t, e, 0, "PV Wall", state.ZBattlefield)
	tw := moveByName(t, e, 0, "Tapestry Warden", state.ZBattlefield)
	// Tapestry Warden is itself a 3/4 creature and would otherwise be a legal
	// crew candidate whose power 3 meets the floor on its own; tap it so the
	// wall is the only other untapped creature the offer gate can sum.
	e.emit(events.Event{Kind: events.Tap, Obj: tw})
	e.pending = nil
	e.priorityRound()

	// Precondition: the wall is the 0-power/toughness 4 discrimination target
	// and Tapestry Warden's Station static is live on the battlefield.
	var wallID state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "PV Wall" {
			wallID = id
		}
	}
	if wallID == 0 || e.Power(wallID) != 0 || e.Toughness(wallID) != 4 {
		t.Fatalf("precondition: wall is %d/%d, want 0/4", e.Power(wallID), e.Toughness(wallID))
	}
	if got := e.tapPowerValue(wallID, "Station"); got != 4 {
		t.Fatalf("precondition: Tapestry Warden's Station value for the wall is %d, want 4 (the static is not live)", got)
	}
	if !e.G.Obj(tw).Tapped {
		t.Fatal("precondition: Tapestry Warden is untapped and would be a legal crew candidate")
	}

	// The Station static must not reach Crew: the wall still contributes 0,
	// so Crew 3 is not offered at all.
	if _, offered := findAbilityOption(e, vehicleID, 0); offered {
		t.Fatal("Crew 3 was offered although the only other creature is a 0-power wall and the Station static must not leak into Crew")
	}
}

// TestTapPowerValueSaddleReadsItsOwnKind pins the Saddle half of the
// threading at the ability level. Saddle is now implemented (CR 702.171,
// cards/kw_saddle.go), but this fixture hand-writes the tag on a synthetic
// ability rather than printing K:Saddle:2, pinning tapCostSAKind's own kind
// read independently of the keyword expansion. Two real corpus carriers
// sit on the battlefield: Cloudspire Captain scopes Saddle AND Crew (Value$
// 2, "saddles Mounts and crews Vehicles as though its power were 2
// greater"), Giant Ox scopes Crew only (Value$ Toughness). On a Saddle
// action Cloudspire's +2 applies and Giant Ox's toughness does not -- and
// with the kind lost (a bare "" scope) neither would apply.
func TestTapPowerValueSaddleReadsItsOwnKind(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	mount := card(t, saddlePVMountSrc)
	extras := []*cards.Card{mount, lookup(t, reg, "Cloudspire Captain"), lookup(t, reg, "Giant Ox")}
	e := corpusEngine(t, reg, extras, nil)
	mountID := moveByName(t, e, 0, mount.Faces[0].Name, state.ZBattlefield)
	captain := moveByName(t, e, 0, "Cloudspire Captain", state.ZBattlefield)
	ox := moveByName(t, e, 0, "Giant Ox", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()

	// Preconditions: the two carriers really carry the scopes this test
	// turns on, and their plain powers are the values the substitution must
	// differ from (Cloudspire 2 -> 4, Giant Ox 0 -> stays 0).
	if p, tough := e.Power(captain), e.Toughness(captain); p != 2 || tough != 3 {
		t.Fatalf("precondition: Cloudspire Captain is %d/%d, want 2/3", p, tough)
	}
	if p, tough := e.Power(ox), e.Toughness(ox); p != 0 || tough != 6 {
		t.Fatalf("precondition: Giant Ox is %d/%d, want 0/6", p, tough)
	}
	if got := e.tapPowerValue(captain, "Saddle"); got != 4 {
		t.Fatalf("precondition: Cloudspire Captain's Saddle value is %d, want 4 (its +2 static is not scoped to Saddle)", got)
	}
	if got := e.tapPowerValue(captain, "Station"); got != 2 {
		t.Fatalf("precondition: Cloudspire Captain's Station value is %d, want 2 (it scopes Saddle/Crew, not Station)", got)
	}

	// Submit the synthetic Saddle ability (index 0) and inspect the election.
	submitChoices(t, e, abilityOption(t, e, mountID, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.MinSum != 2 {
		t.Fatalf("Saddle tap election %+v, want KChoose MinSum 2", d)
	}
	got := map[state.ObjID]int{}
	for _, o := range d.Options {
		got[o.Obj] = o.Value
	}
	if got[captain] != 4 {
		t.Fatalf("Cloudspire Captain's Saddle Option.Value = %d, want 4 (its own Saddle-scoped +2)", got[captain])
	}
	if got[ox] != 0 {
		t.Fatalf("Giant Ox's Saddle Option.Value = %d, want 0 (its Crew-scoped Toughness must not reach Saddle)", got[ox])
	}
}

// TestTapCostSAKindReadsTheMintingKeyword is the unit half: the kind is read
// off the ability's own Keyword$ tag, so a Crew-tagged ability reads "Crew",
// a Saddle-tagged one "Saddle", and a hand-written tapXType ability (which
// carries no keyword) reads "".
func TestTapCostSAKindReadsTheMintingKeyword(t *testing.T) {
	crew := card(t, crewPVVehicleSrc)
	var crewSA *cards.SA
	for _, ab := range crew.Faces[0].Abilities {
		if ab.Params["Keyword"] == "Crew" {
			crewSA = ab
		}
	}
	if crewSA == nil {
		t.Fatalf("the synthetic Vehicle has no Crew-tagged ability: %+v", crew.Faces[0].Abilities)
	}
	if got := tapCostSAKind(crewSA); got != "Crew" {
		t.Errorf("tapCostSAKind(crew ability) = %q, want %q", got, "Crew")
	}
	if got := tapCostSAKind(card(t, saddlePVMountSrc).Faces[0].Abilities[0]); got != "Saddle" {
		t.Errorf("tapCostSAKind(saddle ability) = %q, want %q", got, "Saddle")
	}
	if got := tapCostSAKind(card(t, "Name:PV Troll\nManaCost:3\nTypes:Creature Troll\nPT:2/2\n"+
		"A:AB$ Pump | Cost$ tapXType<Any/Creature.Other+withTotalPowerGE2> | Defined$ Self | NumAtt$ +2 | SpellDescription$ x\n").Faces[0].Abilities[0]); got != "" {
		t.Errorf("tapCostSAKind(hand-written tapXType ability) = %q, want \"\"", got)
	}
	if got := tapCostSAKind(nil); got != "" {
		t.Errorf("tapCostSAKind(nil) = %q, want \"\"", got)
	}
}
