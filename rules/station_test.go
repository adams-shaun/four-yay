package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// fb-20260916T022949Z-10e9a496: the Hearthhull feedback. The Station
// implementation itself landed with the mass-turn/new-mechanics merge
// (rules/station.go); these tests pin the integration on the reported card --
// Hearthhull, the Worldseed -- and close the gaps the landed tests left: the
// negative offer gates and the KChoose continuation across Engine.Clone.

// findStationOption returns the "station" option for spacecraft id in d, or -1.
func findStationOption(d *decision.Decision, id state.ObjID) int {
	for _, o := range d.Options {
		if o.Kind == "station" && o.Obj == id {
			return o.Index
		}
	}
	return -1
}

// TestStationHearthhullCloneContinuationReplays is the Hearthhull leaf of the
// reported feedback, at the real corpus card: the sorcery-window option
// appears, the KChoose offers another untapped creature (a summoning-sick
// one -- being tapped to pay another permanent's cost is not sickness's own
// tap-ability gate, CR 302.6), and never the Station permanent itself. The
// choice is taken while a KChoose is pending, so the continuation survives an
// Engine.Clone taken exactly there: the clone answers, the original does not
// move, feeding the original the same intent lands both on the same chain
// head, and the whole game replays byte-identically from the log.
func TestStationHearthhullCloneContinuationReplays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Hearthhull, the Worldseed"), card(t, bearSrc)},
		[]*cards.Card{})
	hearth := moveByName(t, e, 0, "Hearthhull, the Worldseed", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision (got %+v)", d)
	}
	idx := findStationOption(d, hearth)
	if idx < 0 {
		t.Fatalf("no station option for Hearthhull at sorcery speed: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit station: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no tap pick after the station option (got %+v)", d)
	}
	pick, srcOffered := -1, false
	for _, o := range d.Options {
		if o.Obj == bear {
			pick = o.Index
		}
		if o.Obj == hearth {
			srcOffered = true
		}
	}
	if srcOffered {
		t.Fatal("the Station permanent itself was offered as its own tap payer")
	}
	if pick < 0 {
		t.Fatalf("the summoning-sick Bear was not offered as a station tap: %+v", d.Options)
	}

	// Clone with the tap pick outstanding and check the continuation on the
	// clone: the copied engine must hold the same pending decision and answer
	// it to the same board.
	c := e.Clone()
	if diff := diffGames(e.G, c.G); diff != "" {
		t.Fatalf("clone differs from original with the Station pick pending:\n%s", diff)
	}
	cd := c.Pending()
	if cd == nil || cd.Kind != decision.KChoose || cd.Seq != d.Seq || len(cd.Options) != len(d.Options) {
		t.Fatalf("clone's pending Station pick differs: %+v vs %+v", cd, d)
	}

	if err := c.Submit(decision.Intent{Seq: cd.Seq, Player: cd.Player, Choices: []int{pick}}); err != nil {
		t.Fatalf("submit tap pick on the clone: %v", err)
	}
	if !c.G.Obj(bear).Tapped {
		t.Fatal("the clone did not tap the chosen creature")
	}
	if got := c.G.Obj(hearth).Counter("CHARGE"); got != 2 {
		t.Fatalf("the clone's Hearthhull has %d CHARGE counters, want 2 (the Bear's power)", got)
	}
	if e.G.Obj(bear).Tapped || e.G.Obj(hearth).Counter("CHARGE") != 0 {
		t.Fatal("answering on the clone moved the original engine")
	}

	// The original answers the same pick: identical chain heads prove the
	// clone copied every piece of engine state the continuation reads.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		t.Fatalf("submit tap pick on the original: %v", err)
	}
	if e.L.Head() != c.L.Head() {
		t.Fatalf("chain heads differ after lockstep: %s vs %s", e.L.Head(), c.L.Head())
	}
	// The board was reached through events: one Tap for the creature, one
	// CounterChange for the CHARGE counters.
	tapped, charged := false, false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Tap && ev.Obj == bear {
			tapped = true
		}
		if ev.Kind == events.CounterChange && ev.Obj == hearth && ev.Counter == "CHARGE" && ev.Amount == 2 {
			charged = true
		}
	}
	if !tapped || !charged {
		t.Fatalf("missing event-driven Station effects: tap=%v charge=%v", tapped, charged)
	}
	replayCheck(t, e, cfg)
	replayCheck(t, c, cfg)
}

// TestStationNegativeGates pins the offer gates the happy path cannot prove:
// with every other creature tapped the option is withheld (an unpayable
// station is never offered), with a spell on the stack the window is not
// sorcery speed so the option is withheld and returns when the stack drains,
// and an animated Hearthhull (a creature in its own right) is still never its
// own tap payer.
func TestStationNegativeGates(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Gate 1: no other untapped creature -- the Bear is tapped, so the
	// option must not be offered at all.
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Hearthhull, the Worldseed"), card(t, bearSrc)},
		[]*cards.Card{})
	hearth := moveByName(t, e, 0, "Hearthhull, the Worldseed", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Tap, Obj: bear})
	e.priorityRound()
	if d := e.Pending(); d != nil && findStationOption(d, hearth) >= 0 {
		t.Fatalf("station offered with no untapped creature to pay: %+v", d.Options)
	}

	// Gate 2: not sorcery speed. The Bear is cast from hand so the stack is
	// nonempty; the pending priority decision must not offer Station while
	// the spell resolves, and must offer it again once the stack drains.
	e2 := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Hearthhull, the Worldseed"), card(t, bearSrc)},
		[]*cards.Card{})
	hearth2 := moveByName(t, e2, 0, "Hearthhull, the Worldseed", state.ZBattlefield)
	moveByName(t, e2, 0, "Bear", state.ZHand)
	addMana(t, e2, 0, "GG")
	castNamed(t, e2, "Bear")
	if len(e2.G.Stack) == 0 {
		t.Fatal("fixture error: the Bear never reached the stack")
	}
	if d := e2.Pending(); d != nil && findStationOption(d, hearth2) >= 0 {
		t.Fatalf("station offered with a spell on the stack: %+v", d.Options)
	}
	passUntilStackEmpty(t, e2, 40)
	e2.priorityRound()
	d2 := e2.Pending()
	if d2 == nil {
		t.Fatal("no priority after the stack drained")
	}
	if findStationOption(d2, hearth2) < 0 {
		t.Fatalf("station did not return at sorcery speed after the stack drained: %+v", d2.Options)
	}

	// Gate 3: the Station permanent itself is never a candidate, even when a
	// live type-layer effect makes Hearthhull a creature and it is untapped.
	e3 := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Hearthhull, the Worldseed")}, nil)
	hearth3 := moveByName(t, e3, 0, "Hearthhull, the Worldseed", state.ZBattlefield)
	e3.AddContinuous(state.ContinuousEffect{Source: hearth3, Controller: 0,
		Affects: "Card.Self", Layer: state.LType, AddTypes: []string{"Creature"}, UntilEOT: true})
	if cands := e3.stationCandidates(0, hearth3); len(cands) != 0 {
		t.Fatalf("animated Hearthhull offered as its own tap payer: %v", cands)
	}
	e3.priorityRound()
	if d := e3.Pending(); d != nil && findStationOption(d, hearth3) >= 0 {
		t.Fatalf("station offered whose only creature candidate is the source: %+v", d.Options)
	}
}
