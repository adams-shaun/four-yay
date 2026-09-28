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

// wastewakerDrive advances the engine until (turn, active, step), passing
// priority, declining combat, and answering every hand-size-limit cleanup
// discard with the offered NON-keep cards (so the fixtures the test needs in
// hand survive it). Anything else fails loudly.
func wastewakerDrive(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step, keep map[state.ObjID]bool) {
	t.Helper()
	for i := 0; i < 8000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before turn %d seat %d step %s (winner %+v)", turn, active, step, e.G.Winner)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit priority: %v", err)
			}
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KChoose:
			if len(d.Options) == 0 {
				t.Fatalf("KChoose with no options: %+v", d)
			}
			if !strings.Contains(d.Prompt, "hand-size limit") {
				t.Fatalf("unexpected choose decision %q: %+v", d.Prompt, d)
			}
			var picks []int
			for _, o := range d.Options {
				if keep[o.Obj] {
					continue
				}
				picks = append(picks, o.Index)
				if len(picks) == d.Max {
					break
				}
			}
			if len(picks) != d.Max {
				t.Fatalf("not enough discardable non-keep cards for %q: %+v", d.Prompt, d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}); err != nil {
				t.Fatalf("submit discard: %v", err)
			}
		default:
			t.Fatalf("unexpected decision kind %v: %+v", d.Kind, d)
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s", turn, active, step)
}

// wastewakerFixture builds the two-seat game both tests share: seat 0
// controls the corpus Eumidian Wastewaker, handCards are lifted into seat 0's
// hand (the cleanup discard after them is answered with Mountains), and
// seat 1's deck is Mountains (plus islandOnBattlefield's Island moved onto
// its battlefield when asked).
func wastewakerFixture(t *testing.T, handCards []string, islandOnBattlefield bool) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	eum, ok := reg.Lookup("Eumidian Wastewaker")
	if !ok {
		t.Fatal("corpus fixture: Eumidian Wastewaker missing")
	}
	extras := []*cards.Card{eum}
	for _, name := range handCards {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus fixture: %s missing", name)
		}
		extras = append(extras, c)
	}
	if islandOnBattlefield {
		c, ok := reg.Lookup("Island")
		if !ok {
			t.Fatal("corpus fixture: Island missing")
		}
		extras = append(extras, c)
	}
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append(extras, mountainDeck(t, 40-len(extras))...),
			mountainDeck(t, 40),
		}})
	if islandOnBattlefield {
		island, ok := reg.Lookup("Island")
		if !ok {
			t.Fatal("corpus fixture: Island missing")
		}
		cfg.Decks[1] = append([]*cards.Card{island}, mountainDeck(t, 39)...)
	}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	eumID := moveByName(t, e, 0, "Eumidian Wastewaker", state.ZBattlefield)
	if o := e.G.Obj(eumID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Eumidian not on the battlefield: %+v", o)
	}
	if islandOnBattlefield {
		id := moveByName(t, e, 1, "Island", state.ZBattlefield)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
			t.Fatalf("Island not on seat 1's battlefield: %+v", o)
		}
	}
	// handCards: move every copy seat 0 owns anywhere into its hand, lifting
	// each from the library when the opening hand did not deal it. Duplicates
	// beyond the named count are left wherever they are.
	ids := make([]state.ObjID, 0, len(handCards))
	for _, name := range handCards {
		found := state.ObjID(0)
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
					found = id
					if z != state.ZHand {
						e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZHand})
					}
					break
				}
			}
			if found != 0 {
				break
			}
		}
		if found == 0 {
			t.Fatalf("seat 0 has no %s for the fixture", name)
		}
		ids = append(ids, found)
	}
	return e, eumID, ids
}

// pickOption returns the pending choice decision's option index whose
// candidate is want.
func choiceOption(t *testing.T, d *decision.Decision, want state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == want {
			return o.Index
		}
	}
	t.Fatalf("candidate %d not offered in %+v", want, d.Options)
	return -1
}

// answerChoice finds the pending KChoose decision for player p and submits
// exactly one pick.
func answerChoice(t *testing.T, e *Engine, p state.PlayerID, pick state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != p {
		t.Fatalf("expected seat %d's choice decision, got %+v", p, d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("Mandatory$ True choice bounds = [%d %d], want [1 1]", d.Min, d.Max)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: p, Choices: []int{choiceOption(t, d, pick)}}); err != nil {
		t.Fatalf("submit seat %d's choice: %v", p, err)
	}
}

