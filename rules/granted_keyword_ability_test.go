package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// grantedSaddleScenario drives Jandor, Fortuned Traveler's layer-6
// `AddKeyword$ Saddle:2` grant end to end. Jandor's static reads
// `Affected$ Creature.Beast,... | AddKeyword$ Saddle:2`, so a Beast on the
// battlefield gains a Saddle 2 ability that NO printed face carries; the
// offer walk must synthesize it from the derived keyword line and the
// activation plumbing must resolve it, saddling the Beast.
func grantedSaddleScenario(t *testing.T) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	jandor := lookup(t, reg, "Jandor, Fortuned Traveler")
	beast, diags := cards.ParseBytes("fixture", []byte("Name:Test Beast\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("fixture parse: %+v", diags)
	}
	bear := lookup(t, reg, "Runeclaw Bear")
	e := corpusEngine(t, reg, []*cards.Card{jandor, beast, bear, bear}, nil)
	jid := moveByName(t, e, 0, "Jandor, Fortuned Traveler", state.ZBattlefield)
	bid := moveByName(t, e, 0, "Test Beast", state.ZBattlefield)
	bear1, bear2 := saddleBears(t, e)
	// Preconditions, each its own failure:
	//  - Jandor and the Beast are where the rule reads them;
	//  - the Beast prints no Saddle, so the offered ability can only be the
	//    granted one;
	//  - the grant really reached the Beast's derived keyword list;
	//  - the two payment bears are untapped with total power 2.
	if e.G.Obj(jid).Zone != state.ZBattlefield || e.G.Obj(bid).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Jandor/Beast zones %v/%v, want both battlefield",
			e.G.Obj(jid).Zone, e.G.Obj(bid).Zone)
	}
	if e.G.Obj(bid).Face().HasKeyword("Saddle") {
		t.Fatal("precondition: the Beast must print no Saddle, else the offered ability is not the granted one")
	}
	if param, ok := e.derivedKeywordParam(bid, "Saddle"); !ok || param != "2" {
		t.Fatalf("precondition: derived Saddle grant = (%q, %v), want (\"2\", true)", param, ok)
	}
	if e.Power(bid) != 2 {
		t.Fatalf("precondition: Beast power = %d, want 2", e.Power(bid))
	}
	_ = bear1
	_ = bear2

	e.pending = nil
	e.priorityRound()
	var saddle decision.Option
	count := 0
	for _, o := range e.Pending().Options {
		if o.Obj == bid && o.Keyword == "Saddle:2" {
			saddle = o
			count++
		}
	}
	if count != 1 {
		t.Fatalf("granted Saddle options = %d, want exactly 1", count)
	}
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: e.Pending().Player, Choices: []int{saddle.Index}}); err != nil {
		t.Fatal(err)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.MinSum != 2 {
		t.Fatalf("saddle payment decision = %+v, want KChoose MinSum 2", d)
	}
	var choices []int
	for _, o := range d.Options {
		if o.Obj == bear1 || o.Obj == bear2 {
			choices = append(choices, o.Index)
		}
	}
	if len(choices) != 2 {
		t.Fatalf("eligible bear choices %v, want 2", choices)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatal(err)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bid).SaddledTurn; got != e.G.Turn {
		t.Fatalf("SaddledTurn = %d, want turn %d", got, e.G.Turn)
	}
}

func TestGrantedSaddleAbilityIsOffered(t *testing.T) { grantedSaddleScenario(t) }

// CR 702.171a: saddle is an activated ability of the Mount. A layer-6
// `AddKeyword$ Saddle:2` grant (CR 613.1f) must expose that ability on the
// permanent that carries the derived keyword -- the grant is not an inert
// marker. This TestCR-named case puts the class in the conformance lane.
func TestCR702GrantedSaddleAbilityIsOffered(t *testing.T) { grantedSaddleScenario(t) }

