package rules

// The set-mechanic / keyword-action trigger modes (task triage-478c51d1).
//
// Nine Forge `T:Mode$` trigger modes that Standard cards print had no entry
// in the trigger registry, so their abilities silently never fired. This file
// proves the five Phase-1 modes fire off the event the mode names:
//
//   Crewed          -- events.Crew (rules/pay's Crew cost record)
//   Saddled         -- events.Saddle (the new mirror record)
//   BecomesSaddled  -- events.AlterAttribute "Saddled" (the K:Saddle grant)
//   BecomesPlotted  -- events.AlterAttribute "Plotted" (the K:Plot action)
//   SacrificedOnce  -- the ordinary sacrifice marker, batched once per action
//
// The corpus carrier census at the bottom ratchets every `.cards/cardsfolder`
// file per mode, so a corpus-pin change that adds a carrier fails loudly
// rather than silently leaving the new card's trigger unregistered.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// crewElection submits the Vehicle's crew ability and returns the KChoose tap
// election, asserting the tap-any-number shape the crew cost carries.
func crewElection(t *testing.T, e *Engine, vehicle state.ObjID) *decision.Decision {
	t.Helper()
	e.pending = nil
	e.priorityRound()
	opt := abilityOption(t, e, vehicle, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending decision %+v, want the crew tap election (KChoose)", d)
	}
	return d
}

// TestCrewedFiresOnCrewAction drives the real corpus Crewed carrier end to
// end: Reckless Velocitaur ("Whenever this creature saddles a Mount or crews
// a Vehicle during your main phase, that Mount or Vehicle gets +2/+0 and
// gains trample until end of turn"). Tapping the Velocitaur itself to crew
// Avengers Quinjet must emit the Crew marker the mode reads and pump the
// Vehicle. The assertion is +2/+0 and trample rather than flying, because
// Avengers Quinjet prints K:Flying already (a +0/+0/flying pump would be
// invisible).
func TestCrewedFiresOnCrewAction(t *testing.T) {
	t.Parallel()
	e, vehicle, ids := crewFixture(t, "Reckless Velocitaur", "Grizzly Bears")
	velocitaur := ids[0]
	// Preconditions: the Velocitaur is on the battlefield with the power to
	// crew the Quinjet alone, and the Vehicle has not yet been pumped.
	if o := e.G.Obj(velocitaur); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Reckless Velocitaur is not on the battlefield")
	}
	if e.Power(velocitaur) != 3 {
		t.Fatalf("precondition: Reckless Velocitaur power = %d, want 3 (enough to crew the Quinjet alone)", e.Power(velocitaur))
	}
	if e.Power(vehicle) != 4 || e.HasKeyword(vehicle, "Trample") {
		t.Fatalf("precondition: Avengers Quinjet is %d/trample=%v, want 4/no trample", e.Power(vehicle), e.HasKeyword(vehicle, "Trample"))
	}
	d := crewElection(t, e, vehicle)
	idx := -1
	for _, o := range d.Options {
		if o.Obj == velocitaur {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("precondition: Reckless Velocitaur is not offered as a crew candidate: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 40)
	if !e.G.Obj(velocitaur).Tapped {
		t.Fatal("precondition: Reckless Velocitaur was not tapped as the crew cost (no crew action happened)")
	}
	if e.Power(vehicle) != 6 || !e.HasKeyword(vehicle, "Trample") {
		t.Fatalf("Mode$ Crewed never fired: the crewed Vehicle is %d/trample=%v, want 6/trample", e.Power(vehicle), e.HasKeyword(vehicle, "Trample"))
	}
	// The marker really was a Crew record for the Velocitaur (belt and braces:
	// the pump above could in principle come from another source).
	if countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Crew && ev.Obj == velocitaur }) == 0 {
		t.Fatal("no events.Crew marker named the crewing Reckless Velocitaur")
	}
}

