package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// chooseTypeHost is a scripted-ask double for the mid-resolution ChooseType
// ask (task ct1): TypeChoices serves the configured typeChoices list and Ask
// records every posed decision and reports suspended, so the test can drive
// the engine's two passes (ask, then answer through Ctx.ChosenType) the way
// rules' resumeResolution does.
type chooseTypeHost struct {
	fakeHost
	asks      []*decision.Decision
	suspended bool
}

func (h *chooseTypeHost) TypeChoices(_ state.PlayerID, _ string) []decision.Option {
	return h.typeChoices
}

func (h *chooseTypeHost) Ask(d *decision.Decision) bool {
	cp := *d
	cp.Options = append([]decision.Option(nil), d.Options...)
	h.asks = append(h.asks, &cp)
	h.suspended = true
	return true
}

func (h *chooseTypeHost) Suspended() bool { return h.suspended }

// chooseTypeAsks collects the Choose events a resolution emitted.
func chooseTypeAsks(h *fakeHost) []events.Event {
	var out []events.Event
	for _, e := range h.log {
		if e.Kind == events.Choose && e.Counter == "type" {
			out = append(out, e)
		}
	}
	return out
}

// TestChooseTypeSingleOptionTakesTheFallbackWithoutAsking pins the
// strict-supersets gate: with zero or one offerable type the choice is
// forced (or empty), so no decision is posed and the deterministic fallback
// records the choice exactly as before (R-9 byte-identical contract).
func TestChooseTypeSingleOptionTakesTheFallbackWithoutAsking(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	h.typeChoices = []decision.Option{{Index: 0, Kind: "type", Label: "Elf"}}
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Land\nOracle:x\n"), 0).ID
	h.g.AddObject(mkCard(t, "Name:Grunt\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"), 0)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "SP$ ChooseType | Defined$ You | Type$ Creature"))
	if len(h.asks) != 0 {
		t.Fatalf("a one-option choice posed a decision: %+v", h.asks)
	}
	if h.g.Obj(src).ChosenType != "Goblin" {
		t.Fatalf("fallback not recorded: ChosenType = %q", h.g.Obj(src).ChosenType)
	}
}

// TestChooseTypeChooserFollowsDefined pins the chooser: the first Defined$
// player asks, not the resolving controller, when the SA names one.
func TestChooseTypeChooserFollowsDefined(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	h.typeChoices = []decision.Option{
		{Index: 0, Kind: "type", Label: "Elf"},
		{Index: 1, Kind: "type", Label: "Zombie"},
	}
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Land\nOracle:x\n"), 0).ID
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "SP$ ChooseType | Defined$ Opponent | Type$ Creature"))
	if len(h.asks) != 1 || h.asks[0].Player != 1 {
		t.Fatalf("ask = %+v, want one decision owned by seat 1", h.asks)
	}
}

// TestChooseTypeUnknownCategoryStaysNotePlusFallback pins the boundary: a
// Type$ category this build still cannot name (not one of the enumerated
// categories) keeps the loud Note and a deterministic fallback, and never
// poses a list that cannot answer the question.
func TestChooseTypeUnknownCategoryStaysNotePlusFallback(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	h.typeChoices = nil
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Land\nOracle:x\n"), 0).ID
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "SP$ ChooseType | Defined$ You | Type$ Foo Bar"))
	if len(h.asks) != 0 {
		t.Fatalf("an unenumerable category posed a decision: %+v", h.asks)
	}
	notes := 0
	for _, e := range h.log {
		if e.Kind == events.Note {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("loud Note count = %d, want exactly one", notes)
	}
	if h.g.Obj(src).ChosenType != "Human" {
		t.Fatalf("fallback = %q, want Human (no owned creature types)", h.g.Obj(src).ChosenType)
	}
}
