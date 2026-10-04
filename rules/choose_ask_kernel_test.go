package rules

// Restored from effects/choose_color_ask_test.go, choose_number_ask_test.go,
// choose_number_bounds_test.go, choose_type_ask_test.go,
// choose_type_categories_test.go and charm_suspension_test.go (W3 legacy
// removal): the resolution-time choose asks, answered through the
// resolution kernel on a real engine.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kr0ChooseEvents collects the Choose events of the given Counter in evs.
func kr0ChooseEvents(evs []events.Event, counter string) []events.Event {
	var out []events.Event
	for _, ev := range evs {
		if ev.Kind == events.Choose && ev.Counter == counter {
			out = append(out, ev)
		}
	}
	return out
}

// kr0ChooseSource is a land source on seat 0's battlefield and a Ctx maker.
func kr0ChooseSource(t *testing.T) (*Engine, state.ObjID, func() *effects.Ctx) {
	t.Helper()
	e := kr0Engine(t, 2)
	src := kr0Src(t, e, 0, "Name:Source\nTypes:Land\nOracle:x\n", state.ZBattlefield)
	return e, src, func() *effects.Ctx { return &effects.Ctx{Source: src, Controller: 0} }
}

// TestChooseColorAsksAndReEntryEmitsTheAnsweredLetterOnceKernel: a
// resolution-time ChooseColor poses a KChoose over the five WUBRG colours (in
// order, full names) to the Defined$ player with no Choose event before the
// answer; the answer records exactly one Choose carrying its letter, and no
// second ask is posed.
func TestChooseColorAsksAndReEntryEmitsTheAnsweredLetterOnceKernel(t *testing.T) {
	t.Parallel()
	e, src, mk := kr0ChooseSource(t)
	start := len(e.L.Events)
	d := kr0Run(t, e, kr0SA(t, "SP$ ChooseColor | Defined$ You"), mk, nil)
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.Min != 1 || d.Max != 1 ||
		d.ResumeKind != "choosecolor" || d.Source != src || d.Prompt != "Choose a color" {
		t.Fatalf("ChooseColor ask shape wrong: %+v", d)
	}
	want := []string{"White", "Blue", "Black", "Red", "Green"}
	if len(d.Options) != len(want) {
		t.Fatalf("options = %+v, want the five WUBRG colours", d.Options)
	}
	for i, w := range want {
		if d.Options[i].Kind != "color" || d.Options[i].Label != w || d.Options[i].Index != i {
			t.Fatalf("option %d = %+v, want color %q", i, d.Options[i], w)
		}
	}
	if evs := kr0ChooseEvents(kr0Since(e, start), "color"); len(evs) != 0 {
		t.Fatalf("Choose event(s) before the answer: %+v", evs)
	}
	if next := kr0Answer(t, e, kr0Label(t, d, "Black")); next != nil {
		t.Fatalf("the answer posed a second ask: %+v", next)
	}
	evs := kr0ChooseEvents(kr0Since(e, start), "color")
	if len(evs) != 1 || evs[0].Text != "B" || evs[0].Obj != src {
		t.Fatalf("Choose events = %+v, want exactly one Choose \"B\" on the source", evs)
	}
	if got := e.G.Obj(src).ChosenColor; got != "B" {
		t.Fatalf("ChosenColor = %q, want B", got)
	}
}

// TestChooseColorChoicesRestrictsTheAskAndForcesASingleOfferKernel: Choices$
// with two colours offers exactly those (WUBRG order); with one colour the
// choice is forced and recorded without an ask.
func TestChooseColorChoicesRestrictsTheAskAndForcesASingleOfferKernel(t *testing.T) {
	t.Parallel()
	e, src, mk := kr0ChooseSource(t)
	d := kr0Run(t, e, kr0SA(t, "SP$ ChooseColor | Defined$ You | Choices$ black,red"), mk, nil)
	if d == nil || len(d.Options) != 2 || d.Options[0].Label != "Black" || d.Options[1].Label != "Red" {
		t.Fatalf("Choices$ ask = %+v, want one ask over Black/Red", d)
	}
	kr0Answer(t, e, kr0Label(t, d, "Red"))
	if got := e.G.Obj(src).ChosenColor; got != "R" {
		t.Fatalf("ChosenColor = %q, want R", got)
	}

	e2, src2, mk2 := kr0ChooseSource(t)
	if d := kr0Run(t, e2, kr0SA(t, "SP$ ChooseColor | Defined$ You | Choices$ Red"), mk2, nil); d != nil {
		t.Fatalf("single-offer Choices$ posed %+v, want no ask", d)
	}
	if got := e2.G.Obj(src2).ChosenColor; got != "R" {
		t.Fatalf("single-offer choice = %q, want R", got)
	}
}