// TestGrantedCrewAbilityIsOffered uses the REAL corpus carrier Kotori, Pilot
// Prodigy (`Affected$ Vehicle.YouCtrl | AddKeyword$ Crew:2`) plus a
// hand-authored Vehicle that prints no Crew. The granted Crew:2 ability must
// be offered for the Vehicle, and activating it must animate it into an
// artifact creature (CR 702.172a).
func TestGrantedCrewAbilityIsOffered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kotori := lookup(t, reg, "Kotori, Pilot Prodigy")
	vehicle, diags := cards.ParseBytes("fixture", []byte("Name:Test Vehicle\nTypes:Artifact Vehicle\nPT:3/3\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("fixture parse: %+v", diags)
	}
	bear := lookup(t, reg, "Runeclaw Bear")
	e := corpusEngine(t, reg, []*cards.Card{kotori, vehicle, bear}, nil)
	kid := moveByName(t, e, 0, "Kotori, Pilot Prodigy", state.ZBattlefield)
	vid := moveByName(t, e, 0, "Test Vehicle", state.ZBattlefield)
	bid := moveByName(t, e, 0, "Runeclaw Bear", state.ZBattlefield)
	// Preconditions, each its own failure:
	//  - Kotori (the granter) and the Vehicle are on the battlefield, where
	//    the source-zone default makes the static live;
	//  - the Vehicle prints no Crew, so the offered ability can only be the
	//    granted one;
	//  - the grant really reached the Vehicle's derived keyword list.
	if o := e.G.Obj(kid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Kotori = %+v, want on the battlefield", o)
	}
	if o := e.G.Obj(vid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: vehicle = %+v, want on the battlefield", o)
	}
	if e.G.Obj(vid).Face().HasKeyword("Crew") {
		t.Fatal("precondition: the vehicle must print no Crew, else the offered ability is not the granted one")
	}
	if param, ok := e.derivedKeywordParam(vid, "Crew"); !ok || param != "2" {
		t.Fatalf("precondition: derived Crew grant = (%q, %v), want (\"2\", true)", param, ok)
	}
	e.G.Obj(bid).SummonSick = false

	e.pending = nil
	e.priorityRound()
	var crew decision.Option
	count := 0
	for _, o := range e.Pending().Options {
		if o.Obj == vid && o.Keyword == "Crew:2" {
			crew = o
			count++
		}
	}
	if count != 1 {
		t.Fatalf("granted Crew:2 options = %d, want exactly 1", count)
	}
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: e.Pending().Player, Choices: []int{crew.Index}}); err != nil {
		t.Fatal(err)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.MinSum != 2 {
		t.Fatalf("crew payment decision = %+v, want KChoose MinSum 2", d)
	}
	var choices []int
	for _, o := range d.Options {
		if o.Obj == bid {
			choices = append(choices, o.Index)
		}
	}
	if len(choices) != 1 {
		t.Fatalf("eligible bear choices %v, want 1", choices)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatal(err)
	}
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(bid).Tapped {
		t.Fatal("the bear was not tapped as the crew cost")
	}
	// CR 702.172a: crewing animates the Vehicle into an artifact creature
	// until end of turn.
	if !e.IsCreature(vid) {
		t.Fatal("the granted Crew ability did not animate the vehicle into a creature")
	}
}

// TestGrantedSaddleAbilitySorcerySpeedGate pins CR 702.171a for the GRANTED
// form of Saddle: the synthesized body carries `SorcerySpeed$ True`
// (cards/kw_saddle.go), so Jandor's `AddKeyword$ Saddle:2` grant is offered
// on the empty-stack main phase but withheld while a spell is on the stack
// -- exactly as the printed `K:Saddle` offer is (TestSaddleSorcerySpeedGate).
// Preconditions are asserted separately: the option is offered before the
// Bolt is cast, so the negative assertion cannot pass because the option was
// never synthesized.
func TestGrantedSaddleAbilitySorcerySpeedGate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jandor := lookup(t, reg, "Jandor, Fortuned Traveler")
	bolt := lookup(t, reg, "Lightning Bolt")
	beast, diags := cards.ParseBytes("fixture", []byte("Name:Test Beast\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("fixture parse: %+v", diags)
	}
	e := corpusEngine(t, reg, append([]*cards.Card{jandor, beast, bolt}, saddleExtraCards(t, reg)...), nil)
	jid := moveByName(t, e, 0, "Jandor, Fortuned Traveler", state.ZBattlefield)
	bid := moveByName(t, e, 0, "Test Beast", state.ZBattlefield)
	saddleBears(t, e)
	if e.G.Obj(jid).Zone != state.ZBattlefield || e.G.Obj(bid).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Jandor/Beast zones %v/%v, want both battlefield",
			e.G.Obj(jid).Zone, e.G.Obj(bid).Zone)
	}
	if e.G.Obj(bid).Face().HasKeyword("Saddle") {
		t.Fatal("precondition: the Beast must print no Saddle, else the offered ability is not the granted one")
	}
	if param, ok := e.derivedKeywordParam(bid, "Saddle"); !ok || param != "2" {
		t.Fatalf("precondition: derived Saddle grant = (%q, %v), want (\"2\", true)", param, ok)
	}

	grantedSaddleOptions := func() int {
		n := 0
		for _, o := range e.Pending().Options {
			if o.Obj == bid && o.Keyword == "Saddle:2" {
				n++
			}
		}
		return n
	}

	// Offered on the empty-stack sorcery-speed window.
	e.pending = nil
	e.priorityRound()
	if n := grantedSaddleOptions(); n != 1 {
		t.Fatalf("precondition: granted Saddle options on the empty stack = %d, want 1", n)
	}

	// Put Lightning Bolt on the stack and confirm the granted Saddle is
	// withheld with it there.
	boltID := moveByName(t, e, 0, "Lightning Bolt", state.ZHand)
	addMana(t, e, 0, "R")
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == boltID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Lightning Bolt: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget && len(d.Options) > 0 {
		submitChoices(t, e, d.Options[0].Index)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("precondition: Lightning Bolt did not reach the stack")
	}
	if n := grantedSaddleOptions(); n != 0 {
		t.Fatalf("granted Saddle was offered %d time(s) with a spell on the stack; CR 702.171a says only as a sorcery", n)
	}
}

