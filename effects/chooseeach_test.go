package effects

// ChooseEach$ on ChooseCard (param:api:ChooseCard.ChooseEach): instead of one
// pick per chooser, the chooser picks ONE card PER GROUP of the " & "
// separated ChooseEach$ value, over the same Choices$ pool. The real corpus
// carrier is Tragic Arrogance's YouChoose body, driven here by its own
// RepeatEach walk (RepeatPlayers$ Player): each iteration's ChooseCard asks
// the spell's controller once per group among the remembered player's
// permanents, each pick is an ordinary "choice" ask answered in place (the
// flat (chooser, group) ResumeTarget index), the picks
// accumulate into Chosen/Remembered across the groups (RememberChosen$), and
// a group whose narrowed pool is empty resolves silently (no ask, nothing
// chosen) — Forge's per-type loop with no candidate of that type.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// countingAskHost is askHost that keeps EVERY posed decision in ask order —
// a per-group walk poses several in sequence and the last one alone is not
// enough to pin the walk's shape.
type countingAskHost struct {
	askHost
	asks []*decision.Decision
}

func (h *countingAskHost) Ask(d *decision.Decision) bool {
	cp := *d
	cp.Options = append([]decision.Option(nil), d.Options...)
	h.asks = append(h.asks, &cp)
	return true
}

func tragicArroganceBoard(t *testing.T) (*countingAskHost, *Ctx, *state.Object, *state.Object, *state.Object, *state.Object) {
	t.Helper()
	card, chooseSA := corpusSA(t, "Tragic Arrogance", "YouChoose")
	if chooseSA.API != "ChooseCard" || chooseSA.Params["ChooseEach"] != "Artifact & Creature & Enchantment & Planeswalker" {
		t.Fatalf("Tragic Arrogance's YouChoose SA changed: %+v", chooseSA)
	}
	h := &countingAskHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(card, 0)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
	// Seat 1 controls the pool ControlledByPlayer$ Remembered narrows to: one
	// card per group plus a SECOND creature, so the creature group's answer
	// can be a real election (not a forced sole option), and no planeswalker,
	// so that group's ask must resolve silently.
	artifact := h.g.AddObject(mkCard(t, "Name:TA Art\nTypes:Artifact\nOracle:x\n"), 1)
	creFirst := h.g.AddObject(mkCard(t, "Name:TA Cre One\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	creSecond := h.g.AddObject(mkCard(t, "Name:TA Cre Two\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	ench := h.g.AddObject(mkCard(t, "Name:TA Ench\nTypes:Enchantment\nOracle:x\n"), 1)
	for _, o := range []*state.Object{artifact, creFirst, creSecond, ench} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Player: 1, IsPlayer: true}}}
	return h, c, artifact, creFirst, creSecond, ench
}

// TestChooseCardChooseEachNoHostTakesFirstPerGroup pins the no-host stand-in:
// with a host that cannot ask, every Mandatory group takes its first
// eligible candidate in group order (the same R-9 deterministic path the
// single-pool shape runs, applied per group).
func TestChooseCardChooseEachNoHostTakesFirstPerGroup(t *testing.T) {
	card, chooseSA := corpusSA(t, "Tragic Arrogance", "YouChoose")
	h := newHost(t, 2)
	src := h.g.AddObject(card, 0)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
	artifact := h.g.AddObject(mkCard(t, "Name:TA Art\nTypes:Artifact\nOracle:x\n"), 1)
	creFirst := h.g.AddObject(mkCard(t, "Name:TA Cre One\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	ench := h.g.AddObject(mkCard(t, "Name:TA Ench\nTypes:Enchantment\nOracle:x\n"), 1)
	for _, o := range []*state.Object{artifact, creFirst, ench} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Player: 1, IsPlayer: true}}}
	Resolve(h, c, chooseSA)
	want := []state.Target{{Obj: artifact.ID}, {Obj: creFirst.ID}, {Obj: ench.ID}}
	if len(c.Chosen) != len(want) || c.Chosen[0].Obj != artifact.ID || c.Chosen[1].Obj != creFirst.ID || c.Chosen[2].Obj != ench.ID {
		t.Fatalf("no-host Chosen = %+v, want each group's first eligible (%+v)", c.Chosen, want)
	}
}