// TestChooseColorFreshAskAfterAnEarlierChoiceOnTheSourceKernel: a ChosenColor
// already on the source is an earlier choice's answer; a fresh ChooseColor
// still asks, and its answer replaces the stale colour.
func TestChooseColorFreshAskAfterAnEarlierChoiceOnTheSourceKernel(t *testing.T) {
	t.Parallel()
	e, src, mk := kr0ChooseSource(t)
	e.emit(events.Event{Kind: events.Choose, Obj: src, Counter: "color", Text: "G"})
	if got := e.G.Obj(src).ChosenColor; got != "G" {
		t.Fatalf("precondition: the earlier choice did not record: %q", got)
	}
	start := len(e.L.Events)
	d := kr0Run(t, e, kr0SA(t, "SP$ ChooseColor | Defined$ You"), mk, nil)
	if d == nil || d.ResumeKind != "choosecolor" || len(d.Options) != 5 {
		t.Fatalf("fresh ask = %+v, want the five-colour choosecolor ask", d)
	}
	kr0Answer(t, e, kr0Label(t, d, "Black"))
	evs := kr0ChooseEvents(kr0Since(e, start), "color")
	if len(evs) != 1 || evs[0].Text != "B" {
		t.Fatalf("Choose events = %+v, want exactly one Choose \"B\"", evs)
	}
	if got := e.G.Obj(src).ChosenColor; got != "B" {
		t.Fatalf("ChosenColor = %q, want the new answer B (the stale G must not survive)", got)
	}
}

// TestChooseNumberAsksAndReEntryEmitsTheAnsweredNumberOnceKernel: a
// resolution-time ChooseNumber poses a KChoose over a number list (values on
// Amount) with no Choose event before the answer; the answer -- including a
// legal ZERO -- records exactly one Choose with its Amount and no re-ask.
func TestChooseNumberAsksAndReEntryEmitsTheAnsweredNumberOnceKernel(t *testing.T) {
	t.Parallel()
	for _, pick := range []int{4, 0} {
		e, src, mk := kr0ChooseSource(t)
		start := len(e.L.Events)
		d := kr0Run(t, e, kr0SA(t, "SP$ ChooseNumber | Defined$ You"), mk, nil)
		if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.Min != 1 || d.Max != 1 ||
			d.ResumeKind != "choosenumber" || d.Source != src || d.Prompt != "Choose a number" {
			t.Fatalf("ChooseNumber ask shape wrong: %+v", d)
		}
		if len(d.Options) < 5 {
			t.Fatalf("option list = %+v, want a non-trivial number list", d.Options)
		}
		for i, o := range d.Options {
			if o.Kind != "number" || o.Amount != i {
				t.Fatalf("option %d = %+v, want Kind number and Amount %d", i, o, i)
			}
		}
		if evs := kr0ChooseEvents(kr0Since(e, start), "number"); len(evs) != 0 {
			t.Fatalf("Choose event(s) before the answer: %+v", evs)
		}
		if next := kr0Answer(t, e, pick); next != nil {
			t.Fatalf("the answer posed a second ask: %+v", next)
		}
		evs := kr0ChooseEvents(kr0Since(e, start), "number")
		if len(evs) != 1 || int(evs[0].Amount) != pick || evs[0].Obj != src {
			t.Fatalf("pick %d: Choose events = %+v, want exactly one Choose Amount %d", pick, evs, pick)
		}
		if int(e.G.Obj(src).ChosenNumber) != pick {
			t.Fatalf("ChosenNumber = %d, want %d", e.G.Obj(src).ChosenNumber, pick)
		}
	}
}

