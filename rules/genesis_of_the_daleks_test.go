package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGenesisOfTheDaleksChapterLosesTotalDalekPower drives the REAL compiled
// Genesis of the Daleks chapter IV through the engine: live Dalek creatures
// on the battlefield, the chapter's `DBDestroyDalek` branch chosen, the
// `DestroyAll` actually moving them to the graveyard, and the chained
// `DBLoseLife` (`LifeAmount$ Y`, `Defined$ Opponent`) making each opponent
// lose life equal to Y = `Count$ThisTurnEntered_Graveyard_from_Battlefield_Dalek$CardPower`.
//
// Before the fix, evalThisTurnEnteredAs handed `Dalek$CardPower` to the
// zone-spec matcher as one filter, matched nothing, and Y evaluated to zero,
// so every opponent lost nothing. This test asserts the played-out life loss
// equals the measured total power of the Daleks the chapter itself destroyed,
// and verifies the whole event stream replays byte-identically.
func TestGenesisOfTheDaleksChapterLosesTotalDalekPower(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	genesis := mustCorpusCard(t, reg, "Genesis of the Daleks")
	// Precondition on the REAL compiled script: the SVar under test is the
	// property-sum form, so a corpus pin move cannot silently make this test
	// exercise a different shape.
	if got := genesis.Faces[0].SVars["Y"]; got != "Count$ThisTurnEntered_Graveyard_from_Battlefield_Dalek$CardPower" {
		t.Fatalf("precondition: Genesis SVar Y = %q, want the property-sum form", got)
	}

	// Three authored Dalek creatures with distinct printed powers whose SUM
	// (12) differs from their COUNT (3). The saga's own earlier token
	// chapters then create further 3/3 Dalek tokens before chapter IV
	// resolves, so the total is measured from live state below rather than
	// hard-coded; the authored 12 keeps the sum strictly above the count.
	dalek := func(name, pt string) *cards.Card {
		return card(t, "Name:"+name+"\nTypes:Artifact Creature Dalek\nPT:"+pt+"\nOracle:x\n")
	}
	d3, d4, d5 := dalek("Dalek Three", "3/3"), dalek("Dalek Four", "4/4"), dalek("Dalek Five", "5/5")

	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := Config{Seed: 771, Names: []string{"a", "b", "c"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{genesis, d3, d4, d5}, mountainDeck(t, 36)...),
			append([]*cards.Card{bear}, mountainDeck(t, 39)...),
			append([]*cards.Card{bear}, mountainDeck(t, 39)...),
		}}
	e := New(seatZeroStart(cfg))
	e.Advance()

	// Place the real Saga and the three authored Daleks on the battlefield.
	// The Saga's entry grants it chapter I (a real CounterChange), and a +3
	// lore CounterChange then crosses II, III and IV through the engine's own
	// checkChapterTriggers path -- the same path a real draw-step counter
	// takes.
	gen := placeInDeck(t, e, 0, genesis, state.ZBattlefield)
	authored := []state.ObjID{
		placeInDeck(t, e, 0, d3, state.ZBattlefield),
		placeInDeck(t, e, 0, d4, state.ZBattlefield),
		placeInDeck(t, e, 0, d5, state.ZBattlefield),
	}
	if z := e.G.Obj(gen).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: Genesis zone = %s, want Battlefield", z)
	}
	for _, id := range authored {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || !hasTypeWord(o.Face().Types, "Dalek") {
			t.Fatalf("precondition: authored Dalek %d zone=%v is not a live Dalek on the battlefield", id, e.G.Obj(id))
		}
	}
	// Precondition on the authored powers: they differ from the count.
	authoredSum, authoredCount := 0, 0
	for _, id := range authored {
		authoredSum += e.G.Obj(id).Face().Power()
		authoredCount++
	}
	if authoredSum != 12 || authoredCount != 3 {
		t.Fatalf("precondition: authored Daleks sum %d over %d, want 12 over 3", authoredSum, authoredCount)
	}

	e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.CounterChange, Obj: gen, Counter: "LORE", Amount: 3})
	if got := e.G.Obj(gen).Counter("LORE"); got != 4 {
		t.Fatalf("precondition: Genesis lore = %d, want 4 (reached chapter IV)", got)
	}

	// Resolve the queued chapters in order. Chapters I-III create Dalek
	// tokens; chapter IV is the villainous choice. Measure the live Dalek
	// power total immediately before the villainous mode is answered -- that
	// is exactly the set DBVillainous's DestroyAll will kill, so Y must equal
	// it.
	var (
		modeIdx    = -1
		powerTotal int
		dalekCount int
	)
	// Put the queued chapters on the stack, then drain the stack the same way
	// drainVillainousChoice does: answer whatever the engine asks (priority
	// passes, the trigger-order permutation, targets, the villainous mode,
	// nested picks) until the stack empties. putTriggersOnStack first poses
	// the trigger-order ask; the loop then resumes the drain through the
	// engine's own Advance path.
	e.putTriggersOnStack()
	for i := 0; i < 300 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KTriggerOrder:
			// The saga's chapters I-IV are queued together by the +3 lore
			// counter. The chosen order is the order they are put on the
			// stack, and the stack resolves last-in-first-out, so to make
			// chapter IV resolve LAST (after I-III have created their
			// Dalek tokens) we submit the queue indices in REVERSE.
			choices := make([]int, len(d.Options))
			for i := range choices {
				choices[i] = len(d.Options) - 1 - i
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit trigger_order: %v", err)
			}
		case decision.KModes:
			// The villainous mode ask for chapter IV. Measure the live Dalek
			// board NOW: this is the set DestroyAll is about to kill.
			modeIdx = i
			powerTotal, dalekCount = liveDalekPower(e)
			if powerTotal <= dalekCount {
				t.Fatalf("precondition: live Dalek power %d over %d creatures must exceed the count", powerTotal, dalekCount)
			}
			// First option is DBDestroyDalek ("destroy all Dalek creatures
			// and each opponent loses life equal to the total power...").
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit villainous mode: %v", err)
			}
		default:
			// A target / choose / order ask: take the first option.
			if len(d.Options) == 0 {
				t.Fatalf("decision %v has no options", d.Kind)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit %v: %v", d.Kind, err)
			}
		}
		if modeIdx >= 0 && len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
			break
		}
	}
	if modeIdx < 0 {
		t.Fatal("the villainous choice (KMode) for chapter IV was never posed")
	}
	if powerTotal <= 0 {
		t.Fatal("precondition: no live Dalek power was measured before the chapter resolved")
	}
	t.Logf("measured: %d live Daleks with total power %d died to the chapter; each opponent lost %d life", dalekCount, powerTotal, powerTotal)

	// Every Dalek that was on the battlefield is now dead: the chapter's own
	// DestroyAll did it, not the test.
	survivors := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && hasTypeWord(o.Face().Types, "Dalek") {
			survivors++
		}
	}
	if survivors != 0 {
		t.Fatalf("Daleks survived the chapter's DestroyAll: %d still on the battlefield", survivors)
	}

	// The measured life change: each opponent loses exactly the total power
	// of the Daleks that died, and the controller loses nothing.
	if got := e.G.Players[0].Life; got != 20 {
		t.Errorf("controller life = %d, want 20 (never an opponent)", got)
	}
	for _, p := range []state.PlayerID{1, 2} {
		if got := int(e.G.Players[p].Life); got != 20-powerTotal {
			t.Errorf("opponent %d life = %d, want %d (20 - %d total Dalek power)",
				p, got, 20-powerTotal, powerTotal)
		}
	}

	replayCheck(t, e, cfg)
}

// liveDalekPower sums the printed power of every Dalek creature on all
// battlefields and counts them, the total chapter IV's DestroyAll is about to
// remove.
func liveDalekPower(e *Engine) (int, int) {
	sum, n := 0, 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && hasTypeWord(o.Face().Types, "Dalek") {
			sum += o.Face().Power()
			n++
		}
	}
	return sum, n
}
