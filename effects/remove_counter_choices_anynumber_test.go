package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The two riders the bare-Choices$ RemoveCounter arm reads beyond Amy Pond's
// scope, both pinned on the real corpus cards that carry them:
//
//   - ChoiceOptional$ True is Forge's "each of ANY number", not an
//     "up to one". Garnet, Princess of Alexandria removes a lore counter from
//     each of any number of Sagas you control, so a two-Saga board must be
//     able to take BOTH (the earlier reading kept Max at the default 1 and
//     silently misplayed the card).
//   - RememberAmount$ True is the removed-COUNT transport the chained payoff
//     reads through Count$RememberedNumber (Garnet's X +1/+1 counters,
//     Dyadrine, Synthesis Amalgam's ConditionCheckSVar$ Z draw-and-token).
//     Without it the removal happened and the payoff saw zero.
//
// Shapes the arm still withholds keep their loud Note, pinned below.

// sagaOnBattlefield mints a Saga permanent under seat 0 with n lore counters,
// through logged events (never a direct state write).
func sagaOnBattlefield(t *testing.T, h *fakeHost, name string, lore int32) state.ObjID {
	t.Helper()
	id := h.g.AddObject(mkCard(t, "Name:"+name+"\nTypes:Enchantment Saga\nOracle:x\n"), 0).ID
	h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "LORE", Amount: lore})
	return id
}

// garnetFixture puts Garnet and two lore-bearing Sagas on the battlefield and
// returns the compiled TrigRemoveCounter SA plus a Ctx carrying Garnet's own
// SVar table (SVar:X:Count$RememberedNumber is what the payoff reads).
func garnetFixture(t *testing.T) (*fakeHost, *Ctx, *cards.SA, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	card, trig := corpusSA(t, "Garnet, Princess of Alexandria", "TrigRemoveCounter")
	if trig.API != "RemoveCounter" ||
		!strings.EqualFold(trig.Params["ChoiceOptional"], "True") ||
		!strings.EqualFold(trig.Params["RememberAmount"], "True") ||
		trig.Params["ChoiceNum"] != "" {
		t.Fatalf("Garnet's compiled trigger = %s %+v, want RemoveCounter with ChoiceOptional$/RememberAmount$ and no ChoiceNum$", trig.API, trig.Params)
	}
	h := newHost(t, 2)
	garnet := h.g.AddObject(card, 0).ID
	h.Emit(events.Event{Kind: events.MoveZone, Obj: garnet, From: state.ZLibrary, To: state.ZBattlefield})
	a := sagaOnBattlefield(t, h, "Saga A", 2)
	b := sagaOnBattlefield(t, h, "Saga B", 3)
	c := &Ctx{Controller: 0, Source: garnet, SVars: h.g.Obj(garnet).Face().SVars}
	return h, c, trig, garnet, a, b
}

// Finding 1: the election Garnet poses is 0..(every eligible Saga), so both
// Sagas can be chosen. A Max of 1 here is the misplay this pins against.
func TestGarnetAnyNumberElectionOffersEverySaga(t *testing.T) {
	h, c, trig, _, a, b := garnetFixture(t)
	h.askResult = false // record the decision, then take the stand-in
	head := *trig
	head.Sub = nil
	Resolve(h, c, &head)
	d := h.lastAsk
	if d == nil {
		t.Fatal("no election posed for Garnet's any-number lore removal")
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("election range = %d..%d, want 0..2 (ChoiceOptional$ True is \"each of any number\")", d.Min, d.Max)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != a || d.Options[1].Obj != b {
		t.Fatalf("options = %+v, want both Sagas in battlefield order (%d, %d)", d.Options, a, b)
	}
}

// ChoiceOptional$ True does NOT unlock the shapes the arm still withholds:
// Eventide's Shadow pairs it with CounterType$/CounterNum$ Any (a which-kind
// and a how-many-per-card ask), which stays one loud Note with nothing moved.
func TestEventidesShadowAnyAmountStaysLoudlyWithheld(t *testing.T) {
	card, trig := corpusSA(t, "Eventide's Shadow", "")
	if trig.API != "RemoveCounter" || !strings.EqualFold(trig.Params["CounterNum"], "Any") ||
		!strings.EqualFold(trig.Params["ChoiceOptional"], "True") {
		t.Fatalf("Eventide's Shadow spell = %s %+v, want RemoveCounter ChoiceOptional$ True CounterNum$ Any", trig.API, trig.Params)
	}
	h := newHost(t, 2)
	src := h.g.AddObject(card, 0).ID
	bear := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0).ID
	h.Emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZLibrary, To: state.ZBattlefield})
	h.Emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 2})
	c := &Ctx{Controller: 0, Source: src, SVars: h.g.Obj(src).Face().SVars}
	head := *trig
	head.Sub = nil
	Resolve(h, c, &head)
	if got := h.g.Obj(bear).Counter("P1P1"); got != 2 {
		t.Fatalf("bear P1P1 = %d, want 2 (a withheld shape moves nothing)", got)
	}
	if len(c.Remembered) != 0 {
		t.Fatalf("Remembered = %+v, want empty (nothing was removed)", c.Remembered)
	}
	var loud bool
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented RemoveCounter choice shape") &&
			strings.Contains(ev.Text, "CounterNum$ Any") {
			loud = true
		}
	}
	if !loud {
		t.Fatalf("withheld shape emitted no loud Note; log = %+v", h.log)
	}
}

func assertNoRemoveCounterChoiceNote(t *testing.T, h *fakeHost) {
	t.Helper()
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("unexpected unimplemented Note: %q", ev.Text)
		}
	}
}
