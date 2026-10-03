package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// reflectedBoard builds a 2-seat game with a source artifact (seat 0) and two
// differently coloured creatures (seat 0). An
// "AB$ ManaReflected | ReflectProperty$ Is | Valid$ Creature" ability's
// reflected set is therefore exactly {W, U} — a genuine two-way choice.
func reflectedBoard(t *testing.T) (*fakeHost, *Ctx, *cards.SA) {
	t.Helper()
	h := &fakeHost{g: state.NewGame(names(2))}
	src := h.g.AddObject(mkCard(t, "Name:Reflector\nTypes:Artifact\nOracle:x\n"), 0)
	w := h.g.AddObject(mkCard(t, "Name:White Helper\nManaCost:W\nTypes:Creature Soldier\nPT:1/1\nOracle:x\n"), 0)
	u := h.g.AddObject(mkCard(t, "Name:Blue Helper\nManaCost:U\nTypes:Creature Wizard\nPT:1/1\nOracle:x\n"), 0)
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID, w.ID, u.ID})
	s := sa(t, "AB$ ManaReflected | Cost$ T | ReflectProperty$ Is | ColorOrType$ Color | Valid$ Creature")
	return h, &Ctx{Source: src.ID, Controller: 0}, s
}

// TestManaReflectedStandaloneFallsBackWhenHostCannotAsk is R-9's no-ask
// contract for this effect: a host whose Ask returns false still resolves
// deterministically — the first candidate in the fixed order (W before U) —
// and records the Note naming the stand-in.
func TestManaReflectedStandaloneFallsBackWhenHostCannotAsk(t *testing.T) {
	h, c, s := reflectedBoard(t)
	plain := &fakeHost{g: h.g}

	effManaReflected(plain, c, s)

	if got := poolCount(t, plain.log, "W"); got != 1 {
		t.Fatalf("no-ask host added W %d times, want 1 (the deterministic first candidate)", got)
	}
	if got := poolCount(t, plain.log, "U"); got != 0 {
		t.Fatalf("no-ask host added U %d times, want 0", got)
	}
	found := false
	for _, ev := range plain.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "chose first reflected colour") {
			found = true
		}
	}
	if !found {
		t.Fatal("no-ask host recorded no first-candidate Note")
	}
}

// poolCount counts the ManaAdd events for one colour in a captured log.
func poolCount(t *testing.T, log []events.Event, colour string) int {
	t.Helper()
	n := 0
	for _, ev := range log {
		if ev.Kind != events.ManaAdd {
			continue
		}
		// Reflector is an Artifact: keep asserting its mana carries the
		// producer tag as well as the chosen colour.
		if tag, _, ok := state.TypedManaCounter(ev.Counter); !ok || tag != state.TypedArtifact {
			t.Fatalf("Reflector emitted untagged mana: %q", ev.Counter)
		}
		if state.ManaSlot(ev.Counter) == state.ManaSlot(colour) {
			n++
		}
	}
	return n
}
