package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// These tests pin the std3 unread/fail-closed parameter fixes for the shapes
// the brief names: Animate Colors$ ChosenColor (Puca's Eye), PutCounter
// CounterTypes$ (Abigale), Pump ReplaceDyingDefined$ (Gnashing of Teeth),
// ChangeZone ThisDefinedAndTgts$ TopOfLibrary (Suspend Aggression) and
// DigUntil MinTotalCMC$ (Dream Harvest). Each asserts the precondition the
// assertion depends on, so a vacuous setup fails loudly rather than passing.

// battlefield puts a card on seat 0's battlefield and returns its id.
func ufBattlefield(t *testing.T, h *fakeHost, src string) state.ObjID {
	t.Helper()
	o := h.g.AddObject(mkCard(t, src), 0)
	h.g.Obj(o.ID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), o.ID))
	return o.ID
}

func ufNotes(h *fakeHost) []string {
	var out []string
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			out = append(out, ev.Text)
		}
	}
	return out
}

// TestAnimateChosenColorGrantsTheChosenColour: Colors$ ChosenColor resolves
// against the source's chosen colour instead of failing closed. Puca's Eye
// chooses red, then animates itself; the animation must be a real LColor
// overwrite to red with no fail-closed Note.
func TestAnimateChosenColorGrantsTheChosenColour(t *testing.T) {
	h := newHost(t, 2)
	id := ufBattlefield(t, h, "Name:Eye\nManaCost:2\nTypes:Artifact\nOracle:x\n")
	h.g.Obj(id).ChosenColor = "R" // precondition: the source actually chose a colour
	if h.g.Obj(id).ChosenColor != "R" {
		t.Fatal("precondition: source must carry a chosen colour")
	}
	Resolve(h, &Ctx{Source: id, Controller: 0},
		sa(t, "SP$ Animate | Defined$ Self | Colors$ ChosenColor | OverwriteColors$ True"))
	if len(h.continuous) != 1 {
		t.Fatalf("registered %d continuous effects, want 1: %+v", len(h.continuous), h.continuous)
	}
	ce := h.continuous[0]
	if ce.Layer != state.LColor || !ce.OverwriteColors || len(ce.AddColors) != 1 || ce.AddColors[0] != "R" {
		t.Fatalf("colour effect = %+v, want LColor overwrite to [R]", ce)
	}
	for _, n := range ufNotes(h) {
		if strings.Contains(n, "ChosenColor") {
			t.Fatalf("resolved ChosenColor still noted %q", n)
		}
	}
}

// TestPutCounterCounterTypesPlacesOneOfEach: CounterTypes$ puts one counter
// of each named kind (never a single default P1P1). Abigale's "a flying
// counter, a first strike counter, and a lifelink counter".
func TestPutCounterCounterTypesPlacesOneOfEach(t *testing.T) {
	h := newHost(t, 2)
	id := ufBattlefield(t, h, "Name:Abigale\nManaCost:WB\nTypes:Creature Bird\nPT:1/1\nOracle:x\n")
	Resolve(h, &Ctx{Source: id, Controller: 0},
		sa(t, "DB$ PutCounter | Defined$ Self | CounterTypes$ Flying,First Strike,Lifelink"))
	o := h.g.Obj(id)
	if o == nil {
		t.Fatal("precondition: recipient must still exist")
	}
	for _, k := range []string{"Flying", "First Strike", "Lifelink"} {
		if o.Counter(k) != 1 {
			t.Fatalf("counter %q = %d, want 1 (all counters: %+v)", k, o.Counter(k), o.Counters)
		}
	}
	if o.Counter("P1P1") != 0 {
		t.Fatalf("default P1P1 = %d, want 0 (CounterTypes$ must not fall back to P1P1)", o.Counter("P1P1"))
	}
}

