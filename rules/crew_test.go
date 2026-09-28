package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// kw:Crew (CR 702.122) -- "Tap any number of other untapped creatures you
// control with total power N or greater: This Vehicle becomes an artifact
// creature until end of turn."
//
// The expansion (cards/kw_crew.go) mints one AB$ Animate whose cost is the
// tap-any-number form the corpus already spells for this exact shape:
// `tapXType<Any/Creature.Other+withTotalPowerGE<N>>`. The withTotalPowerGE
// group predicate is a SET-level constraint over the tapped creatures'
// TOTAL power, so it is not a per-candidate filter: rules/mana.go's
// stripGroupPowerFloor moves it into CostPart.MinPower, and the floor is
// enforced at three sites that all read the same Decision.MinSum /
// Option.Value wire contract --
//
//   - the offer gate (nonManaCastable): the ability is not even offered
//     when the board's other untapped creatures cannot reach the floor;
//   - Decision.Validate (decision/decision.go): the one legal-answer home
//     every client and the bot repair read;
//   - the tap election's re-entry (tapPermanentCostAsk): a board that
//     changed under the offer aborts the whole activation (CR 733.1)
//     rather than posing an ask no legal answer satisfies.
//
// Mossbridge Troll's withTotalPowerGE10 cost rides the same machinery, so
// its paid direction is pinned here too
// (TestMossbridgeTrollPaysWhenTotalPowerReachesTheFloor); its withheld
// direction stays beside its Battlesphere siblings in
// myr_battlesphere_test.go.

// crewFixture builds a two-seat engine whose battlefield carries the REAL
// corpus Avengers Quinjet (Vehicle, 4/4, Crew 3) plus the named crew
// candidates for seat 0, and returns the engine with every id. It returns
// after the first priority round, so the crew ability is already offered.
func crewFixture(t *testing.T, candidates ...string) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	vehicle, ok := reg.Lookup("Avengers Quinjet")
	if !ok {
		t.Fatal("corpus has no Avengers Quinjet")
	}
	extras := []*cards.Card{vehicle}
	for _, name := range candidates {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus has no %s", name)
		}
		extras = append(extras, c)
	}
	e := corpusEngine(t, reg, extras, nil)
	vehicleID := moveByName(t, e, 0, "Avengers Quinjet", state.ZBattlefield)
	if o := e.G.Obj(vehicleID); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("Avengers Quinjet precondition: zone %v tapped %v, want battlefield untapped", o.Zone, o.Tapped)
	}
	var ids []state.ObjID
	for _, name := range candidates {
		ids = append(ids, moveByName(t, e, 0, name, state.ZBattlefield))
	}
	// Avengers Quinjet carries its own ETB trigger ("Whenever this Vehicle
	// enters or attacks, choose one -- ..."): the priority round below puts
	// it on the stack and starts resolving it, so answer it fully, then ask
	// priority again -- the crew ability, not the Charm, is the option under
	// test. The first mode is the optional no-op at an empty Hero hand.
	e.pending = nil
	e.priorityRound()
	resolveQuinjetETB(t, e)
	e.pending = nil
	e.priorityRound()
	return e, vehicleID, ids
}

// resolveQuinjetETB answers Avengers Quinjet's enter-the-battlefield Charm
// trigger (and any ask its chosen mode poses) until the stack empties, so
// the caller's next priority round sees only the crew ability. It never
// advances a step or answers a priority pass while the stack is empty.
func resolveQuinjetETB(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 30; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KModes, decision.KTriggerOptional, decision.KChoose:
			if len(d.Options) == 0 {
				t.Fatalf("Avengers Quinjet ETB ask %+v has no option to answer", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("answer Avengers Quinjet ETB ask: %v", err)
			}
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				return
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("pass priority over Avengers Quinjet ETB: %v", err)
			}
		default:
			t.Fatalf("unexpected decision %+v while resolving Avengers Quinjet's ETB", d)
		}
	}
	t.Fatal("Avengers Quinjet's ETB trigger did not resolve within the answer budget")
}