// TestCrewedValidCrewFilterDirection proves the ValidCrew$ filter names the
// CREWING creature, not the Vehicle: a crew action paid entirely by a
// different creature must NOT fire Reckless Velocitaur's Crewed trigger.
func TestCrewedValidCrewFilterDirection(t *testing.T) {
	t.Parallel()
	e, vehicle, ids := crewFixture(t, "Reckless Velocitaur", "Grizzly Bears", "Llanowar Elves")
	velocitaur, bears, elves := ids[0], ids[1], ids[2]
	// Precondition the assertion depends on: the compared ids differ, and
	// the two other creatures alone reach the crew cost without the pilot.
	if velocitaur == bears || velocitaur == elves {
		t.Fatal("precondition: the pilot id equals a candidate id; the filter comparison is vacuous")
	}
	if e.Power(bears)+e.Power(elves) < 3 {
		t.Fatalf("precondition: bears+elves power = %d, want >= 3 to crew without the pilot", e.Power(bears)+e.Power(elves))
	}
	if e.Power(vehicle) != 4 || e.HasKeyword(vehicle, "Trample") {
		t.Fatalf("precondition: Avengers Quinjet is %d/trample=%v, want 4/no trample", e.Power(vehicle), e.HasKeyword(vehicle, "Trample"))
	}
	d := crewElection(t, e, vehicle)
	var choices []int
	for _, o := range d.Options {
		if o.Obj == bears || o.Obj == elves {
			choices = append(choices, o.Index)
		}
	}
	if len(choices) != 2 {
		t.Fatalf("precondition: expected exactly the two non-pilot candidates, got %+v", d.Options)
	}
	submitChoices(t, e, choices...)
	passUntilStackEmpty(t, e, 40)
	if !e.G.Obj(bears).Tapped || !e.G.Obj(elves).Tapped {
		t.Fatal("precondition: the non-pilot crew action did not tap the candidates")
	}
	if e.G.Obj(velocitaur).Tapped {
		t.Fatal("precondition: the pilot was tapped; this is not the non-pilot crew case")
	}
	if e.Power(vehicle) != 4 || e.HasKeyword(vehicle, "Trample") {
		t.Fatalf("Mode$ Crewed fired although a different creature crewed: the Vehicle is %d/trample=%v, want 4/no trample", e.Power(vehicle), e.HasKeyword(vehicle, "Trample"))
	}
	// Positive control in the same test: a matching crew (the pilot itself)
	// must fire, so this test fails when the Crewed registration is removed
	// rather than passing vacuously on an absent trigger.
	d2 := crewElection(t, e, vehicle)
	pilotIdx := -1
	for _, o := range d2.Options {
		if o.Obj == velocitaur {
			pilotIdx = o.Index
		}
	}
	if pilotIdx < 0 {
		t.Fatalf("positive control: the untapped pilot is not offered as a crew candidate: %+v", d2.Options)
	}
	submitChoices(t, e, pilotIdx)
	passUntilStackEmpty(t, e, 40)
	if e.Power(vehicle) != 6 || !e.HasKeyword(vehicle, "Trample") {
		t.Fatalf("positive control: a matching crew did not fire Crewed (Vehicle %d/trample=%v)", e.Power(vehicle), e.HasKeyword(vehicle, "Trample"))
	}
}

// TestSaddledFiresOnSaddleAction drives the real corpus Saddled carrier:
// Canyon Vaulter saddles Stubborn Burrowfiend (Saddle 2) by tapping itself,
// so the Mount must gain Flying. The ValidCrew$ Card.Self filter names the
// SADDLING creature (ev.Obj), exactly as the Crewed half does.
func TestSaddledFiresOnSaddleAction(t *testing.T) {
	t.Parallel()
	e := saddleCarrierEngine(t)
	mount, vaulter := saddleMountAndVaulter(t, e)
	if e.HasKeyword(mount, "Flying") {
		t.Fatal("precondition: the Mount already has Flying")
	}
	if o := e.G.Obj(vaulter); o == nil || o.Zone != state.ZBattlefield || e.Power(vaulter) < 2 {
		t.Fatalf("precondition: Canyon Vaulter is not a power>=2 saddler on the battlefield")
	}
	d := saddleElection(t, e, mount, 2)
	saddlePick(t, e, d, vaulter)
	passUntilStackEmpty(t, e, 40)
	if !e.G.Obj(vaulter).Tapped {
		t.Fatal("precondition: Canyon Vaulter was not tapped as the saddle cost")
	}
	if !e.HasKeyword(mount, "Flying") {
		t.Fatal("Mode$ Saddled never fired: the Mount the Vaulter saddled did not gain Flying")
	}
	if countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Saddle && ev.Obj == vaulter }) == 0 {
		t.Fatal("no events.Saddle marker named the saddling Canyon Vaulter")
	}
}

