package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestExchangeTextBoxSwapsKeywords is the CR 612.1 regression for the
// ABILITY half of a text-box exchange: a text box includes its abilities, so
// after api:ExchangeTextBox resolves each object's DERIVED keyword set must
// be the other's. The old string-only implementation left the keywords on
// their printed faces, so Alpha kept Flying and Beta kept Trample. The
// precondition makes the assertion non-vacuous: the two printed keyword sets
// must genuinely differ in both directions before the exchange.
func TestExchangeTextBoxSwapsKeywords(t *testing.T) {
	t.Parallel()
	a := card(t, "Name:Alpha\nManaCost:B\nTypes:Creature Human\nPT:1/1\nK:Flying\nOracle:Flying.\n")
	b := card(t, "Name:Beta\nManaCost:B\nTypes:Creature Beast\nPT:2/2\nK:Trample\nK:Vigilance\nOracle:Trample, vigilance.\n")
	e, _, _ := corpusDeckEngine(t, nil, []*cards.Card{a, b})
	aID, bID := findBoardObject(t, e, a), findBoardObject(t, e, b)
	if aID == 0 || bID == 0 || aID == bID {
		t.Fatalf("setup: a=%d b=%d, both must be distinct battlefield objects", aID, bID)
	}
	// Precondition: the keyword sets really differ in both directions, so a
	// no-op exchange cannot pass and a one-sided swap is detected.
	if !e.HasKeyword(aID, "Flying") || e.HasKeyword(aID, "Trample") {
		t.Fatalf("precondition: Alpha must have Flying and not Trample, got %v", e.Keywords(aID))
	}
	if !e.HasKeyword(bID, "Trample") || e.HasKeyword(bID, "Flying") {
		t.Fatalf("precondition: Beta must have Trample and not Flying, got %v", e.Keywords(bID))
	}

	sa := &cards.SA{API: "ExchangeTextBox", Params: map[string]string{"Duration": "AsLongAsInPlay", "Defined": "Targeted"}}
	ctx := &effects.Ctx{Source: aID, Controller: 0, Targets: []state.Target{{Obj: aID}, {Obj: bID}}}
	effects.Resolve(e, ctx, sa)

	if !e.HasKeyword(aID, "Trample") || e.HasKeyword(aID, "Flying") {
		t.Fatalf("Alpha keywords after exchange = %v, want Beta's (Trample, Vigilance)", e.Keywords(aID))
	}
	if !e.HasKeyword(bID, "Flying") || e.HasKeyword(bID, "Trample") {
		t.Fatalf("Beta keywords after exchange = %v, want Alpha's (Flying)", e.Keywords(bID))
	}
	// Non-ability characteristics must survive: the exchange moves the text
	// box, never the object's identity, P/T or types.
	if e.Power(aID) != 1 || e.Toughness(aID) != 1 || e.Name(aID) != "Alpha" {
		t.Fatalf("Alpha non-text characteristics changed: %d/%d %q", e.Power(aID), e.Toughness(aID), e.Name(aID))
	}
	if e.Power(bID) != 2 || e.Toughness(bID) != 2 || e.Name(bID) != "Beta" {
		t.Fatalf("Beta non-text characteristics changed: %d/%d %q", e.Power(bID), e.Toughness(bID), e.Name(bID))
	}
}

// TestExchangeOfWordsEndToEnd drives the compiled carrier end to end: the
// real Exchange of Words is cast, its ChangesZone ETB trigger queues, its two
// targets are chosen, and the resolved DBExchangeText swaps BOTH halves of the
// two creatures' boxes. It is the end-to-end companion to the handler-level
// carrier test, so a compilation or trigger-wiring mismatch in the carrier
// cannot pass unnoticed. Preconditions: both creatures are on the battlefield
// and their printed texts and keyword sets differ before the cast.
func TestExchangeOfWordsEndToEnd(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	words := mustCorpusCard(t, reg, "Exchange of Words")
	angel := mustCorpusCard(t, reg, "Serra Angel")
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, _ := corpusDeckEngine(t, []*cards.Card{words}, []*cards.Card{angel, bears})
	angelID, bearsID := findBoardObject(t, e, angel), findBoardObject(t, e, bears)
	if angelID == 0 || bearsID == 0 || angelID == bearsID {
		t.Fatalf("setup: angel=%d bears=%d, both must be on the battlefield", angelID, bearsID)
	}
	if !e.HasKeyword(angelID, "Flying") || e.HasKeyword(bearsID, "Flying") {
		t.Fatalf("precondition: angel must have Flying and bears must not, got %v / %v", e.Keywords(angelID), e.Keywords(bearsID))
	}
	angelText, bearsText := e.Text(angelID), e.Text(bearsID)
	if angelText == bearsText {
		t.Fatalf("precondition: the two text boxes must differ, both %q", angelText)
	}

	addMana(t, e, 0, "UUU")
	castCardNow(t, e, "Exchange of Words")
	sawTargets := 0
	for i := 0; i < 200; i++ {
		// The exchange is observable once the bears' rendered text changes;
		// stop before the loop wanders into the next turn's decisions.
		if e.Text(bearsID) != bearsText {
			break
		}
		d := e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KTarget:
			angelIdx := indexOfObjOption(d, angelID)
			bearsIdx := indexOfObjOption(d, bearsID)
			if angelIdx >= 0 && bearsIdx >= 0 {
				sawTargets += 2
				submitChoices(t, e, angelIdx, bearsIdx)
			} else if angelIdx >= 0 {
				sawTargets++
				submitChoices(t, e, angelIdx)
			} else if bearsIdx >= 0 {
				sawTargets++
				submitChoices(t, e, bearsIdx)
			} else {
				t.Fatalf("target ask offers neither creature: %+v", d.Options)
			}
		case decision.KPriority:
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected pending decision %v (%s)", d.Kind, d.Prompt)
		}
	}
	if sawTargets < 2 {
		t.Fatalf("Exchange of Words never posed its two target asks (saw %d)", sawTargets)
	}

	// Each box now reads as the other.
	if got, want := e.Text(angelID), bearsText; got != want {
		t.Fatalf("angel text after exchange = %q, want bears' %q", got, want)
	}
	if got, want := e.Text(bearsID), angelText; got != want {
		t.Fatalf("bears text after exchange = %q, want angel's %q", got, want)
	}
	replayCheck(t, e, cfg)
}