// TestPumpReplaceDyingDefinedRegistersExile: Pump's ReplaceDyingDefined$
// registers the same battlefield-to-graveyard-exile replacement DealDamage's
// rider builds, and the parameter is no longer reported unread (Gnashing of
// Teeth's -5/-5 "if that creature would die this turn, exile it instead").
func TestPumpReplaceDyingDefinedRegistersExile(t *testing.T) {
	h := newHost(t, 2)
	id := ufBattlefield(t, h, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	line := "SP$ Pump | Defined$ Self | NumAtt$ -5 | NumDef$ -5 | ReplaceDyingDefined$ Targeted"
	if p := PumpOf(sa(t, line)); len(p.Unread) != 0 {
		t.Fatalf("Pump Unread = %v, want empty (ReplaceDyingDefined$ must be consumed)", p.Unread)
	}
	Resolve(h, &Ctx{Source: id, Controller: 0}, sa(t, line))
	var repl *state.ContinuousEffect
	for i := range h.continuous {
		if h.continuous[i].ReplacementEvent == "Moved" {
			repl = &h.continuous[i]
		}
	}
	if repl == nil {
		t.Fatalf("no Moved replacement registered; got %+v, notes %v", h.continuous, ufNotes(h))
	}
	if repl.ReplacementParams["Origin"] != "Battlefield" || repl.ReplacementParams["Destination"] != "Graveyard" ||
		repl.ReplacementParams["ValidCard"] != "Card.IsRemembered" {
		t.Fatalf("replacement params = %+v, want battlefield->graveyard exile", repl.ReplacementParams)
	}
	found := false
	for _, rid := range repl.Remembered {
		if rid == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("remembered %v does not include the pumped object %d", repl.Remembered, id)
	}
	for _, n := range ufNotes(h) {
		if strings.Contains(n, "ReplaceDyingDefined") {
			t.Fatalf("pump emitted unread note %q", n)
		}
	}
}

// TestChangeZoneThisDefinedAndTgtsTopOfLibrary: Suspend Aggression's
// ThisDefinedAndTgts$ TopOfLibrary adds the top card of the controller's
// library to the move, so both the source and the top card are exiled.
func TestChangeZoneThisDefinedAndTgtsTopOfLibrary(t *testing.T) {
	h := newHost(t, 2)
	src := ufBattlefield(t, h, "Name:Spell\nTypes:Sorcery\nOracle:x\n")
	top := h.g.AddObject(mkCard(t, "Name:TopCard\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID
	below := h.g.AddObject(mkCard(t, "Name:Below\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{top, below})
	if got := h.g.Zone(state.ZLibrary, 0); len(got) != 2 || got[0] != top {
		t.Fatalf("precondition: library top must be %d, got %v", top, got)
	}
	Resolve(h, &Ctx{Source: src, Controller: 0},
		sa(t, "SP$ ChangeZone | Defined$ Self | ThisDefinedAndTgts$ TopOfLibrary | Destination$ Exile"))
	if z := h.g.Obj(src).Zone; z != state.ZExile {
		t.Fatalf("source zone = %v, want Exile", z)
	}
	if z := h.g.Obj(top).Zone; z != state.ZExile {
		t.Fatalf("top library card zone = %v, want Exile (ThisDefinedAndTgts$ TopOfLibrary must add it)", z)
	}
	if z := h.g.Obj(below).Zone; z != state.ZLibrary {
		t.Fatalf("second library card zone = %v, want Library (only the top moves)", z)
	}
	for _, n := range ufNotes(h) {
		if strings.Contains(n, "ThisDefinedAndTgts") {
			t.Fatalf("resolved ThisDefinedAndTgts still noted %q", n)
		}
	}
}

// TestDigUntilMinTotalCMCRevealsPastTheThreshold: MinTotalCMC$ keeps revealing
// until the matching cards' cumulative mana value reaches the threshold. A
// library of zero-mana-value cards must therefore be exiled WHOLE (XMage and
// the Oracle both read it that way), not stopped after one card.
func TestDigUntilMinTotalCMCRevealsPastTheThreshold(t *testing.T) {
	h := newHost(t, 2)
	// No ManaCost -> mana value 0 for every card.
	var lib []state.ObjID
	for _, nm := range []string{"A", "B", "C", "D"} {
		lib = append(lib, h.g.AddObject(mkCard(t, "Name:"+nm+"\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID)
	}
	h.g.SetZone(state.ZLibrary, 0, lib)
	for _, id := range lib {
		if mv := h.g.Obj(id).Face().ManaValue(); mv != 0 {
			t.Fatalf("precondition: %d mana value = %d, want 0", id, mv)
		}
	}
	Resolve(h, &Ctx{Controller: 0},
		sa(t, "SP$ DigUntil | Defined$ You | MinTotalCMC$ 5 | FoundDestination$ Exile | RevealedDestination$ Exile"))
	if got := h.g.Zone(state.ZLibrary, 0); len(got) != 0 {
		t.Fatalf("library has %d cards left, want 0 (whole 0-MV library exiles)", len(got))
	}
	var reveal *events.Event
	for i := range h.log {
		if h.log[i].Kind == events.Note && len(h.log[i].IDs) > 0 && !h.log[i].Secret {
			reveal = &h.log[i]
			break
		}
	}
	if reveal == nil || len(reveal.IDs) != 4 {
		t.Fatalf("reveal note = %+v, want all 4 cards revealed", reveal)
	}
}