// TestSaddledValidCrewFilterDirection is the mirror of the Crewed filter
// test: a saddle action paid entirely by a different creature must not fire
// Canyon Vaulter's Saddled trigger.
func TestSaddledValidCrewFilterDirection(t *testing.T) {
	t.Parallel()
	e := saddleCarrierEngine(t)
	mount, vaulter := saddleMountAndVaulter(t, e)
	bear := moveByName(t, e, 0, "Runeclaw Bear", state.ZBattlefield)
	// Precondition: the bear is a different object with enough power to
	// saddle the Mount alone.
	if bear == vaulter {
		t.Fatal("precondition: the saddling bear id equals the Vaulter id")
	}
	if e.Power(bear) < 2 {
		t.Fatalf("precondition: Runeclaw Bear power = %d, want >= 2", e.Power(bear))
	}
	d := saddleElection(t, e, mount, 2)
	saddlePick(t, e, d, bear)
	passUntilStackEmpty(t, e, 40)
	if !e.G.Obj(bear).Tapped {
		t.Fatal("precondition: the bear was not tapped as the saddle cost")
	}
	if e.G.Obj(vaulter).Tapped {
		t.Fatal("precondition: the Vaulter was tapped; this is not the non-Vaulter saddle case")
	}
	if e.HasKeyword(mount, "Flying") {
		t.Fatal("Mode$ Saddled fired although a different creature saddled: ValidCrew$ Card.Self must filter the saddleER")
	}
	// Positive control in the same test: a matching saddle (the pilot itself)
	// must fire, so this test fails when the Saddled registration is removed
	// rather than passing vacuously on an absent trigger.
	d2 := saddleElection(t, e, mount, 2)
	saddlePick(t, e, d2, vaulter)
	passUntilStackEmpty(t, e, 40)
	if !e.HasKeyword(mount, "Flying") {
		t.Fatal("positive control: a matching saddle did not fire Saddled")
	}
}

// saddleCarrierEngine builds a corpus engine with Canyon Vaulter, Stubborn
// Burrowfiend and two Runeclaw Bears in seat 0's deck. When tapBear is true
// the caller intends to saddle by tapping a bear; otherwise the only
// saddling candidate the fixture guarantees is the Vaulter.
func saddleCarrierEngine(t *testing.T) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	extras := []*cards.Card{
		lookup(t, reg, "Canyon Vaulter"),
		lookup(t, reg, "Stubborn Burrowfiend"),
		lookup(t, reg, "Runeclaw Bear"),
		lookup(t, reg, "Runeclaw Bear"),
	}
	return corpusEngine(t, reg, extras, nil)
}

// saddleMountAndVaulter moves the Mount and the Vaulter onto seat 0's
// battlefield, clears the Vaulter's summoning-sick flag (saddle taps are
// costs, but clearing it keeps the fixture honest) and returns their ids.
func saddleMountAndVaulter(t *testing.T, e *Engine) (mount, vaulter state.ObjID) {
	t.Helper()
	mount = moveByName(t, e, 0, "Stubborn Burrowfiend", state.ZBattlefield)
	vaulter = moveByName(t, e, 0, "Canyon Vaulter", state.ZBattlefield)
	for _, id := range []state.ObjID{mount, vaulter} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d is not on the battlefield", id)
		}
		e.G.Obj(id).SummonSick = false
	}
	return mount, vaulter
}

// saddlePick selects one objs' option in the saddle election and submits it.
func saddlePick(t *testing.T, e *Engine, d *decision.Decision, obj state.ObjID) {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("saddle election offered no option for object %d: %+v", obj, d.Options)
}