// TestGrantedKeywordAbilityActivatorGate pins CR 602.2a for a GRANTED
// keyword ability: the battlefield is walked for every seat's priority, so
// the activator gate -- no Activator$ means the source's controller, and
// only them -- must withhold the option from anyone who does not control
// the permanent carrying the derived keyword. The probe uses granted Crew
// rather than granted Saddle because Crew carries no `SorcerySpeed$`
// (cards/kw_crew.go): seat 1, the Vehicle's controller, holds priority
// during seat 0's main phase, so the positive control -- the controller IS
// offered the option -- is legal in the same scenario where the opponent
// must be withheld. (A Saddle probe cannot carry that control: Saddle is
// sorcery-speed, so a non-active seat is withheld by the CR 302.1/602.5a
// window, not by the activator gate.) Seat 0 is the active player and holds
// the bears the seat-0 election would pay with, so the cost-satisfiability
// gate cannot mask the gap.
func TestGrantedKeywordAbilityActivatorGate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kotori := lookup(t, reg, "Kotori, Pilot Prodigy")
	vehicle, diags := cards.ParseBytes("fixture", []byte("Name:Test Vehicle\nTypes:Artifact Vehicle\nPT:3/3\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("fixture parse: %+v", diags)
	}
	bear := lookup(t, reg, "Runeclaw Bear")
	e := corpusEngine(t, reg, []*cards.Card{bear, bear}, []*cards.Card{kotori, vehicle, bear, bear})
	kid := moveByName(t, e, 1, "Kotori, Pilot Prodigy", state.ZBattlefield)
	vid := moveByName(t, e, 1, "Test Vehicle", state.ZBattlefield)
	for _, p := range []state.PlayerID{0, 1} {
		for n := 0; n < 2; n++ {
			id := moveByName(t, e, p, "Runeclaw Bear", state.ZBattlefield)
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: seat %d's bear %d is not on the battlefield", p, id)
			}
			e.G.Obj(id).SummonSick = false
		}
	}
	// Preconditions, each its own failure: Kotori (the granter) and the
	// Vehicle are on seat 1's battlefield, where the source-zone default
	// makes the static live; the Vehicle prints no Crew, so the offered
	// ability can only be the granted one; the grant really reached the
	// Vehicle's derived keyword list; and the controller is where the
	// activator gate reads it.
	if o := e.G.Obj(kid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Kotori = %+v, want on seat 1's battlefield", o)
	}
	if o := e.G.Obj(vid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: vehicle = %+v, want on seat 1's battlefield", o)
	}
	if e.G.Obj(vid).Face().HasKeyword("Crew") {
		t.Fatal("precondition: the vehicle must print no Crew, else the offered ability is not the granted one")
	}
	if param, ok := e.derivedKeywordParam(vid, "Crew"); !ok || param != "2" {
		t.Fatalf("precondition: derived Crew grant = (%q, %v), want (\"2\", true)", param, ok)
	}
	if e.controllerOf(vid) != 1 {
		t.Fatalf("precondition: vehicle controller = %d, want 1", e.controllerOf(vid))
	}

	// Seat 0 (the active player, an opponent of the Vehicle's controller)
	// holds priority first. The granted Crew option must be withheld from it.
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Player != 0 {
		t.Fatalf("precondition: pending decision %+v, want seat 0's priority", d)
	}
	for _, o := range d.Options {
		if o.Obj == vid && o.Keyword == "Crew:2" {
			t.Fatalf("CR 602.2a: seat 0 was offered the granted Crew option on seat 1's Vehicle: %+v", o)
		}
	}
	// Positive control: seat 1, the Vehicle's controller, IS offered the
	// granted Crew, so the withholding above is the activator gate and not a
	// blanket suppression of the synthesized option.
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "pass" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("seat 0's priority decision has no pass option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d1 := e.Pending()
	if d1 == nil || d1.Player != 1 {
		t.Fatalf("precondition: after seat 0 passes, pending = %+v, want seat 1's priority", d1)
	}
	n := 0
	for _, o := range d1.Options {
		if o.Obj == vid && o.Keyword == "Crew:2" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("positive control: seat 1 was offered %d granted Crew option(s), want 1", n)
	}
}