// TestDeadpoolTradingCardEndToEnd drives the compiled second carrier end to
// end: Deadpool's ETBReplacement ChooseCard chain feeds DBExchangeText
// `Defined$ Self & ChosenCard` through the real resolution path. An absent
// Duration$ is indefinite (CR 611.2a), so the swap persists after Deadpool
// leaves the battlefield -- the exchanged box stays on the surviving
// creature. Preconditions as above.
func TestDeadpoolTradingCardEndToEnd(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deadpool := mustCorpusCard(t, reg, "Deadpool, Trading Card")
	angel := mustCorpusCard(t, reg, "Serra Angel")
	// Deadpool replaces as it enters, so it must ENTER through the hand cast
	// for its ETBReplacement to run; seat 0 casts it for {2}{B}{R}.
	e, _, _ := corpusDeckEngine(t, []*cards.Card{deadpool}, []*cards.Card{angel})
	angelID := findBoardObject(t, e, angel)
	if angelID == 0 {
		t.Fatal("setup: Serra Angel must be on the battlefield")
	}
	if !e.HasKeyword(angelID, "Flying") {
		t.Fatalf("precondition: angel must have Flying, got %v", e.Keywords(angelID))
	}
	angelText := e.Text(angelID)

	addMana(t, e, 0, "BBRR")
	castCardNow(t, e, "Deadpool, Trading Card")
	var deadpoolID state.ObjID
	sawChoose := false
	for i := 0; i < 200; i++ {
		// The exchange is observable once the angel's rendered box changes.
		if e.Text(angelID) != angelText {
			break
		}
		d := e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KTarget, decision.KChoose:
			if d.Kind == decision.KChoose {
				sawChoose = true
			}
			// The ChooseCard ask offers the other creature(s); pick the angel
			// when it is among the options, otherwise accept the first.
			if idx := indexOfObjOption(d, angelID); idx >= 0 {
				submitChoices(t, e, idx)
			} else if len(d.Options) > 0 {
				submitChoices(t, e, 0)
			} else {
				t.Fatalf("empty decision options: %+v", d)
			}
		case decision.KPriority:
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected pending decision %v (%s)", d.Kind, d.Prompt)
		}
	}
	if !sawChoose {
		t.Fatal("Deadpool's ChooseCard chain never posed its choice")
	}
	for i := range e.G.Objs {
		if e.G.Objs[i].Card == deadpool && e.G.Objs[i].Zone == state.ZBattlefield {
			deadpoolID = e.G.Objs[i].ID
		}
	}
	if deadpoolID == 0 {
		t.Fatal("Deadpool did not enter the battlefield")
	}
	// Precondition: the exchange really happened before the departure check.
	if e.Text(angelID) == angelText {
		t.Fatalf("precondition: exchange must change Angel's box, still %q", angelText)
	}

	// CR 611.2a: no Duration$ means the change is indefinite, so removing
	// Deadpool leaves the swapped box on the surviving creature.
	e.emit(events.Event{Kind: events.MoveZone, Obj: deadpoolID, From: state.ZBattlefield, To: state.ZGraveyard})
	if got, want := e.Text(angelID), deadpool.Faces[0].Oracle; got != want {
		t.Fatalf("after Deadpool leaves, angel text = %q, want Deadpool's indefinite box %q", got, want)
	}
}

// findBoardObject returns the id of the first battlefield object whose card is
// c, or 0.
func findBoardObject(t *testing.T, e *Engine, c *cards.Card) state.ObjID {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Card == c && o.Zone == state.ZBattlefield {
			return o.ID
		}
	}
	return 0
}