// TestBecomesSaddledSelfTrigger drives Stubborn Burrowfiend: its own Saddle 2
// ability saddles IT, which must fire its BecomesSaddled "for the first time
// each turn" trigger. The body mills two cards, so a library of known
// non-creature cards makes the graveyard growth an exact witness.
func TestBecomesSaddledSelfTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	burrow := corpusAlternativeCard(t, "Stubborn Burrowfiend")
	e := handEngine(t)
	mount := addToBattlefield(t, e, burrow, 0)
	b1 := addToBattlefield(t, e, lookup(t, reg, "Runeclaw Bear"), 0)
	b2 := addToBattlefield(t, e, lookup(t, reg, "Runeclaw Bear"), 0)
	for _, id := range []state.ObjID{mount, b1, b2} {
		e.G.Obj(id).SummonSick = false
	}
	// Preconditions: the Mount is unsaddled on the battlefield and the two
	// bears together reach its Saddle 2 cost.
	if e.G.Obj(mount).SaddledTurn != 0 {
		t.Fatal("precondition: Stubborn Burrowfiend is already saddled")
	}
	if e.Power(b1)+e.Power(b2) < 2 {
		t.Fatalf("precondition: the two bears' total power = %d, want >= 2", e.Power(b1)+e.Power(b2))
	}
	before := len(e.G.Zone(state.ZGraveyard, 0))
	d := saddleElection(t, e, mount, 2)
	var choices []int
	for _, o := range d.Options {
		if o.Obj == b1 || o.Obj == b2 {
			choices = append(choices, o.Index)
		}
	}
	if len(choices) != 2 {
		t.Fatalf("precondition: saddle election offered %d bears, want 2: %+v", len(choices), d.Options)
	}
	submitChoices(t, e, choices...)
	passUntilStackEmpty(t, e, 40)
	// The designation is the trigger's precondition.
	if got := e.G.Obj(mount).SaddledTurn; got != e.G.Turn {
		t.Fatalf("precondition: mount SaddledTurn = %d, want the current turn %d", got, e.G.Turn)
	}
	// The witness: the trigger's Mill 2 ran (the library is Mountains).
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != before+2 {
		t.Fatalf("Mode$ BecomesSaddled never fired: graveyard grew by %d, want 2 (the Mill 2 body)", got-before)
	}
}

// TestBecomesSaddledFirstTimeOncePerTurn proves FirstTimeSaddled$ True: a
// SECOND saddle of the same Mount in one turn must not fire the trigger
// again. The body mills two cards, so the graveyard growth is the witness:
// exactly one mill (2 cards) across two saddle actions, not two (4 cards).
func TestBecomesSaddledFirstTimeOncePerTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	burrow := corpusAlternativeCard(t, "Stubborn Burrowfiend")
	e := handEngine(t)
	mount := addToBattlefield(t, e, burrow, 0)
	bears := make([]state.ObjID, 4)
	for i := range bears {
		bears[i] = addToBattlefield(t, e, lookup(t, reg, "Runeclaw Bear"), 0)
		e.G.Obj(bears[i]).SummonSick = false
	}
	e.G.Obj(mount).SummonSick = false
	// Precondition: four separate bears so the Mount can pay Saddle 2 twice.
	if e.G.Obj(bears[0]).ID == e.G.Obj(bears[1]).ID {
		t.Fatal("precondition: the bears are not distinct objects")
	}
	if n := len(e.G.Zone(state.ZBattlefield, 0)); n != 5 {
		t.Fatalf("precondition: battlefield holds %d objects, want 5 (mount + four bears)", n)
	}
	before := len(e.G.Zone(state.ZGraveyard, 0))

	saddleTwice := func(first, second state.ObjID) {
		t.Helper()
		d := saddleElection(t, e, mount, 2)
		var choices []int
		for _, o := range d.Options {
			if o.Obj == first || o.Obj == second {
				choices = append(choices, o.Index)
			}
		}
		if len(choices) != 2 {
			t.Fatalf("saddle election offered %d of the two intended bears: %+v", len(choices), d.Options)
		}
		submitChoices(t, e, choices...)
		passUntilStackEmpty(t, e, 40)
	}
	saddleTwice(bears[0], bears[1])
	if got := e.G.Obj(mount).SaddledTurn; got != e.G.Turn {
		t.Fatalf("precondition: first saddle did not stamp SaddledTurn (got %d, turn %d)", got, e.G.Turn)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != before+2 {
		t.Fatalf("first saddle: graveyard grew by %d, want 2 (one Mill 2)", got-before)
	}
	saddleTwice(bears[2], bears[3])
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != before+2 {
		t.Fatalf("FirstTimeSaddled$: graveyard grew by %d across two saddles, want 2 (the second must not fire)", got-before)
	}
}

