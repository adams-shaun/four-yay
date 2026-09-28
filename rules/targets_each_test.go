package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTargetsForEachPlayerUsesDecisionGroups pins Forge TargetRestrictions'
// setForEachPlayer contract on Blatant Thievery's real target shape: at most
// one permanent per opposing controller, with one required from each.
func TestTargetsForEachPlayerUsesDecisionGroups(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Blatant Thievery")
	if !ok {
		t.Fatal("Blatant Thievery missing from corpus")
	}
	sa := card.Faces[0].SpellAbility()
	e := newSeats(t, 3)
	for _, p := range []state.PlayerID{1, 2} {
		for i := 0; i < 2; i++ {
			o := e.G.AddObject(&cards.Card{Faces: []*cards.Face{{Name: "Relic", Types: []string{"Artifact"}}}}, p)
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		}
	}
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil || d.Min != 2 || d.Max != 2 || len(d.Options) != 4 {
		t.Fatalf("target decision = %+v, want two required choices over four options", d)
	}
	if d.Options[0].Group == "" || d.Options[0].Group != d.Options[1].Group || d.Options[0].Group == d.Options[2].Group {
		t.Fatalf("target groups = [%q %q %q], want one group per controller", d.Options[0].Group, d.Options[1].Group, d.Options[2].Group)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0, 1}}); err == nil {
		t.Fatal("two targets controlled by one player were accepted")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0, 2}}); err != nil {
		t.Fatalf("one target per controller rejected: %v", err)
	}
}