// TestChooseNumberFreshAskAfterAnEarlierChoiceOnTheSourceKernel: a
// ChosenNumber already on the source does not short-circuit a fresh ask.
func TestChooseNumberFreshAskAfterAnEarlierChoiceOnTheSourceKernel(t *testing.T) {
	t.Parallel()
	e, src, mk := kr0ChooseSource(t)
	e.emit(events.Event{Kind: events.Choose, Obj: src, Counter: "number", Amount: 5})
	if got := e.G.Obj(src).ChosenNumber; got != 5 {
		t.Fatalf("precondition: the earlier choice did not record: %d", got)
	}
	d := kr0Run(t, e, kr0SA(t, "SP$ ChooseNumber | Defined$ You"), mk, nil)
	if d == nil || d.ResumeKind != "choosenumber" || len(d.Options) < 2 {
		t.Fatalf("fresh ask = %+v, want a choosenumber ask", d)
	}
	kr0Answer(t, e, 2)
	if got := e.G.Obj(src).ChosenNumber; got != 2 {
		t.Fatalf("ChosenNumber = %d, want the new answer 2", got)
	}
}

// TestChooseNumberBoundAnswerResumeReEntryKernel: Max$ bound by the
// controller's energy (2) shapes the offered list (0..2); answering 2
// records it.
func TestChooseNumberBoundAnswerResumeReEntryKernel(t *testing.T) {
	t.Parallel()
	e, src, mk := kr0ChooseSource(t)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 2})
	start := len(e.L.Events)
	d := kr0Run(t, e, kr0SA(t, "DB$ ChooseNumber | Max$ Count$YourCountersEnergy"), mk, nil)
	if d == nil || len(d.Options) != 3 {
		t.Fatalf("bounded ask = %+v, want the three options 0..2", d)
	}
	two := -1
	for _, o := range d.Options {
		if o.Amount == 2 {
			two = o.Index
		}
	}
	if two < 0 {
		t.Fatalf("the bounded list offers no 2: %+v", d.Options)
	}
	kr0Answer(t, e, two)
	evs := kr0ChooseEvents(kr0Since(e, start), "number")
	if len(evs) != 1 || evs[0].Amount != 2 {
		t.Fatalf("Choose events = %+v, want exactly one Amount 2", evs)
	}
	if e.G.Obj(src).ChosenNumber != 2 {
		t.Fatalf("ChosenNumber = %d, want 2", e.G.Obj(src).ChosenNumber)
	}
}

// TestChooseTypeAsksAndReEntryEmitsTheAnsweredTypeOnceKernel: a
// resolution-time ChooseType (creature) poses a KChoose of "type" options to
// the Defined$ player, no Choose before the answer, exactly one after.
func TestChooseTypeAsksAndReEntryEmitsTheAnsweredTypeOnceKernel(t *testing.T) {
	t.Parallel()
	e, src, mk := kr0ChooseSource(t)
	start := len(e.L.Events)
	d := kr0Run(t, e, kr0SA(t, "SP$ ChooseType | Defined$ You | Type$ Creature"), mk, nil)
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.Min != 1 || d.Max != 1 ||
		d.ResumeKind != "choosetype" || d.Source != src || d.Prompt != "Choose a creature type" {
		t.Fatalf("ChooseType ask shape wrong: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind != "type" {
			t.Fatalf("option %+v is not a \"type\" option", o)
		}
	}
	if evs := kr0ChooseEvents(kr0Since(e, start), "type"); len(evs) != 0 {
		t.Fatalf("Choose event(s) before the answer: %+v", evs)
	}
	if next := kr0Answer(t, e, kr0Label(t, d, "Zombie")); next != nil {
		t.Fatalf("the answer posed a second ask: %+v", next)
	}
	var got []events.Event
	for _, ev := range kr0Since(e, start) {
		if ev.Kind == events.Choose && ev.Obj == src && ev.Text == "Zombie" {
			got = append(got, ev)
		}
	}
	if len(got) != 1 {
		t.Fatalf("Choose \"Zombie\" events = %+v, want exactly one", got)
	}
	if e.G.Obj(src).ChosenType != "Zombie" {
		t.Fatalf("ChosenType = %q, want Zombie", e.G.Obj(src).ChosenType)
	}
}

