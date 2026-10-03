package rules

// W3 step 5 (the park-and-continue paths, lasagna spec §7.2): a park ask is
// answered from the tape at the point of the parked event, so the kernel's
// event order deliberately differs from the legacy park's. These tests run
// each scenario on the kernel, require its asks served with no legacy ask
// ending the run, a log-only replay that matches, and the parked event
// applied in place: before the resolving object leaves the stack.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// tapePark runs scenario on the kernel only and returns the engine and the
// kernel counter deltas, requiring at least minServed served answers, no
// legacy switch or abort, and a matching log-only replay.
func tapePark(t *testing.T, seats int, seed uint64, minServed int64, scenario func(t *testing.T, e *Engine), srcs ...string) (*Engine, resolve.Stats) {
	t.Helper()
	before := resolve.ReadStats()
	e, cfg := tapeFixture(t, seats, seed, true, srcs...)
	scenario(t, e)
	st := resolve.ReadStats().Sub(before)
	replayCheck(t, e, cfg)
	if testing.Verbose() {
		for _, ev := range e.L.Events[max(0, len(e.L.Events)-40):] {
			t.Log(tapeEventString(ev))
		}
	}
	if st.Served < minServed || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("the park asks were not served from the tape: %+v", st)
	}
	return e, st
}

// tapeEventIndex is the index of the first event of kind k (with obj, when
// non-zero) at or after from, or -1.
func tapeEventIndex(e *Engine, from int, k events.Kind, obj state.ObjID) int {
	for i := from; i < len(e.L.Events); i++ {
		if ev := e.L.Events[i]; ev.Kind == k && (obj == 0 || ev.Obj == obj) {
			return i
		}
	}
	return -1
}

const tapeBladeSrc = "Name:Tape Blade\nManaCost:1\nTypes:Artifact Equipment\n" +
	"R:Event$ Attached | ValidCard$ Card.Self | ValidTarget$ Creature | ReplaceWith$ ChooseColor | ActiveZones$ Battlefield | Description$ x\n" +
	"SVar:ChooseColor:DB$ ChooseColor | Defined$ You\nK:Equip:1\nOracle:x\n"

const tapePaperSrc = "Name:Tape Paper\nManaCost:1\nTypes:Artifact Equipment\n" +
	"R:Event$ Attached | ValidCard$ Card.Self | ValidTarget$ Creature | ReplaceWith$ ChooseName | ActiveZones$ Battlefield | Description$ x\n" +
	"SVar:ChooseName:DB$ NameCard | Defined$ You | ValidCards$ Creature | ValidDescription$ creature card\nK:Equip:1\nOracle:x\n"

// An Attached replacement's election (Sanctuary Blade's colour, Psychic
// Paper's name and creature type) is answered as the Attach happens: the
// Attach applies before the rest of the spell's chain and before the spell
// leaves the stack.
func TestTapeParkAttached(t *testing.T) {
	for i, eq := range []string{"Tape Blade", "Tape Paper"} {
		t.Run(eq, func(t *testing.T) {
			name := "Tape Equip " + eq
			src := "Name:" + name + "\nManaCost:U\nTypes:Sorcery\n" +
				"A:SP$ Attach | Object$ Valid Equipment.YouCtrl | Defined$ Valid Creature.YouCtrl | SubAbility$ DBGain\n" +
				tapeGainSVar + "\nOracle:x\n"
			var spell state.ObjID
			e, _ := tapePark(t, 2, 13900+uint64(i), 1, func(t *testing.T, e *Engine) {
				moveByName(t, e, 0, eq, state.ZBattlefield)
				moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
				spell = fixtureInHand(t, e, name)
				tapeCastAndResolve(t, e, name, "U")
			}, src, tapeBladeSrc, tapePaperSrc, ptResumeBearSrc)
			ask := -1
			for i, ev := range e.L.Events {
				if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KChoose) {
					ask = i
				}
			}
			att := tapeEventIndex(e, 0, events.Attach, 0)
			left := tapeEventIndex(e, 0, events.MoveZone, spell)
			for left >= 0 && e.L.Events[left].From != state.ZStack {
				left = tapeEventIndex(e, left+1, events.MoveZone, spell)
			}
			if ask < 0 || att < 0 || left < 0 || att > left {
				t.Fatalf("Attach at %d, spell left the stack at %d (last choose ask %d): the election was not answered in place", att, left, ask)
			}
		})
	}
}

const tapePrismBearSrc = "Name:Tape Prism Bear\nManaCost:B\nTypes:Creature Bear\nPT:2/2\nK:ETBReplacement:Other:ChooseColor\nSVar:ChooseColor:DB$ ChooseColor\nOracle:x\n"

const tapeTotemSrc = "Name:Tape Totem\nManaCost:B\nTypes:Artifact\nK:ETBReplacement:Other:ChooseCT\nSVar:ChooseCT:DB$ ChooseType | Type$ Creature\n" +
	"K:ETBReplacement:Other:DBChoose\nSVar:DBChoose:DB$ ChooseCard | Defined$ You | Choices$ Land.YouCtrl | ChoiceZone$ Battlefield | Mandatory$ True\nOracle:x\n"

// An entry an effect makes mid-chain (a mass return from the graveyard)
// asks its as-enters choices -- and its entry replacement body's ask -- in
// place: each permanent enters, answered, before the effect moves on and
// before the spell leaves the stack.
func TestTapeParkMidChainEntry(t *testing.T) {
	name := "Tape Mass Return"
	src := "Name:" + name + "\nManaCost:U\nTypes:Sorcery\n" +
		"A:SP$ ChangeZoneAll | ChangeType$ Creature.YouOwn,Artifact.YouOwn | Origin$ Graveyard | Destination$ Battlefield | SubAbility$ DBGain\n" +
		tapeGainSVar + "\nOracle:x\n"
	var spell, bear, totem state.ObjID
	e, _ := tapePark(t, 2, 13950, 3, func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Mountain", state.ZBattlefield)
		moveByName(t, e, 0, "Mountain", state.ZBattlefield)
		bear = moveByName(t, e, 0, "Tape Prism Bear", state.ZGraveyard)
		totem = moveByName(t, e, 0, "Tape Totem", state.ZGraveyard)
		spell = fixtureInHand(t, e, name)
		tapeCastAndResolve(t, e, name, "U")
	}, src, tapePrismBearSrc, tapeTotemSrc)
	left := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == spell && ev.From == state.ZStack {
			left = i
		}
	}
	for _, id := range []state.ObjID{bear, totem} {
		in := -1
		for i, ev := range e.L.Events {
			if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
				in = i
			}
		}
		if in < 0 || left < 0 || in > left {
			t.Fatalf("%s entered at %d, the spell left the stack at %d: the entry was not answered in place", e.Name(id), in, left)
		}
	}
}
