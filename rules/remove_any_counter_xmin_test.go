package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestParseRemoveAnyCounterXMin pins the X1+ count grammar of the two
// counter-removal cost heads: `RemoveAnyCounter<X1+/...>` and
// `SubCounter<X1+/...>` parse as ONE announced SubCounter part (no phantom
// generic pip, nothing Unknown) with the bound folded into Cost.XMin.
func TestParseRemoveAnyCounterXMin(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"RemoveAnyCounter<X1+/P1P1/Creature>",
		"SubCounter<X1+/DREAM/NICKNAME>",
	} {
		c := ParseCost(input)
		if len(c.Unknown) != 0 || c.Generic != 0 || len(c.SubCounter) != 1 || !c.SubCounter[0].Announced || c.XMin != 1 {
			t.Errorf("ParseCost(%q) = generic %d XMin %d parts %#v unknown %q", input, c.Generic, c.XMin, c.SubCounter, c.Unknown)
		}
	}
	// The plain announced-X and fixed-count forms are untouched.
	for _, input := range []string{"RemoveAnyCounter<X/P1P1/Creature>", "SubCounter<X/DREAM/NICKNAME>", "RemoveAnyCounter<2/Any/Creature>"} {
		c := ParseCost(input)
		if len(c.Unknown) != 0 || len(c.SubCounter) != 1 {
			t.Errorf("ParseCost(%q) = generic %d XMin %d parts %#v unknown %q", input, c.Generic, c.XMin, c.SubCounter, c.Unknown)
		}
	}
}

const xMinOozeSrc = "Name:XMin Ooze\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ GainLife | Cost$ T RemoveAnyCounter<X1+/P1P1/Creature> | Defined$ You | LifeAmount$ 2 | SpellDescription$ Gain 2 life.\nOracle:x\n"
const xMinBearSrc = "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// xMinOozeEngine deals seat 0 the XMin1 ooze-shaped source and a Bear, moves
// both onto the battlefield, and (when withCounters is set) places that many
// +1/+1 counters on the Bear. It returns the engine, config and the source's
// id; the Bear's id travels out too for the counter assertions.
func xMinOozeEngine(t *testing.T, seed uint64, withCounters int) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	e, cfg, srcID := newFixtureDeck(t, seed, xMinOozeSrc, xMinBearSrc)
	bearID := putCreature(t, e, 0, xMinBearSrc)
	if withCounters > 0 {
		e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: int32(withCounters)})
	}
	if got := e.G.Obj(bearID).Counter("P1P1"); got != int32(withCounters) {
		t.Fatalf("setup: Bear carries %d +1/+1 counters, want %d", got, withCounters)
	}
	if e.G.Obj(bearID).Zone != state.ZBattlefield || e.G.Obj(srcID).Zone != state.ZHand {
		t.Fatalf("setup: Bear in %s, source in %s", e.G.Obj(bearID).Zone, e.G.Obj(srcID).Zone)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: srcID, From: e.G.Obj(srcID).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	return e, cfg, srcID, bearID
}

// TestRemoveAnyCounterXMinAskFloorBinds drives the X1+ ask end to end on an
// inline fixture: the announced counter-removal cost's X ask offers exactly
// the counter-count values DOWN TO the X1+ floor of 1 -- never X = 0 -- and
// the chosen announcement settles by removing that many counters.
func TestRemoveAnyCounterXMinAskFloorBinds(t *testing.T) {
	t.Parallel()
	e, cfg, srcID, bearID := xMinOozeEngine(t, 83, 2)
	opt := abilityOption(t, e, srcID, 0)
	startLife := e.G.Players[0].Life
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("X options = %+v, want exactly X=1 and X=2 (one per counter, floor at 1)", d.Options)
	}
	for _, o := range d.Options {
		if o.Kind != "x" || (o.Amount != 1 && o.Amount != 2) {
			t.Fatalf("X options = %+v, want only X=1 and X=2 -- the X1+ floor binds, no X=0", d.Options)
		}
	}
	submitChoices(t, e, d.Options[1].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 0 {
		t.Fatalf("+1/+1 counters after paying = %d, want 0 (both removed)", got)
	}
	if got := e.G.Players[0].Life; got != startLife+2 {
		t.Fatalf("life after paying = %d, want %d (the ability resolved)", got, startLife+2)
	}
	replayCheck(t, e, cfg)
}

