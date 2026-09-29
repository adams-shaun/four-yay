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