// TestCrewAvengersQuinjetAnimatesUntilEndOfTurn is the end-to-end proof the
// acceptance ratchet's kw:Crew registration cites: the real corpus Vehicle
// offers its crew ability, the tap election carries the set-level power
// floor as Decision.MinSum, an insufficient election is rejected by
// Decision.Validate (and only that), and a sufficient one taps the chosen
// creatures and animates the Vehicle until end of turn. The Vehicle crews
// while summoning sick: CR 302.6 gates attacking and the {T} symbol, never
// tapping creatures as a cost, so the ability is offered and payable with
// the sickness flag live.
func TestCrewAvengersQuinjetAnimatesUntilEndOfTurn(t *testing.T) {
	t.Parallel()
	e, vehicle, ids := crewFixture(t, "Grizzly Bears", "Llanowar Elves")
	bears, elves := ids[0], ids[1]
	// Preconditions the assertions below depend on: the vehicle is a summoning-SICK,
	// non-creature artifact with its printed 4/4 (crewing pays no heed to
	// sickness, so the fixture keeps the flag live), the candidates are
	// untapped creatures with DIFFERENT powers, and the crew ability (index
	// 0) is offered.
	if o := e.G.Obj(vehicle); !o.SummonSick {
		t.Fatal("precondition: Avengers Quinjet is not summoning sick; the fixture no longer exercises crew's sickness independence")
	}
	if e.IsCreature(vehicle) {
		t.Fatal("precondition: Avengers Quinjet is already a creature")
	}
	if got := e.Power(vehicle); got != 4 || e.Power(bears) != 2 || e.Power(elves) != 1 {
		t.Fatalf("precondition powers: vehicle %d bears %d elves %d, want 4/2/1", e.Power(vehicle), e.Power(bears), e.Power(elves))
	}
	if e.G.Obj(bears).Tapped || e.G.Obj(elves).Tapped {
		t.Fatal("precondition: a crew candidate is already tapped")
	}
	// The minted ability is the face's index-0 activated ability.
	opt := abilityOption(t, e, vehicle, 0)
	if !strings.Contains(opt.Label, "Crew 3") {
		t.Fatalf("crew ability label %q does not name Crew 3", opt.Label)
	}
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending decision %+v, want the crew tap election (KChoose)", d)
	}
	if d.Min != 1 || d.Max != 2 {
		t.Fatalf("crew tap election Min/Max = %d/%d, want 1/2 (tap at least one other creature)", d.Min, d.Max)
	}
	if d.MinSum != 3 {
		t.Fatalf("crew tap election MinSum = %d, want 3 (the vehicle's crew cost)", d.MinSum)
	}
	if len(d.Options) != 2 {
		t.Fatalf("crew tap election offers %d options %+v, want exactly the two other creatures", len(d.Options), d.Options)
	}
	powers := map[state.ObjID]int{}
	for _, o := range d.Options {
		if o.Obj == vehicle {
			t.Fatalf("the crew election offers the Vehicle itself (%+v) -- CR 702.122a says OTHER creatures", o)
		}
		powers[o.Obj] = o.Value
	}
	if powers[bears] != 2 || powers[elves] != 1 {
		t.Fatalf("crew option Values %+v, want the candidates' powers (bears 2, elves 1)", powers)
	}

	// An insufficient election (the 1-power Elves alone) must be rejected by
	// Validate -- and the decision must survive for a legal answer.
	elvesIdx := -1
	bearsIdx := -1
	for _, o := range d.Options {
		if o.Obj == elves {
			elvesIdx = o.Index
		}
		if o.Obj == bears {
			bearsIdx = o.Index
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{elvesIdx}}); err == nil {
		t.Fatal("submitting a 1-power crew election was accepted -- the total-power floor is not enforced")
	}
	if d2 := e.Pending(); d2 == nil || d2.Seq != d.Seq {
		t.Fatal("the rejected crew election did not stay pending for a legal answer")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{elvesIdx, bearsIdx}}); err != nil {
		t.Fatalf("legal crew election rejected: %v", err)
	}
	passUntilStackEmpty(t, e, 20)

	if !e.G.Obj(bears).Tapped || !e.G.Obj(elves).Tapped {
		t.Fatal("the crewed creatures were not tapped as the cost")
	}
	if !e.IsCreature(vehicle) {
		t.Fatal("the crew activation did not animate Avengers Quinjet")
	}
	if got := e.Power(vehicle); got != 4 {
		t.Fatalf("crewed vehicle power = %d, want 4 (the printed P/T, no Power$/Toughness$ in the animation)", got)
	}
	if !faceHasType(e.G.Obj(vehicle), "Artifact") {
		t.Fatal("crewed vehicle lost its printed Artifact type (CR 702.122b: it BECOMES an artifact creature)")
	}

	// The animation reverts at end of turn: on seat 0's next turn (turn 3,
	// after turn 1's cleanup) the vehicle is an ordinary Vehicle again and
	// the crewed creatures have untapped in seat 0's untap step.
	driveToStepAll(t, e, 3, 0, state.StepMain1)
	if e.IsCreature(vehicle) {
		t.Fatal("the crew animation outlived end of turn")
	}
	if e.G.Obj(bears).Tapped || e.G.Obj(elves).Tapped {
		t.Fatal("the crewing creatures never untapped")
	}
}