// TestHavocEaterGoadsOneCreaturePerOpponentAndCountsThePower pins the
// TargetsForEachPlayer$ target shape on Havoc Eater's real corpus card
// (task pfpe1): its ETB trigger's DB$ Goad carries TargetMin$ 0,
// TargetMax$ X (SVar:X:PlayerCountOpponents$Amount) and
// TargetsForEachPlayer$ True — the ask is over the OPPONENTS' creatures with
// at most one per controller, the maximum is the OPPONENT COUNT (a dynamic
// bound the PlayerCountOpponents$Amount head must resolve; pre-pfpe1 the
// head failed unresolvable and the bound degraded to 1, collapsing "for each
// opponent, goad up to one target creature that opponent controls" to a
// single target), and the chained DB$ PutCounter reads
// Y:Remembered$CardPower — RememberGoaded$ True must land the goaded
// creatures in the resolution's Remembered so X +1/+1 counters, where X is
// the total goaded power, actually land.
func TestHavocEaterGoadsOneCreaturePerOpponentAndCountsThePower(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	eater, ok := reg.Lookup("Havoc Eater")
	if !ok {
		t.Fatal("Havoc Eater missing from corpus")
	}
	bear, ok := reg.Lookup("Runeclaw Bear")
	if !ok {
		t.Fatal("Runeclaw Bear missing from corpus")
	}
	// Seat 1 carries TWO bears so the one-per-controller discipline is
	// exercised on the wire, not just the offer.
	deck0 := append(mountainDeck(t, 40), eater)
	deck1 := append(mountainDeck(t, 40), bear, bear)
	deck2 := append(mountainDeck(t, 40), bear)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{deck0, deck1, deck2}}))
	e.Advance()
	toMain1(t, e)
	eaterID := crAbortMove(t, e, 0, "Havoc Eater", state.ZBattlefield)
	bearA := crAbortMove(t, e, 1, "Runeclaw Bear", state.ZBattlefield)
	bearB := crAbortMove(t, e, 1, "Runeclaw Bear", state.ZBattlefield)
	bearC := crAbortMove(t, e, 2, "Runeclaw Bear", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the trigger's target decision", d)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("target bounds = (%d, %d), want (0, 2): TargetMax$ X is the opponent count", d.Min, d.Max)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %+v, want one per opposing creature", d.Options)
	}
	groupOf := map[state.ObjID]string{}
	for _, o := range d.Options {
		if o.Obj != bearA && o.Obj != bearB && o.Obj != bearC {
			t.Fatalf("offered %d — only the opponents' creatures are targets", o.Obj)
		}
		groupOf[o.Obj] = o.Group
	}
	if groupOf[bearA] == "" || groupOf[bearA] != groupOf[bearB] || groupOf[bearA] == groupOf[bearC] {
		t.Fatalf("groups = %v, want one per controller", groupOf)
	}
	indexOf := func(id state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id {
				return o.Index
			}
		}
		return -1
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{indexOf(bearA), indexOf(bearB)}}); err == nil {
		t.Fatal("two creatures controlled by one opponent were accepted")
	}
	// Replay discipline: the same answer must fold identically on a clone —
	// the bound is resolved at ask time and the goaded set is a per-
	// resolution ctx Remembered, both re-derived by replay without events.
	clone := e.Clone()
	pick := []int{indexOf(bearA), indexOf(bearC)}
	for _, engine := range []*Engine{e, clone} {
		if err := engine.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: pick}); err != nil {
			t.Fatalf("submit one-per-opponent answer: %v", err)
		}
		for i := 0; i < 10 && !engine.G.Over; i++ {
			dd := engine.Pending()
			if dd == nil {
				break
			}
			if dd.Kind != decision.KPriority {
				t.Fatalf("unexpected decision after the goad ask: %+v", dd)
			}
			passIdx := -1
			for _, o := range dd.Options {
				if o.Kind == "pass" {
					passIdx = o.Index
				}
			}
			if passIdx < 0 {
				t.Fatal("priority decision without a pass option")
			}
			if err := engine.Submit(decision.Intent{Seq: dd.Seq, Player: dd.Player, Choices: []int{passIdx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		}
	}
	eo := e.G.Obj(eaterID)
	if n := targetCounterN(eo, "P1P1"); n != 4 {
		t.Fatalf("Havoc Eater P1P1 counters = %d, want 4: X is the total goaded power (2+2)", n)
	}
	for _, id := range []state.ObjID{bearA, bearC} {
		o := e.G.Obj(id)
		if len(o.Goads) == 0 {
			t.Fatalf("bear %d was not goaded", id)
		}
		goaded := false
		for _, ge := range o.Goads {
			if ge.Player == 0 {
				goaded = true
			}
		}
		if !goaded {
			t.Fatalf("bear %d's goads %+v name no seat-0 goader", id, o.Goads)
		}
	}
	if len(e.G.Obj(bearB).Goads) != 0 {
		t.Fatal("the unchosen bear was goaded")
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the TargetsForEachPlayer ask and resolution")
	}
}

// TestHavocEaterElectedZeroGoadsNothingAndStillResolves pins the Min-0 arm:
// declining every target resolves the trigger with no goad and no counter,
// the CR 608.2c untargeted resolution (a Min 0 ask with no legal target
// never wedges either).
func TestHavocEaterElectedZeroGoadsNothingAndStillResolves(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	eater, ok := reg.Lookup("Havoc Eater")
	if !ok {
		t.Fatal("Havoc Eater missing from corpus")
	}
	bear, ok := reg.Lookup("Runeclaw Bear")
	if !ok {
		t.Fatal("Runeclaw Bear missing from corpus")
	}
	deck0 := append(mountainDeck(t, 40), eater)
	deck1 := append(mountainDeck(t, 40), bear)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck0, deck1}}))
	e.Advance()
	toMain1(t, e)
	eaterID := crAbortMove(t, e, 0, "Havoc Eater", state.ZBattlefield)
	_ = crAbortMove(t, e, 1, "Runeclaw Bear", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 0 || d.Max != 1 {
		t.Fatalf("pending = %+v, want the Min-0 one-opponent target ask", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
		t.Fatalf("submit empty answer: %v", err)
	}
	for i := 0; i < 10 && !e.G.Over; i++ {
		dd := e.Pending()
		if dd == nil {
			break
		}
		if dd.Kind != decision.KPriority {
			t.Fatalf("unexpected decision after the declined ask: %+v", dd)
		}
		passIdx := -1
		for _, o := range dd.Options {
			if o.Kind == "pass" {
				passIdx = o.Index
			}
		}
		if passIdx < 0 {
			t.Fatal("priority decision without a pass option")
		}
		if err := e.Submit(decision.Intent{Seq: dd.Seq, Player: dd.Player, Choices: []int{passIdx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	if n := targetCounterN(e.G.Obj(eaterID), "P1P1"); n != 0 {
		t.Fatalf("Havoc Eater P1P1 counters = %d, want 0", n)
	}
}

// counterN reads one counter kind off an object's Counters (the cloak_test
// helper shape, local to this file).
func targetCounterN(o *state.Object, kind string) int {
	for i := range o.Counters {
		if o.Counters[i].Kind == kind {
			return int(o.Counters[i].N)
		}
	}
	return 0
}

// TestTolarianContemptBareInlineBoundAsksOnePerOpponent pins the bare inline
// spelling of the TargetsForEachPlayer$ dynamic bound (pfpe1 follow-up):
// Tolarian Contempt writes its end-step bound INLINE --
// TargetMax$ PlayerCountOpponents$Amount, no SVar name and no Count$ prefix
// -- where Havoc Eater reaches the same body through
// SVar:X:PlayerCountOpponents$Amount behind TargetMax$ X. Before the fix
// NumResolved answered the bare inline form (0, false), the bound degraded
// to the default 1, and "for each opponent, choose up to one target creature
// they control" collapsed to a single target -- the exact symptom the ticket
// was filed for, surviving on one of the three dynamic-bound carriers.
func TestTolarianContemptBareInlineBoundAsksOnePerOpponent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	contempt, ok := reg.Lookup("Tolarian Contempt")
	if !ok {
		t.Fatal("Tolarian Contempt missing from corpus")
	}
	bear, ok := reg.Lookup("Runeclaw Bear")
	if !ok {
		t.Fatal("Runeclaw Bear missing from corpus")
	}
	// Seat 1 carries TWO bears so the one-per-controller discipline is
	// exercised on the wire, not just the offer.
	deck0 := append(mountainDeck(t, 40), contempt)
	deck1 := append(mountainDeck(t, 40), bear, bear)
	deck2 := append(mountainDeck(t, 40), bear)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{deck0, deck1, deck2}}))
	e.Advance()
	toMain1(t, e)
	bearA := crAbortMove(t, e, 1, "Runeclaw Bear", state.ZBattlefield)
	bearB := crAbortMove(t, e, 1, "Runeclaw Bear", state.ZBattlefield)
	bearC := crAbortMove(t, e, 2, "Runeclaw Bear", state.ZBattlefield)
	_ = crAbortMove(t, e, 0, "Tolarian Contempt", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	// The ETB trigger places a rejection counter on each opposing creature
	// (TrigPutCounterAll); the end-step Phase trigger then poses the ask.
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the end-step trigger's target decision", d)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("target bounds = (%d, %d), want (0, 2): the bare inline TargetMax$ PlayerCountOpponents$Amount is the opponent count", d.Min, d.Max)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %+v, want one per opposing creature", d.Options)
	}
	groupOf := map[state.ObjID]string{}
	for _, o := range d.Options {
		if o.Obj != bearA && o.Obj != bearB && o.Obj != bearC {
			t.Fatalf("offered %d — only the opponents' creatures are targets", o.Obj)
		}
		if n := targetCounterN(e.G.Obj(o.Obj), "REJECTION"); n != 1 {
			t.Fatalf("offered creature %d carries %d rejection counters, want 1", o.Obj, n)
		}
		groupOf[o.Obj] = o.Group
	}
	if groupOf[bearA] == "" || groupOf[bearA] != groupOf[bearB] || groupOf[bearA] == groupOf[bearC] {
		t.Fatalf("groups = %v, want one per controller", groupOf)
	}
	indexOf := func(id state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id {
				return o.Index
			}
		}
		return -1
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{indexOf(bearA), indexOf(bearB)}}); err == nil {
		t.Fatal("two creatures controlled by one opponent were accepted")
	}
	// One creature per opponent, answered; the sub-ability chain (DBRepeat's
	// RepeatEach over the targeted) then moves each chosen creature to its
	// OWNER's library. Tolarian Contempt's script writes NO LibraryPosition$
	// on the primary move (Forge's absent default is 0 = TOP), and its
	// top-or-bottom election rides AlternativeDecider$ RememberedOwner, a
	// decider this engine does not recognise. The deterministic no-host
	// fallback therefore takes the PRIMARY placement (top) -- the same
	// primary-destination contract the Dream Cache DestinationAlternative$
	// shape has -- and emits a Note naming the unrecognised decider. Before
	// golgari_thug2 the absent spelling left the card at the MoveZone bottom
	// append, which is what the old bottom assertion here was pinning.
	pick := []int{indexOf(bearA), indexOf(bearC)}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: pick}); err != nil {
		t.Fatalf("submit one-per-opponent answer: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	if z := e.G.Obj(bearA).Zone; z != state.ZLibrary {
		t.Fatalf("chosen bear %d zone = %v, want its owner's library", bearA, z)
	}
	if z := e.G.Obj(bearC).Zone; z != state.ZLibrary {
		t.Fatalf("chosen bear %d zone = %v, want its owner's library", bearC, z)
	}
	if z := e.G.Obj(bearB).Zone; z != state.ZBattlefield {
		t.Fatalf("unchosen bear %d zone = %v, want battlefield", bearB, z)
	}
	lib1 := e.G.Zone(state.ZLibrary, 1)
	lib2 := e.G.Zone(state.ZLibrary, 2)
	if len(lib1) == 0 || len(lib2) == 0 {
		t.Fatalf("precondition: owner libraries must hold the returned bears (lib1=%d lib2=%d)", len(lib1), len(lib2))
	}
	if lib1[0] != bearA {
		t.Fatalf("bear %d not on top of its owner's library (head %d)", bearA, lib1[0])
	}
	if lib2[0] != bearC {
		t.Fatalf("bear %d not on top of its owner's library (head %d)", bearC, lib2[0])
	}
}
