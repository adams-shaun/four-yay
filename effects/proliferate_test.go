package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Task prolif1: api:Proliferate (CR 701.27). The primitive was unregistered,
// so every `DB$ Proliferate` / `AB$ Proliferate` / `SP$ Proliferate` line in
// the corpus degraded to one "unimplemented API Proliferate" Note and added
// nothing. These effects-level leaves pin the primitive itself on synthetic
// scripts; the end-to-end real-corpus carriers live in
// rules/proliferate_test.go.

// proliferateSA builds the bare `DB$ Proliferate` shape with any extra
// parameters appended.
func proliferateSA(t *testing.T, extra string) *cards.SA {
	t.Helper()
	line := "DB$ Proliferate"
	if extra != "" {
		line += " | " + extra
	}
	return sa(t, line)
}

func TestProliferateZeroEligibleDoesNotAsk(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bare := h.g.AddObject(mkCard(t, "Name:Bare\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	bare.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{bare.ID})
	c := &Ctx{Source: bare.ID, Controller: 0}
	Resolve(h, c, proliferateSA(t, ""))
	if h.asked != nil {
		t.Fatalf("zero eligible recipients posed a decision: %+v", h.asked)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			t.Fatalf("zero eligible recipients emitted a Note: %q", ev.Text)
		}
	}
}

func TestProliferateSingleEligibleStillAsks(t *testing.T) {
	// The any-number convention, NOT putCounterChoose's strict-supersets
	// gate: "{} vs {that one}" is a real election even with one eligible.
	h := &askHost{}
	h.g = state.NewGame(names(2))
	carrier := h.g.AddObject(mkCard(t, "Name:Carrier\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	carrier.Zone = state.ZBattlefield
	carrier.AddCounter("P1P1", 1)
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{carrier.ID})
	c := &Ctx{Source: carrier.ID, Controller: 0}
	Resolve(h, c, proliferateSA(t, ""))
	if h.asked == nil {
		t.Fatal("a single eligible recipient did not pose the any-number ask")
	}
	if h.asked.Max != 1 || h.asked.Min != 0 {
		t.Fatalf("ask bounds = Min %d / Max %d, want 0/1", h.asked.Min, h.asked.Max)
	}
}

func TestProliferateNoHostTakesAllEligible(t *testing.T) {
	// fakeHost serves no answer: the R-9 stand-in takes every
	// eligible recipient with one Note, the exact mirror of botpolicy's arm.
	h := &fakeHost{}
	h.g = state.NewGame(names(2))
	carrier := h.g.AddObject(mkCard(t, "Name:Carrier\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	carrier.Zone = state.ZBattlefield
	carrier.AddCounter("P1P1", 1)
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{carrier.ID})
	h.g.Players[0].AddCounter("POISON", 1)
	c := &Ctx{Source: carrier.ID, Controller: 0}
	Resolve(h, c, proliferateSA(t, ""))
	if carrier.Counter("P1P1") != 2 {
		t.Fatalf("carrier P1P1 = %d, want 2", carrier.Counter("P1P1"))
	}
	if h.g.Players[0].Counter("POISON") != 2 {
		t.Fatalf("player POISON = %d, want 2", h.g.Players[0].Counter("POISON"))
	}
	notes := 0
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("no-host Notes = %d, want exactly 1", notes)
	}
}

func TestProliferateAmountZeroIsSilent(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	carrier := h.g.AddObject(mkCard(t, "Name:Carrier\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	carrier.Zone = state.ZBattlefield
	carrier.AddCounter("P1P1", 1)
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{carrier.ID})
	c := &Ctx{Source: carrier.ID, Controller: 0}
	Resolve(h, c, proliferateSA(t, "Amount$ 0"))
	if h.asked != nil {
		t.Fatal("Amount$ 0 posed an ask")
	}
	if carrier.Counter("P1P1") != 1 {
		t.Fatal("Amount$ 0 added a counter")
	}
}

func TestProliferateAmountXWithoutAnnouncementIsLoud(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	carrier := h.g.AddObject(mkCard(t, "Name:Carrier\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	carrier.Zone = state.ZBattlefield
	carrier.AddCounter("P1P1", 1)
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{carrier.ID})
	c := &Ctx{Source: carrier.ID, Controller: 0} // c.X == 0, unannounced
	Resolve(h, c, proliferateSA(t, "Amount$ X"))
	if h.asked != nil {
		t.Fatal("an unannounced Amount$ X posed an ask")
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			found = true
		}
	}
	if !found {
		t.Fatal("an unannounced Amount$ X did not loud-degrade")
	}
}

func TestProliferateUnknownParameterIsLoud(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	carrier := h.g.AddObject(mkCard(t, "Name:Carrier\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	carrier.Zone = state.ZBattlefield
	carrier.AddCounter("P1P1", 1)
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{carrier.ID})
	c := &Ctx{Source: carrier.ID, Controller: 0}
	Resolve(h, c, proliferateSA(t, "TotallyUnknown$ 7"))
	if h.asked != nil {
		t.Fatal("an unmodelled parameter still posed an ask")
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			found = true
		}
	}
	if !found {
		t.Fatal("an unmodelled parameter did not loud-degrade")
	}
}
