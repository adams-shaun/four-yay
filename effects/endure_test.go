package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const endureSpiritFixture = "Name:Spirit Token\nTypes:Creature Spirit\nPT:0/0\nColor:W\nOracle:\n"

// endureTapeHost is fixtureHost plus the resolution kernel's ask seam
// (effects.askSeam: AskCount + TapeAnswer), so an effects-level Endure test
// can drive the real answered branch instead of only the no-tape stand-in.
// The seam is added on a NEW type, never by appending to context_test.go's
// shared fakeHost.
type endureTapeHost struct {
	*fakeHost
	// answer is the option index TapeAnswer serves for every ask; -1 makes
	// the seam report "unserved", exercising the R-9 stand-in path.
	answer int
	asks   int
}

func (h *endureTapeHost) AskCount() uint64 { return uint64(h.asks) }

func (h *endureTapeHost) TapeAnswer(d *decision.Decision) (decision.Intent, bool) {
	h.asks++
	if h.answer < 0 || h.answer >= len(d.Options) {
		return decision.Intent{}, false
	}
	return decision.Intent{Choices: []int{h.answer}}, true
}

// endureHost builds a 2-seat game with object 1 (seat 0) on the battlefield
// and the white Spirit token script registered, and a Ctx sourced at it.
func endureHost(t *testing.T, answer int) (*endureTapeHost, *Ctx) {
	t.Helper()
	fh, c := fixtureHost(t)
	fh.g.Tokens = map[string]*cards.Card{endureSpiritToken: mkCard(t, endureSpiritFixture)}
	h := &endureTapeHost{fakeHost: fh, answer: answer}
	h.Emit(events.Event{Kind: events.MoveZone, Obj: c.Source, From: state.ZLibrary, To: state.ZBattlefield})
	if o := h.g.Obj(c.Source); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Endure source %d is not on the battlefield: %+v", c.Source, o)
	}
	return h, c
}

// TestEndureRegistersAndPosesTheChoice is the "cannot be silently absent"
// gate: the primitive is registered, and on a live permanent it poses exactly
// the two-branch election rather than falling through to the unimplemented
// note.
func TestEndureRegistersAndPosesTheChoice(t *testing.T) {
	if !Supported()["api:Endure"] {
		t.Fatal("api:Endure is not registered")
	}
	h, c := endureHost(t, -1)
	Resolve(h, c, sa(t, "DB$ Endure | Num$ 2"))
	if h.asks != 1 {
		t.Fatalf("Endure asked %d decisions, want 1", h.asks)
	}
	if h.lastAsk == nil || h.lastAsk.Kind != decision.KChoose || len(h.lastAsk.Options) != 2 {
		t.Fatalf("Endure ask = %+v, want a 2-option KChoose", h.lastAsk)
	}
	if h.lastAsk.Options[0].Kind != "endure_counters" || h.lastAsk.Options[1].Kind != "endure_spirit" {
		t.Fatalf("Endure options = %+v, want the counters/spirit pair", h.lastAsk.Options)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && ev.Text == "unimplemented API Endure" {
			t.Fatalf("Endure fell through to the unimplemented note: %+v", ev)
		}
	}
}

// TestEndureCounterBranch adds the chosen amount when the seats answers for
// the counter branch.
func TestEndureCounterBranch(t *testing.T) {
	h, c := endureHost(t, 0)
	if got := h.g.Obj(c.Source).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: source starts with %d +1/+1 counters, want 0", got)
	}
	Resolve(h, c, sa(t, "DB$ Endure | Num$ 2"))
	if got := h.g.Obj(c.Source).Counter("P1P1"); got != 2 {
		t.Fatalf("counter branch placed %d counters, want 2", got)
	}
	if len(h.g.Zone(state.ZBattlefield, 0)) != 1 {
		t.Fatalf("counter branch created a token; battlefield = %v", h.g.Zone(state.ZBattlefield, 0))
	}
}