// TestCrewIsNotOfferedWhenTotalPowerFallsShort pins the offer gate: with no
// board that can reach the crew cost, the ability is not offered at all
// (never an election whose every answer would be rejected -- the livelock
// shape the offer gates exist to withhold).
func TestCrewIsNotOfferedWhenTotalPowerFallsShort(t *testing.T) {
	t.Parallel()
	e, vehicle, ids := crewFixture(t, "Llanowar Elves")
	elves := ids[0]
	if e.IsCreature(vehicle) || e.Power(elves) != 1 {
		t.Fatalf("precondition: vehicle creature=%v elves power=%d, want false/1", e.IsCreature(vehicle), e.Power(elves))
	}
	if _, offered := findAbilityOption(e, vehicle, 0); offered {
		t.Fatal("the crew ability was offered although the only other creature has 1 power (crew 3 needs 3)")
	}
}

// TestCrewElectionExcludesTappedCreaturesAndTheVehicle pins the candidate
// rules: the election offers only OTHER untapped creatures -- a tapped
// creature cannot pay the crew cost, and the Vehicle (a non-creature
// artifact at offer time, excluded twice over by Creature.Other) is never a
// candidate no matter what its printed power is.
func TestCrewElectionExcludesTappedCreaturesAndTheVehicle(t *testing.T) {
	t.Parallel()
	e, vehicle, ids := crewFixture(t, "Grizzly Bears", "Llanowar Elves", "Centaur Courser")
	bears, elves, courser := ids[0], ids[1], ids[2]
	// Preconditions: the elves are a legal-looking candidate that this test
	// TAPS before the election, so the offered list is where the rule binds.
	if e.Power(courser) != 3 || e.G.Obj(elves).Tapped {
		t.Fatalf("precondition: courser power %d, elves tapped %v", e.Power(courser), e.G.Obj(elves).Tapped)
	}
	e.pending = nil
	e.emit(events.Event{Kind: events.Tap, Obj: elves})
	e.priorityRound()

	opt := abilityOption(t, e, vehicle, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.MinSum != 3 {
		t.Fatalf("crew tap election %+v, want KChoose MinSum 3", d)
	}
	seen := map[state.ObjID]int{}
	for _, o := range d.Options {
		seen[o.Obj] = o.Value
		if o.Obj == vehicle {
			t.Fatal("the crew election offered the Vehicle itself")
		}
	}
	if _, ok := seen[elves]; ok {
		t.Fatal("the crew election offered the tapped Elves as a candidate")
	}
	if _, ok := seen[bears]; !ok {
		t.Fatalf("the crew election offered %+v, want the untapped Bears among the candidates", d.Options)
	}
	if _, ok := seen[courser]; !ok {
		t.Fatalf("the crew election offered %+v, want the untapped Courser among the candidates", d.Options)
	}
	// Tap the untapped pair (2 + 3 = 5 >= 3): the tapped creature stays
	// untapped and untouched.
	choice := []int{}
	for _, o := range d.Options {
		choice = append(choice, o.Index)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choice}); err != nil {
		t.Fatalf("legal crew election rejected: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	// The tapped Elves were never a candidate, so the activation paid the
	// floor with Bears + Courser only (2 + 3 = 5): the pair is tapped and the
	// pre-tapped Elves were not counted or re-tapped by the crew.
	if !e.G.Obj(bears).Tapped || !e.G.Obj(courser).Tapped {
		t.Fatal("the elected crew creatures were not tapped")
	}
	if !e.IsCreature(vehicle) {
		t.Fatal("the crew activation did not animate the vehicle")
	}
}

// TestCrewBotAnswerMeetsTheFloor pins the one-home rule end to end: the
// bot's own policy answer (botpolicy.Decide through Clamp, the seat.Bot
// path) over a crew election whose first offered option ALONE does not meet
// the floor must pass Decision.Validate and complete the activation. The
// constraint binds here because the floor is unmeetable by any single
// low-power pick the default arm would take.
func TestCrewBotAnswerMeetsTheFloor(t *testing.T) {
	t.Parallel()
	e, vehicle, ids := crewFixture(t, "Grizzly Bears", "Llanowar Elves")
	bears, elves := ids[0], ids[1]
	if e.Power(bears)+e.Power(elves) != 3 {
		t.Fatalf("precondition: candidate powers %d + %d, want 2 + 1 = 3", e.Power(bears), e.Power(elves))
	}
	submitChoices(t, e, abilityOption(t, e, vehicle, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.MinSum != 3 {
		t.Fatalf("crew tap election %+v, want KChoose MinSum 3", d)
	}
	// The bot must answer the tapcost election through the shared floor.
	in := newTestBot(7).answer(e, d)
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %+v failed Decision.Validate: %v", in, err)
	}
	sum := 0
	for _, c := range in.Choices {
		sum += d.Options[c].Value
	}
	if sum < 3 {
		t.Fatalf("bot answer %+v taps only %d power, below the crew floor of 3", in, sum)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("engine rejected the bot's crew answer: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(bears).Tapped || !e.G.Obj(elves).Tapped {
		t.Fatal("the bot's floor-exact answer did not tap both candidates")
	}
	if !e.IsCreature(vehicle) {
		t.Fatal("the bot-paid crew activation did not animate the vehicle")
	}
}

// TestMossbridgeTrollPaysWhenTotalPowerReachesTheFloor pins the paid
// direction of the shared withTotalPowerGE machinery on its corpus
// originator: a board whose other untapped creatures reach total power 10
// offers the ability, an election short of the floor is rejected, and a
// sufficient one taps exactly the chosen creatures and resolves the +20/+20.
func TestMossbridgeTrollPaysWhenTotalPowerReachesTheFloor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	trollCard, ok := reg.Lookup("Mossbridge Troll")
	if !ok {
		t.Fatal("corpus has no Mossbridge Troll")
	}
	extras := []*cards.Card{trollCard}
	for _, name := range []string{"Centaur Courser", "Centaur Courser", "Centaur Courser", "Grizzly Bears"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus has no %s", name)
		}
		extras = append(extras, c)
	}
	e := corpusEngine(t, reg, extras, nil)
	troll := moveByName(t, e, 0, "Mossbridge Troll", state.ZBattlefield)
	if o := e.G.Obj(troll); o.Zone != state.ZBattlefield || e.Power(troll) != 5 {
		t.Fatalf("precondition: troll power %d zone %v, want 5 on the battlefield", e.Power(troll), o.Zone)
	}
	// The tap cost's floor is a TOTAL over the other untapped creatures the
	// board offers, so the fixture must put them ON the battlefield -- seeding
	// them in the deck leaves the offer gate nothing to sum and the ability
	// withheld for the wrong reason.
	for _, name := range []string{"Centaur Courser", "Centaur Courser", "Centaur Courser", "Grizzly Bears"} {
		moveByName(t, e, 0, name, state.ZBattlefield)
	}
	e.pending = nil
	e.priorityRound()
	opt := abilityOption(t, e, troll, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.MinSum != 10 {
		t.Fatalf("Mossbridge tap election %+v, want KChoose MinSum 10 (the floor is now modelled, not failed closed)", d)
	}
	choice := []int{}
	sum := 0
	for _, o := range d.Options {
		choice = append(choice, o.Index)
		sum += o.Value
	}
	if sum < 10 {
		t.Fatalf("precondition: candidates total %d power, want >= 10", sum)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choice}); err != nil {
		t.Fatalf("legal Mossbridge election rejected: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.Power(troll); got != 25 {
		t.Fatalf("Mossbridge Troll power = %d, want 25 (5 + the +20/+20 the floor-gated cost paid for)", got)
	}
}

// TestParseCostCrewFloorStripsTheGroupPredicate pins the parser half: the
// Any form's withTotalPowerGE<N> predicate moves into CostPart.MinPower and
// out of the spec (so the per-object filter never sees the token it cannot
// evaluate), while the X form keeps it in the spec -- failing closed, as no
// corpus carrier combines a floor with an announced count.
func TestParseCostCrewFloorStripsTheGroupPredicate(t *testing.T) {
	t.Parallel()
	c := ParseCost("tapXType<Any/Creature.Other+withTotalPowerGE3>")
	if len(c.TapPermanent) != 1 {
		t.Fatalf("Any form parsed %+v, want one TapPermanent part", c.TapPermanent)
	}
	part := c.TapPermanent[0]
	if part.Dyn != "Any" || part.Spec != "Creature.Other" || part.MinPower != 3 {
		t.Fatalf("Any form parsed dyn=%q spec=%q floor=%d, want Any/Creature.Other/3", part.Dyn, part.Spec, part.MinPower)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("Any form reported Unknown %v", c.Unknown)
	}
	c = ParseCost("tapXType<X/Artifact+withTotalPowerGE2>")
	if len(c.TapPermanent) != 1 || c.TapPermanent[0].Spec != "Artifact+withTotalPowerGE2" || c.TapPermanent[0].MinPower != 0 {
		t.Fatalf("X form parsed %+v, want the predicate kept in the spec and floor 0", c.TapPermanent)
	}
}

// TestCrewActivationLimitRiderRidesTheMintedAbility pins the one corpus
// rider (Luxurious Locomotive's `K:Crew:1:ActivationLimit$ 1`, "activate
// only once each turn"): the rider is carried onto the minted SA verbatim,
// where the offer loop's ActivationLimit gate reads it.
func TestCrewActivationLimitRiderRidesTheMintedAbility(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	loco, ok := reg.Lookup("Luxurious Locomotive")
	if !ok {
		t.Fatal("corpus has no Luxurious Locomotive")
	}
	found := false
	for _, sa := range loco.Faces[0].Abilities {
		if sa.Params["Keyword"] == "Crew" {
			found = true
			if strings.TrimSpace(sa.Params["ActivationLimit"]) == "" {
				t.Fatalf("the crew expansion dropped Luxurious Locomotive's ActivationLimit$ rider: %+v", sa.Params)
			}
			if sa.Params["KeywordLine"] != "Crew:1:ActivationLimit$ 1" {
				t.Fatalf("KeywordLine %q, want the full keyword line", sa.Params["KeywordLine"])
			}
		}
	}
	if !found {
		t.Fatalf("Luxurious Locomotive has no minted crew ability; abilities = %+v", loco.Faces[0].Abilities)
	}
}

// TestCrewAbilityParsesOnTheRealVehicle pins the expansion's shape on the
// real card face: one minted Animate ability whose cost carries the floor
// token the machinery reads.
func TestCrewAbilityParsesOnTheRealVehicle(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	vehicle, ok := reg.Lookup("Avengers Quinjet")
	if !ok {
		t.Fatal("corpus has no Avengers Quinjet")
	}
	found := false
	for _, sa := range vehicle.Faces[0].Abilities {
		if sa.Params["Keyword"] != "Crew" {
			continue
		}
		found = true
		cost := ParseCost(sa.Params["Cost"])
		if len(cost.TapPermanent) != 1 || cost.TapPermanent[0].Dyn != "Any" ||
			cost.TapPermanent[0].Spec != "Creature.Other" || cost.TapPermanent[0].MinPower != 3 {
			t.Fatalf("Quinjet crew cost %+v, want Any/Creature.Other with floor 3", cost.TapPermanent)
		}
		if sa.Params["Defined"] != "Self" || sa.Params["Types"] != "Artifact,Creature" {
			t.Fatalf("Quinjet crew body %+v, want Defined$ Self Types$ Artifact,Creature", sa.Params)
		}
	}
	if !found {
		t.Fatalf("Avengers Quinjet has no minted crew ability; abilities = %+v", vehicle.Faces[0].Abilities)
	}
}