// TestChooseTypeBasicLandOffersTheFiveBasicLandTypesKernel: the Basic Land
// category offers CR 205.3i's five basic land types, sorted, with the
// land-type prompt and no loud Note; the answer is recorded.
func TestChooseTypeBasicLandOffersTheFiveBasicLandTypesKernel(t *testing.T) {
	t.Parallel()
	e, src, mk := kr0ChooseSource(t)
	start := len(e.L.Events)
	d := kr0Run(t, e, kr0SA(t, "SP$ ChooseType | Defined$ You | Type$ Basic Land"), mk, nil)
	if d == nil || d.Prompt != "Choose a land type" {
		t.Fatalf("ask = %+v, want the land-type ask", d)
	}
	want := []string{"Forest", "Island", "Mountain", "Plains", "Swamp"}
	if len(d.Options) != len(want) {
		t.Fatalf("options = %+v, want %v", d.Options, want)
	}
	for i, w := range want {
		if d.Options[i].Label != w {
			t.Fatalf("option %d = %+v, want %q", i, d.Options[i], w)
		}
	}
	for _, ev := range kr0Since(e, start) {
		if ev.Kind == events.Note {
			t.Fatalf("an enumerable category emitted a Note: %q", ev.Text)
		}
	}
	kr0Answer(t, e, kr0Label(t, d, "Mountain"))
	if e.G.Obj(src).ChosenType != "Mountain" {
		t.Fatalf("ChosenType = %q, want Mountain", e.G.Obj(src).ChosenType)
	}
}

// TestCharmModeLoopStopsAtAMidModeSuspensionKernel: with the chosen modes
// [DoAsk, DoLose] and DoAsk's nested Charm posing its own mode ask, the later
// mode DoLose does not run while that ask is pending; after the answer the
// nested pick and then DoLose both run, once.
func TestCharmModeLoopStopsAtAMidModeSuspensionKernel(t *testing.T) {
	t.Parallel()
	e := kr0Engine(t, 2)
	src := kr0Src(t, e, 0, "Name:Source\nTypes:Land\nOracle:x\n", state.ZBattlefield)
	svars := map[string]string{
		"DoAsk":  "DB$ Charm | Choices$ InnerA,InnerB",
		"InnerA": "DB$ GainLife | Defined$ You | LifeAmount$ 1 | SpellDescription$ Gain 1",
		"InnerB": "DB$ GainLife | Defined$ You | LifeAmount$ 2 | SpellDescription$ Gain 2",
		"DoLose": "DB$ LoseLife | Defined$ You | LifeAmount$ 5",
	}
	if cards.ResolveSVar(svars, "DoAsk") == nil {
		t.Fatal("precondition: the DoAsk SVar did not resolve")
	}
	life := e.G.Players[0].Life
	d := kr0Run(t, e, kr0SA(t, "SP$ Charm | Choices$ DoAsk,DoLose"), func() *effects.Ctx {
		return &effects.Ctx{Source: src, Controller: 0, SVars: svars, Modes: []string{"DoAsk", "DoLose"}}
	}, nil)
	if d == nil || d.Kind != decision.KModes || len(d.Options) != 2 {
		t.Fatalf("nested Charm posed %+v, want its two-mode ask", d)
	}
	if got := e.G.Players[0].Life; got != life {
		t.Fatalf("life = %d, want %d: a later mode ran while the nested ask was pending", got, life)
	}
	if next := kr0Answer(t, e, 1); next != nil {
		t.Fatalf("unexpected further ask %+v", next)
	}
	if got := e.G.Players[0].Life; got != life+2-5 {
		t.Fatalf("life = %d, want %d (InnerB +2, then DoLose -5, once each)", got, life+2-5)
	}
}