// TestBecomesPlottedSelfTrigger plots Aloe Alchemist, whose BecomesPlotted
// trigger ("When CARDNAME becomes plotted, target creature gets +3/+2 and
// gains trample until end of turn") must fire from exile and pump the chosen
// creature. This also pins the zone timing the mode depends on: the exile
// MoveZone precedes the AlterAttribute grant.
func TestBecomesPlottedSelfTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	aloe := corpusAlternativeCard(t, "Aloe Alchemist")
	e := handEngine(t, aloe)
	aloeID := e.G.Zone(state.ZHand, 0)[0]
	bear := addToBattlefield(t, e, lookup(t, reg, "Runeclaw Bear"), 0)
	// Preconditions: the plotted carrier is in hand, the target is on the
	// battlefield with printed 2/2, and the plot action is offered.
	if o := e.G.Obj(aloeID); o.Zone != state.ZHand {
		t.Fatalf("precondition: Aloe Alchemist is in %s, want hand", o.Zone)
	}
	if got := e.G.Obj(bear).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: the target bear is in %s, want battlefield", got)
	}
	if e.Power(bear) != 2 || e.Toughness(bear) != 2 {
		t.Fatalf("precondition: target bear is %d/%d, want 2/2", e.Power(bear), e.Toughness(bear))
	}
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MG] = 1
	if findMode(e.legalActions(0), "plot") < 0 {
		t.Fatalf("precondition: Aloe Alchemist was never offered a plot action: %+v", e.legalActions(0))
	}
	castMode(t, e, aloeID, "plot")
	if o := e.G.Obj(aloeID); o.Zone != state.ZExile || o.PlottedTurn != e.G.Turn {
		t.Fatalf("precondition: plot did not exile with the designation: zone=%s plottedTurn=%d", o.Zone, o.PlottedTurn)
	}
	// The trigger is queued by the AlterAttribute emit and pushed to the
	// stack at the next priority round.
	e.priorityRound()
	if len(e.G.Stack) == 0 {
		t.Fatal("Mode$ BecomesPlotted never fired: nothing went on the stack after the plot action")
	}
	drainSetMechanic(t, e, func(d *decision.Decision) []int {
		if d.Kind == decision.KTarget {
			for _, o := range d.Options {
				if o.Obj == bear {
					return []int{o.Index}
				}
			}
			t.Fatalf("precondition: the BecomesPlotted trigger offered no bear target: %+v", d.Options)
		}
		return firstOption(d)
	})
	if e.Power(bear) != 5 || e.Toughness(bear) != 4 {
		t.Fatalf("Mode$ BecomesPlotted never resolved: target bear is %d/%d, want 5/4 (+3/+2)", e.Power(bear), e.Toughness(bear))
	}
	if !e.HasKeyword(bear, "Trample") {
		t.Fatal("Mode$ BecomesPlotted never resolved: the target did not gain trample")
	}
}

// TestCamelliaSacrificedOnceFiresOncePerAction pins the "one or more" cadence:
// Camellia, the Seedmiser's SacrificedOnce trigger ("Whenever you sacrifice
// one or more Foods, create a 1/1 green Squirrel") must fire exactly ONCE for
// an action that sacrifices two Foods, not once per permanent.
func TestCamelliaSacrificedOnceFiresOncePerAction(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	camellia := lookup(t, reg, "Camellia, the Seedmiser")
	food := lookup(t, reg, "Bagel and Schmear")
	feast := card(t, "Name:Feast Twice\nManaCost:0\nTypes:Sorcery\n"+
		"A:SP$ Sacrifice | SacValid$ Food | Amount$ 2\nOracle:x\n")
	e := corpusEngine(t, reg, []*cards.Card{camellia, food, food, feast}, nil)
	camID := moveByName(t, e, 0, "Camellia, the Seedmiser", state.ZBattlefield)
	f1 := moveByName(t, e, 0, "Bagel and Schmear", state.ZBattlefield)
	f2 := moveByName(t, e, 0, "Bagel and Schmear", state.ZBattlefield)
	feastID := moveByName(t, e, 0, "Feast Twice", state.ZHand)
	if o := e.G.Obj(camID); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Camellia is in %s, want battlefield", o.Zone)
	}
	if f1 == f2 {
		t.Fatal("precondition: the two Foods are the same object")
	}
	for _, id := range []state.ObjID{f1, f2} {
		if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Food %d is in %s, want battlefield", id, o.Zone)
		}
	}
	if n := countTokensNamed(t, e, "Squirrel Token"); n != 0 {
		t.Fatalf("precondition: %d Squirrels already on the battlefield", n)
	}

	// Cast the free sorcery that sacrifices two Foods in one action.
	e.pending = nil
	e.priorityRound()
	castIdx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == feastID {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("precondition: Feast Twice is not offered as a cast: %+v", e.Pending().Options)
	}
	submitChoices(t, e, castIdx)
	drainSetMechanic(t, e, func(d *decision.Decision) []int {
		if d.Kind == decision.KChoose {
			// The api:Sacrifice selection (when the engine asks): take both
			// Foods. A KChoose with no Food option is some other ask; fall
			// through to the deterministic first option.
			var out []int
			for _, o := range d.Options {
				if o.Obj == f1 || o.Obj == f2 {
					out = append(out, o.Index)
				}
			}
			if len(out) == 2 {
				return out
			}
		}
		return firstOption(d)
	})
	for _, id := range []state.ObjID{f1, f2} {
		if got := e.G.Obj(id).Zone; got == state.ZBattlefield {
			t.Fatalf("precondition: Food %d was not sacrificed (still on the battlefield)", id)
		}
	}
	if n := countTokensNamed(t, e, "Squirrel Token"); n != 1 {
		t.Fatalf("Mode$ SacrificedOnce fired %d times for one two-Food action, want exactly 1", n)
	}
}

