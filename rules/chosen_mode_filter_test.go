package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// chosenModeCast chooses the real ETBReplacement mode, then lets the spell
// resolve. The recorded mode belongs to the battlefield object, not a spell
// or a resolution-local context.
func chosenModeCast(t *testing.T, e *Engine, name, mode string) state.ObjID {
	t.Helper()
	id, d := castSearchSpell(t, e, name)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("%s entry choice = %+v, want modes", name, d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("%s mode options = %+v, want two", name, d.Options)
	}
	chosen := -1
	for _, o := range d.Options {
		if o.Label == mode {
			chosen = o.Index
		}
	}
	if chosen < 0 {
		t.Fatalf("%s not offered in %+v", mode, d.Options)
	}
	submitChoices(t, e, chosen)
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || !slices.Contains(o.ChosenModes, mode) {
		t.Fatalf("%s entry = %+v, want battlefield with mode %s", name, o, mode)
	}
	return id
}

func TestChosenModePredicateGatesModeTriggers(t *testing.T) {
	t.Parallel()
	g := &state.Game{NextID: 3, Objs: []state.Object{
		{ID: 1, Zone: state.ZBattlefield, ChosenModes: []string{"Khans"}},
		{ID: 2, Zone: state.ZBattlefield},
	}}
	if !effects.MatchesSpecCtx(g, "Card.ChosenModeKhans", 1, effects.SpecContext{}) ||
		effects.MatchesSpecCtx(g, "Card.ChosenModeDragons", 1, effects.SpecContext{}) ||
		effects.MatchesSpecCtx(g, "Card.ChosenModeKhans", 2, effects.SpecContext{}) {
		t.Fatal("ChosenMode did not match exactly the candidate's recorded mode")
	}
	if u := effects.UnknownPredicates("Card.ChosenModeKhans"); len(u) != 0 {
		t.Fatalf("recognised ChosenMode classified unknown: %v", u)
	}

	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Outpost Siege")
	id := chosenModeCast(t, e, "Outpost Siege", "Khans")
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("trigger source not on battlefield: %+v", o)
	}
	before := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if len(before) == 0 {
		t.Fatal("no library card for Khans trigger to exile")
	}
	start := len(e.L.Events)
	// The next upkeep belongs to the caster. The trigger is granted by the
	// ChosenModeKhans static and must dig the top library card into exile.
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepUpkeep)
	passUntilStackEmpty(t, e, 40)
	moved := false
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZExile && slices.Contains(before, ev.Obj) {
			moved = true
		}
	}
	if !moved {
		t.Fatal("Outpost Siege Khans granted no upkeep exile-top trigger")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZHand})
	if o := e.G.Obj(id); o.Zone != state.ZHand || len(o.ChosenModes) != 0 {
		t.Fatalf("leaving the battlefield kept the old Khans choice: %+v", o)
	}
	replayCheck(t, e, cfg)
}

// Each subtest starts from a fresh cast: a decline cannot poison the accept
// path, and both answers have the same legal Min-0 hidden-pick ask.
func TestPhenomenonInvestigatorsDoubtMayReturn(t *testing.T) {
	t.Parallel()
	for _, accept := range []bool{false, true} {
		name := "decline"
		if accept {
			name = "accept"
		}
		t.Run(name, func(t *testing.T) {
			reg := searchTestRegistry(t)
			e, cfg := searchEngine(t, reg, "Phenomenon Investigators")
			bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
			if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Owner != 0 {
				t.Fatalf("eligible nonland bear not owned on battlefield: %+v", o)
			}
			id := chosenModeCast(t, e, "Phenomenon Investigators", "Doubt")
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("trigger source not on battlefield: %+v", o)
			}
			// Passing to this turn's end step must reach the actual pick, not
			// merely a priority decision with no mode-gated trigger behind it.
			var d *decision.Decision
			for i := 0; i < 300 && e.G.Turn == 1; i++ {
				d = e.Pending()
				if d != nil && d.Kind == decision.KChoose && d.ResumeKind == "hidden_pick" {
					break
				}
				if d == nil {
					t.Fatal("no pending decision while advancing to end step")
				}
				switch d.Kind {
				case decision.KPriority:
					submitPass(t, e)
				case decision.KAttackers, decision.KBlockers:
					submitChoices(t, e)
				default:
					t.Fatalf("unexpected decision on way to end step: %+v", d)
				}
			}
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hidden_pick" ||
				e.G.Step != state.StepEnd || e.G.Active != 0 {
				t.Fatalf("Doubt at end step = %+v (turn %d step %s), want hidden pick", d, e.G.Turn, e.G.Step)
			}
			if d.Min != 0 || d.Max != 1 || d.Player != 0 {
				t.Fatalf("ChoiceOptional bounds/player = %d..%d seat %d, want 0..1 seat 0", d.Min, d.Max, d.Player)
			}
			if !slices.Contains(optionIDs(d), bear) || !slices.Contains(optionIDs(d), id) {
				t.Fatalf("hidden pick missing owned nonland permanents: %+v", d.Options)
			}
			for _, opt := range d.Options {
				o := e.G.Obj(opt.Obj)
				if o == nil || o.Zone != state.ZBattlefield || o.Owner != 0 ||
					effects.MatchesSpecCtx(e.G, "Land", opt.Obj, effects.SpecContext{}) {
					t.Fatalf("offered ineligible permanent: %+v / %+v", opt, o)
				}
			}
			start := len(e.L.Events)
			if accept {
				chooseByObj(t, e, bear)
			} else {
				submitChoices(t, e)
			}
			passUntilStackEmpty(t, e, 40)
			wantZone, wantDraw := state.ZBattlefield, 0
			if accept {
				wantZone, wantDraw = state.ZHand, 1
			}
			if got := e.G.Obj(bear).Zone; got != wantZone {
				t.Fatalf("bear zone = %s, want %s", got, wantZone)
			}
			if got := logDrawsFor(e, start, 0); got != wantDraw {
				t.Fatalf("Doubt drew %d cards after %s, want %d", got, name, wantDraw)
			}
			if !accept && len(e.G.Obj(id).Remembered) != 0 {
				t.Fatalf("decline remembered a card: %+v", e.G.Obj(id).Remembered)
			}
			replayCheck(t, e, cfg)
		})
	}
}