// TestRemoveAnyCounterXMinNoCountersAborts pins the empty-range fail-closed
// direction: with a XMin1+ announced counter-removal cost and NO removable
// counter anywhere, the legal X range is empty (floor 1, bound 0), so the
// activation aborts at the announcement instead of posing an ask with no
// options -- which would wedge the seat. The offer gate still OFFERS the
// ability (it skips announced parts by design); choosing it must abort, not
// ask.
func TestRemoveAnyCounterXMinNoCountersAborts(t *testing.T) {
	t.Parallel()
	e, cfg, srcID, bearID := xMinOozeEngine(t, 84, 0)
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 0 {
		t.Fatalf("setup: Bear carries %d +1/+1 counters, want none", got)
	}
	// Precondition: the ability IS offered despite the zero-counter board.
	if _, ok := findAbilityOption(e, srcID, 0); !ok {
		t.Fatalf("ability not offered at all: %+v", e.Pending().Options)
	}
	startLife := e.G.Players[0].Life
	opt := abilityOption(t, e, srcID, 0)
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d != nil {
		for _, o := range d.Options {
			if o.Kind == "x" {
				t.Fatalf("X ask posed with no payable announcement: %+v", d)
			}
		}
	}
	if !hasNote(e, "announced X has no payable value") {
		t.Fatalf("no fail-closed abort note; log tail holds none")
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != startLife {
		t.Fatalf("life after the aborted activation = %d, want %d (nothing may resolve)", got, startLife)
	}
	replayCheck(t, e, cfg)
}

// TestRemoveAnyCounterRealCorpusOozeFluxAnnouncesX pins the RemoveAnyCounter<X1+/...>
// spelling on the REAL compiled corpus card Ooze Flux: its ability
// (`Cost$ XMin1 1 G RemoveAnyCounter<X1+/P1P1/Creature>`, oracle "{1}{G},
// Remove one or more +1/+1 counters from among creatures you control: Create
// an X/X green Ooze creature token, where X is the number of +1/+1 counters
// removed this way.") is offered, its X ask offers exactly the
// +1/+1-counter counts down to the floor of 1 (never 0), and the chosen
// announcement settles by removing the counters and minting an X/X Ooze
// token. Because Ooze Flux comes from the corpus, a parser or compiler
// regression on the card's own Cost$ makes this test fail.
func TestRemoveAnyCounterRealCorpusOozeFluxAnnouncesX(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	ooze := searchCorpusCard(t, reg, "Ooze Flux")
	if ooze == nil {
		t.Fatal("precondition: Ooze Flux absent from the compiled corpus")
	}
	if reg.Tokens["g_x_x_ooze"] == nil {
		t.Fatal("precondition: g_x_x_ooze token script absent from the corpus token registry")
	}
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	land := searchCorpusCard(t, reg, "Island")
	deck := []*cards.Card{ooze, bear}
	for len(deck) < 40 {
		deck = append(deck, land)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = land
	}
	cfg := seatZeroStart(Config{Seed: 8123, Names: []string{"ooze", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens, NameUniverse: reg.Universe()})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	oozeID := searchMoveByName(t, e, "Ooze Flux", state.ZBattlefield)
	bearID := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if e.G.Obj(oozeID).Zone != state.ZBattlefield || e.G.Obj(bearID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Ooze Flux %s Bears %s", e.G.Obj(oozeID).Zone, e.G.Obj(bearID).Zone)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: 2})
	if e.G.Obj(bearID).Counter("P1P1") != 2 {
		t.Fatalf("setup: Bears carry %d +1/+1 counters, want 2", e.G.Obj(bearID).Counter("P1P1"))
	}
	addMana(t, e, 0, "GGG")
	opt := abilityOptionByLabel(t, e, oozeID, "Ooze")
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("X options = %+v, want exactly X=1 and X=2 (one per dream counter, floor at 1)", d.Options)
	}
	for _, o := range d.Options {
		if o.Kind != "x" || (o.Amount != 1 && o.Amount != 2) {
			t.Fatalf("X options = %+v, want only X=1 and X=2 -- the X1+ floor binds, no X=0", d.Options)
		}
	}
	submitChoices(t, e, d.Options[1].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 0 {
		t.Fatalf("+1/+1 counters after paying = %d, want 0 (both removed)", got)
	}
	if !hasEvent(e, events.TokenCreate, 0) {
		t.Fatal("no Ooze token created by the real corpus Ooze Flux activation")
	}
	replayCheck(t, e, cfg)
}
