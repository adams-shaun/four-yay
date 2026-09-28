package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestBlackDragonGateExcludesBlackFromTheColourAsk pins Exclude$ on the
// cast-flow ETB colour ask (etbexclude1): the REAL corpus card Black Dragon
// Gate ("As Black Dragon Gate enters, choose a color other than black")
// played through its own play_land flow must offer the four WUBRG colours
// minus Black, in the fixed WUBRG order with the gap closed (Option.Index
// renumbered), and the answered colour is recorded on the entering object.
// Before the fix this ask offered all five colours and a player could record
// the forbidden Black.
func TestBlackDragonGateExcludesBlackFromTheColourAsk(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{corpusCard(t, "Black Dragon Gate")}, nil)
	gate := moveCorpusCard(t, e, "Black Dragon Gate", 0, state.ZHand)

	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == gate {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for the gate: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "color" {
		t.Fatalf("colour choice %+v", d)
	}
	if len(d.Options) != 4 {
		t.Fatalf("colour choice offers %d options, want 4 (WUBRG minus Black): %+v", len(d.Options), d.Options)
	}
	want := []string{"White", "Blue", "Red", "Green"}
	for i, w := range want {
		if d.Options[i].Label != w {
			t.Fatalf("option %d label %q, want %q (WUBRG order minus Black)", i, d.Options[i].Label, w)
		}
		if d.Options[i].Index != i {
			t.Fatalf("option %d Index %d, want renumbered %d", i, d.Options[i].Index, i)
		}
	}
	for _, o := range d.Options {
		if o.Label == "Black" {
			t.Fatalf("the excluded colour is offered: %+v", d.Options)
		}
	}
	submitChoices(t, e, 3) // Green
	if o := e.G.Obj(gate); o.Zone != state.ZBattlefield || o.ChosenColor != "G" {
		t.Fatalf("gate after play = zone %s ChosenColor %q, want battlefield / G", o.Zone, o.ChosenColor)
	}
	replayCheck(t, e, cfg)
}

// TestETBColorExcludeFailOpenOnUnresolvableToken pins the fail-open contract:
// an Exclude$ token etbColourLetter cannot resolve is ignored, so the ask
// still offers all five colours and is never emptied (the etbOptions
// totality rule). Corpus carriers never spell a token this shape cannot
// resolve, so the fixture is inline.
func TestETBColorExcludeFailOpenOnUnresolvableToken(t *testing.T) {
	t.Parallel()
	land := "Name:FailOpen\nManaCost:no cost\nTypes:Land\nK:ETBReplacement:Other:ChooseColor\n" +
		"SVar:ChooseColor:DB$ ChooseColor | Defined$ You | Exclude$ nonsense | SpellDescription$ x\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n"
	// seatZeroStart pins the toss on seat 0 (etbConfig itself does not pin
	// the toss; a seat-1 starter chases driveToStep to the mill-out), the
	// fixture card seeded as a REAL deck card like the Jewel fixture.
	cfg := seatZeroStart(Config{Seed: 78, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{append([]*cards.Card{card(t, land)}, mountainDeck(t, 39)...), mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	l := findByName(e, "FailOpen", 0)
	if l == 0 {
		t.Fatal("FailOpen not found")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: l, From: e.G.Obj(l).Zone, To: state.ZHand})
	e.pending = nil
	e.Advance()
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "play_land" && o.Obj == l {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "color" || len(d.Options) != 5 {
		t.Fatalf("colour choice after an unresolvable Exclude$ token %+v, want all five", d)
	}
	for i, w := range []string{"White", "Blue", "Black", "Red", "Green"} {
		if d.Options[i].Label != w {
			t.Fatalf("option %d label %q, want %q (full WUBRG order)", i, d.Options[i].Label, w)
		}
	}
	submitChoices(t, e, 2) // Black -- the ask was never narrowed, so it is answerable
	if o := e.G.Obj(l); o.Zone != state.ZBattlefield || o.ChosenColor != "B" {
		t.Fatalf("land after play = zone %s ChosenColor %q, want battlefield / B", o.Zone, o.ChosenColor)
	}
	replayCheck(t, e, cfg)
}