// TestEndureSpiritBranch creates a dynamic N/N white Spirit when the seat
// answers for the token branch.
func TestEndureSpiritBranch(t *testing.T) {
	h, c := endureHost(t, 1)
	Resolve(h, c, sa(t, "DB$ Endure | Num$ 3"))
	if got := h.g.Obj(c.Source).Counter("P1P1"); got != 0 {
		t.Fatalf("spirit branch placed %d counters on the source, want 0", got)
	}
	var spirits []*state.Object
	for _, id := range h.g.Zone(state.ZBattlefield, 0) {
		o := h.g.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Spirit Token" {
			spirits = append(spirits, o)
		}
	}
	if len(spirits) != 1 {
		t.Fatalf("spirit branch created %d Spirit tokens, want 1", len(spirits))
	}
	if len(h.continuous) != 1 {
		t.Fatalf("spirit branch registered %d continuous effects, want 1", len(h.continuous))
	}
	ce := h.continuous[0]
	if !ce.HasSet || ce.SetPower != 3 || ce.SetToughness != 3 {
		t.Fatalf("Spirit P/T rider = %+v, want Set 3/3", ce)
	}
	if ce.Source != spirits[0].ID || ce.Layer != state.LPT || ce.Sub != state.SubSet {
		t.Fatalf("Spirit rider is not the layer-7b self set on the mint: %+v", ce)
	}
}

// TestEndureZeroDoesNothing pins CR 701.63b: endure 0 puts nothing and
// creates nothing, with no ask.
func TestEndureZeroDoesNothing(t *testing.T) {
	h, c := endureHost(t, 1)
	Resolve(h, c, sa(t, "DB$ Endure | Num$ 0"))
	if h.asks != 0 {
		t.Fatalf("endure 0 posed %d asks, want 0", h.asks)
	}
	if got := h.g.Obj(c.Source).Counter("P1P1"); got != 0 {
		t.Fatalf("endure 0 placed %d counters, want 0", got)
	}
	if len(h.g.Zone(state.ZBattlefield, 0)) != 1 {
		t.Fatalf("endure 0 created a token; battlefield = %v", h.g.Zone(state.ZBattlefield, 0))
	}
}

// TestEndureDepartedPermanentCreatesTokenOnly pins CR 701.63a's departure
// case: a permanent that has left the battlefield can no longer take
// counters, so the token is created without posing a choice.
func TestEndureDepartedPermanentCreatesTokenOnly(t *testing.T) {
	h, c := endureHost(t, 0)
	// Move the source to the graveyard: counters are no longer possible.
	h.Emit(events.Event{Kind: events.MoveZone, Obj: c.Source, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := h.g.Obj(c.Source); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: source is not in the graveyard: %+v", o)
	}
	Resolve(h, c, sa(t, "DB$ Endure | Num$ 2"))
	if h.asks != 0 {
		t.Fatalf("departed Endure posed %d asks, want 0 (no counters possible)", h.asks)
	}
	found := false
	for _, id := range h.g.Zone(state.ZBattlefield, 0) {
		if o := h.g.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Spirit Token" {
			found = true
		}
	}
	if !found {
		t.Fatalf("departed Endure created no Spirit token; battlefield = %v", h.g.Zone(state.ZBattlefield, 0))
	}
}

// TestEndureNoTapeTakesCounterStandIn pins the R-9 no-tape fallback: a host
// that serves no answer still gets a deterministic counter branch (never a
// silent no-op and never a livelock).
func TestEndureNoTapeTakesCounterStandIn(t *testing.T) {
	h, c := endureHost(t, -1)
	Resolve(h, c, sa(t, "DB$ Endure | Num$ 4"))
	if got := h.g.Obj(c.Source).Counter("P1P1"); got != 4 {
		t.Fatalf("no-tape stand-in placed %d counters, want 4", got)
	}
}