// addToBattlefield adds c to player p's battlefield and returns its id.
func addToBattlefield(t *testing.T, e *Engine, c *cards.Card, p state.PlayerID) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	o.Zone = state.ZBattlefield
	zone := append(append([]state.ObjID{}, e.G.Zone(state.ZBattlefield, p)...), o.ID)
	e.G.SetZone(state.ZBattlefield, p, zone)
	return o.ID
}

// firstOption answers a decision with its first option (or nothing when it
// has none), the deterministic default the other drain helpers use.
func firstOption(d *decision.Decision) []int {
	if len(d.Options) == 0 {
		return nil
	}
	return []int{d.Options[0].Index}
}

// drainSetMechanic drives the stack and the queued triggers to empty,
// passing priority and answering each non-priority ask with pick. It returns
// at the first priority decision with an empty stack and trigger queue, so
// the caller can assert on the settled board without advancing a step.
func drainSetMechanic(t *testing.T, e *Engine, pick func(*decision.Decision) []int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
				e.Advance()
				continue
			}
			t.Fatalf("no decision while draining the stack (depth %d, queued %d)", len(e.G.Stack), len(e.pendingTriggers))
		}
		if d.Kind == decision.KPriority && len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
			return
		}
		if d.Kind == decision.KPriority {
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			continue
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: pick(d)}); err != nil {
			t.Fatalf("submit %s ask: %v", d.Kind, err)
		}
	}
	t.Fatal("set-mechanic drain did not finish within the answer budget")
}

// TestSetMechanicTriggerModeCarrierCensus ratchets every `.cards/cardsfolder`
// file per Phase-1 mode. Only filenames are committed (never card script
// text, which is GPL-3.0). A missing corpus fails loudly: the walk returns an
// error on the absent directory rather than silently matching nothing.
func TestSetMechanicTriggerModeCarrierCensus(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", ".cards", "cardsfolder")
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, root+string(filepath.Separator))] = string(b)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ mode, list string }{
		{"SacrificedOnce", "testdata/set-mechanic-carriers/sacrificed-once.txt"},
		{"Crewed", "testdata/set-mechanic-carriers/crewed.txt"},
		{"Saddled", "testdata/set-mechanic-carriers/saddled.txt"},
		{"BecomesSaddled", "testdata/set-mechanic-carriers/becomes-saddled.txt"},
		{"BecomesPlotted", "testdata/set-mechanic-carriers/becomes-plotted.txt"},
		{"Forage", "testdata/set-mechanic-carriers/forage.txt"},
		{"ManifestDread", "testdata/set-mechanic-carriers/manifest-dread.txt"},
		{"CollectEvidence", "testdata/set-mechanic-carriers/collect-evidence.txt"},
	} {
		b, err := os.ReadFile(filepath.Join(".", tc.list))
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line != "" {
				want = append(want, line)
			}
		}
		var got []string
		for name, content := range files {
			if hasTriggerModeLine(content, tc.mode) {
				got = append(got, name)
			}
		}
		sort.Strings(got)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("Mode$ %s corpus carriers changed: got %d, pinned %d\n got: %v\nwant: %v",
				tc.mode, len(got), len(want), got, want)
		}
	}
}

// hasTriggerModeLine reports whether src contains a `T:Mode$ <mode>` line for
// exactly mode (the token is followed by a space or end of line, so "Saddled"
// never matches "BecomesSaddled").
func hasTriggerModeLine(src, mode string) bool {
	needle := "T:Mode$ " + mode
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, needle) {
			continue
		}
		rest := line[len(needle):]
		if rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '|' {
			return true
		}
	}
	return false
}