// TestEumidianWastewakerChoosersPickDiscardOrSacrifice is pred:CanBeSacrificedBy's
// end-to-end pin on the real corpus card: each chooser — the defending player
// first, then the controller — is offered BOTH halves of
// `Choices$ Card.inZoneHand,Permanent.CanBeSacrificedBy` (before the fix the
// permanent alternative matched nothing and only hand cards were offered).
// The controller sacrifices a land with its pick, exercising the trigger's
// whole chain together: DBDiscard's `DiscardValid$ Card.ChosenCard` (the
// sacrificed chooser discards nothing — its chosen card is not in its hand),
// DBSac's `ValidCards$ Card.ChosenCardStrict` (the chosen permanent is
// sacrificed), and DBDraw's `Count$ValidGraveyard Land.ChosenCard` (Island +
// Forest = 2 draws).
func TestEumidianWastewakerChoosersPickDiscardOrSacrifice(t *testing.T) {
	t.Parallel()
	e, eumID, ids := wastewakerFixture(t, []string{"Forest"}, true)
	islandID, forestID := ids[0], ids[0]
	// wastewakerFixture lifts exactly one Forest; the Island is seat 1's
	// battlefield permanent, located by name below.
	islandID = 0
	for _, id := range e.G.Zone(state.ZBattlefield, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Island" {
			islandID = id
		}
	}
	if islandID == 0 {
		t.Fatal("seat 1 has no Island on the battlefield")
	}
	for _, id := range ids {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Forest" {
			forestID = id
		}
	}
	if forestID == 0 {
		t.Fatal("seat 0 has no Forest in hand")
	}

	wastewakerDrive(t, e, 3, 0, state.StepDeclareAttackers,
		map[state.ObjID]bool{forestID: true, eumID: true})
	submitAttackersOnly(t, e, eumID)
	passToKind(t, e, decision.KChoose)

	// Seat 1's ask: BOTH halves offered — its hand cards and its Island.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 1 {
		t.Fatalf("expected seat 1's choice decision, got %+v", d)
	}
	var sawHand bool
	for _, o := range d.Options {
		if o.Obj == islandID {
			continue
		}
		if oo := e.G.Obj(o.Obj); oo != nil && oo.Zone == state.ZHand && oo.Controller == 1 {
			sawHand = true
		}
	}
	if choiceOption(t, d, islandID) < 0 || !sawHand {
		t.Fatalf("seat 1's choice did not offer both halves: hand=%v options=%+v", sawHand, d.Options)
	}
	answerChoice(t, e, 1, islandID)

	// Seat 0's ask: its own hand and permanent.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 {
		t.Fatalf("expected seat 0's choice decision, got %+v", d)
	}
	if choiceOption(t, d, forestID) < 0 || choiceOption(t, d, eumID) < 0 {
		t.Fatalf("seat 0's choice did not offer its hand card and its permanent: %+v", d.Options)
	}
	hand0 := len(e.G.Zone(state.ZHand, 0))
	draws0 := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Draw && ev.Player == 0 })
	answerChoice(t, e, 0, forestID)

	if o := e.G.Obj(islandID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the chosen permanent was not sacrificed: %+v", o)
	}
	if o := e.G.Obj(forestID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the chosen hand card was not discarded: %+v", o)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0+1 {
		t.Fatalf("seat 0's hand = %d, want %d (discarded one, drew 2)", got, hand0+1)
	}
	if got := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Draw && ev.Player == 0 }); got != draws0+2 {
		t.Fatalf("seat 0 drew %d cards since the answers, want exactly 2", got-draws0)
	}
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Choose && ev.Counter == "chosen"
	}); n != 2 {
		t.Fatalf("chosen-record events = %d, want one per chooser (2)", n)
	}
}

// TestEumidianWastewakerHandOnlyChoiceResolves is the negative control: a
// chooser with NO battlefield permanent sees only the hand half and the
// trigger still resolves end to end — both choosers discard, and the draw
// counts the two chosen lands that reached the graveyard.
func TestEumidianWastewakerHandOnlyChoiceResolves(t *testing.T) {
	t.Parallel()
	e, eumID, ids := wastewakerFixture(t, []string{"Forest", "Island"}, false)
	keep := map[state.ObjID]bool{eumID: true}
	for _, id := range ids {
		keep[id] = true
	}
	wastewakerDrive(t, e, 3, 0, state.StepDeclareAttackers, keep)
	submitAttackersOnly(t, e, eumID)
	passToKind(t, e, decision.KChoose)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 1 {
		t.Fatalf("expected seat 1's choice decision, got %+v", d)
	}
	for _, o := range d.Options {
		if oo := e.G.Obj(o.Obj); oo == nil || oo.Zone != state.ZHand {
			t.Fatalf("seat 1 (no permanents) was offered a non-hand candidate %+v", o)
		}
	}
	if len(d.Options) == 0 {
		t.Fatal("seat 1's choice offered nothing")
	}
	first := d.Options[0].Index
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{first}}); err != nil {
		t.Fatalf("submit seat 1's choice: %v", err)
	}
	_, islandID := ids[0], ids[1]
	hand0 := len(e.G.Zone(state.ZHand, 0))
	draws0 := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Draw && ev.Player == 0 })
	answerChoice(t, e, 0, islandID)

	if o := e.G.Obj(islandID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("seat 0's chosen Island was not discarded: %+v", o)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0+1 {
		t.Fatalf("seat 0's hand = %d, want %d (discarded one, drew 2)", got, hand0+1)
	}
	if got := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Draw && ev.Player == 0 }); got != draws0+2 {
		t.Fatalf("seat 0 drew %d cards since the answers, want exactly 2", got-draws0)
	}
}
