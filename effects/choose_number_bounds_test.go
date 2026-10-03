package effects

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The bounded ChooseNumber ask (task bounded1, absorbing
// agent-20260922T091245Z-d777f1b6): a card's own Max$ bound, Min$ floor and
// ListTitle$ prompt now shape the mid-resolution number ask choose.go poses.
// These pins drive the primitive directly through the same scripted-ask
// double choose_number_ask_test.go uses; the real-corpus end-to-end pins
// (Pia Nalaar, Chief Mechanic; Localized Destruction / Aether Refinery) live
// rules-side in rules/choose_number_bounds_test.go.

// seedEnergy grants p n {E} through the event the engine folds, so the bound
// read is event-backed exactly as in play.
func seedEnergy(t *testing.T, h *fakeHost, p state.PlayerID, n int32) {
	t.Helper()
	h.Emit(events.Event{Kind: events.PlayerCounterChange, Player: p, Counter: "ENERGY", Amount: n})
}

// TestChooseNumberMaxBindsTheOfferedList pins the literal bound: Max$ 5 offers
// exactly 0..5 (six options, values on Amount), and Max$ 20 offers 0..20 — a
// literal ABOVE the historical fixed cap of 12, so the old 0..12 list cannot
// pass the test and the bound is not silently clamped back to it.
func TestChooseNumberMaxBindsTheOfferedList(t *testing.T) {
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(2))
	src := chooseNumberSrc(t, &h.fakeHost)
	for _, tc := range []struct{ max int32 }{{5}, {20}} {
		h.asks, h.suspended = nil, false
		Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseNumber | Max$ "+strconv.Itoa(int(tc.max))))
		if len(h.asks) != 1 {
			t.Fatalf("Max$ %d: asked %d decisions, want one", tc.max, len(h.asks))
		}
		opts := h.asks[0].Options
		if len(opts) != int(tc.max)+1 {
			t.Fatalf("Max$ %d: %d options, want %d (0..%d): %+v", tc.max, len(opts), tc.max+1, tc.max, opts)
		}
		for i, o := range opts {
			if o.Kind != "number" || o.Amount != i || o.Label != strconv.Itoa(i) {
				t.Fatalf("Max$ %d: option %d = %+v, want the ascending number %d", tc.max, i, o, i)
			}
		}
		// The unbounded fixed-list prompt survives a Max$ with no ListTitle.
		if h.asks[0].Prompt != "Choose a number" {
			t.Fatalf("Max$ %d: prompt = %q, want the historical fallback", tc.max, h.asks[0].Prompt)
		}
	}
}

// TestChooseNumberMaxSVarIndirection pins the SVar-mediated bound (Rampaging
// Aetherhood's and Territorial Aetherkite's spelling): Max$ Max resolves the
// named SVar's Count$YourCountersEnergy body against the ASKING controller's
// actual energy, so seat 0's 3 {E} bounds ITS list at 0..3 while seat 1's 5
// {E} bounds ITS ask at 0..5 — the asking controller's total, never a fixed
// seat's. With no {E} at all the list is the single legal value 0, so no ask
// is posed and the deterministic first-legal-value emit records it.
func TestChooseNumberMaxSVarIndirection(t *testing.T) {
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(2))
	src := chooseNumberSrc(t, &h.fakeHost)
	saBody := sa(t, "DB$ ChooseNumber | Max$ Max")
	seedEnergy(t, &h.fakeHost, 0, 3)
	seedEnergy(t, &h.fakeHost, 1, 5)
	Resolve(h, &Ctx{Source: src, Controller: 0, SVars: map[string]string{"Max": "Count$YourCountersEnergy"}}, saBody)
	if len(h.asks) != 1 {
		t.Fatalf("asked %d decisions, want one", len(h.asks))
	}
	if got := len(h.asks[0].Options); got != 4 {
		t.Fatalf("seat 0's bound options = %d, want exactly 0..3 (its own 3 {E}, not seat 1's 5): %+v", got, h.asks[0].Options)
	}
	for i, o := range h.asks[0].Options {
		if o.Amount != i {
			t.Fatalf("option %d = %+v, want Amount %d", i, o, i)
		}
	}
	// The other controller's ask reads THEIR total: 0..5, provably not
	// seat 0's 0..3.
	h.asks, h.suspended = nil, false
	Resolve(h, &Ctx{Source: src, Controller: 1, SVars: map[string]string{"Max": "Count$YourCountersEnergy"}}, saBody)
	if len(h.asks) != 1 {
		t.Fatalf("seat 1's ask posed %d decisions, want one", len(h.asks))
	}
	opts := h.asks[0].Options
	if len(opts) != 6 || opts[0].Amount != 0 || opts[len(opts)-1].Amount != 5 {
		t.Fatalf("seat 1's bound options = %+v, want 0..5 (its own 5 {E})", opts)
	}
	// A controller with no {E} at all: the bound resolves to a real 0, the
	// choice is forced, and the fallback records it.
	h2 := &chooseNumberHost{}
	h2.g = state.NewGame(names(2))
	src2 := chooseNumberSrc(t, &h2.fakeHost)
	Resolve(h2, &Ctx{Source: src2, Controller: 0, SVars: map[string]string{"Max": "Count$YourCountersEnergy"}},
		sa(t, "DB$ ChooseNumber | Max$ Count$YourCountersEnergy"))
	if len(h2.asks) != 0 {
		t.Fatalf("a bound-0 ask posed %d decisions, want none (the choice is forced)", len(h2.asks))
	}
	evs := chooseNumberEvents(&h2.fakeHost)
	if len(evs) != 1 || evs[0].Amount != 0 {
		t.Fatalf("bound-0 Choose events = %+v, want exactly one Amount 0", evs)
	}
}

// TestChooseNumberMaxInlineCountExpression pins the direct form (Pia Nalaar,
// Chief Mechanic's spelling): Max$ Count$YourCountersEnergy is an inline
// expression, not an SVar name, and resolves against the current context the
// same way — 2 {E} bounds the list at 0..2.
func TestChooseNumberMaxInlineCountExpression(t *testing.T) {
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(2))
	src := chooseNumberSrc(t, &h.fakeHost)
	seedEnergy(t, &h.fakeHost, 0, 2)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseNumber | Max$ Count$YourCountersEnergy"))
	if len(h.asks) != 1 {
		t.Fatalf("asked %d decisions, want one", len(h.asks))
	}
	if got := len(h.asks[0].Options); got != 3 {
		t.Fatalf("bound options = %d, want exactly 0..2: %+v", got, h.asks[0].Options)
	}
	for i, o := range h.asks[0].Options {
		if o.Amount != i {
			t.Fatalf("option %d = %+v, want Amount %d", i, o, i)
		}
	}
}

// TestChooseNumberListTitlePrompt pins ListTitle$: the card's own title is the
// prompt, verbatim, and it applies with or without a Max$ bound (the
// ChooseAnyNumber$ "pay any amount" carriers carry ListTitle$ alone).
func TestChooseNumberListTitlePrompt(t *testing.T) {
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(2))
	src := chooseNumberSrc(t, &h.fakeHost)
	seedEnergy(t, &h.fakeHost, 0, 2)
	Resolve(h, &Ctx{Source: src, Controller: 0},
		sa(t, "DB$ ChooseNumber | Max$ Count$YourCountersEnergy | ListTitle$ amount of energy to pay"))
	if len(h.asks) != 1 {
		t.Fatalf("asked %d decisions, want one", len(h.asks))
	}
	if got := h.asks[0].Prompt; got != "amount of energy to pay" {
		t.Fatalf("prompt = %q, want the card's ListTitle verbatim", got)
	}
	// Without a Max$, ListTitle$ alone still names the prompt over the fixed
	// list (the historical 0..12 offer is untouched).
	h.asks, h.suspended = nil, false
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseNumber | ListTitle$ How many times do you want repeat this process?"))
	if len(h.asks) != 1 {
		t.Fatalf("asked %d decisions, want one", len(h.asks))
	}
	if got := h.asks[0].Prompt; got != "How many times do you want repeat this process?" {
		t.Fatalf("prompt = %q, want the card's ListTitle verbatim", got)
	}
	if got := len(h.asks[0].Options); got != 13 {
		t.Fatalf("unbounded options = %d, want the historical fixed 0..12", got)
	}
}

// TestChooseNumberMinFloor pins Min$: the card's own lower bound floors the
// offered list (Min$ 2 Max$ 6 offers exactly 2..6), and Min$ 0 with Max$ 5
// keeps zero offerable — the zero/optional semantics the ticket must preserve
// (a "you may pay" carrier's answered 0 is a legal decline, never a forced
// positive).
func TestChooseNumberMinFloor(t *testing.T) {
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(2))
	src := chooseNumberSrc(t, &h.fakeHost)
	for _, tc := range []struct {
		line  string
		first int
		last  int
	}{
		{"DB$ ChooseNumber | Min$ 2 | Max$ 6", 2, 6},
		{"DB$ ChooseNumber | Min$ 0 | Max$ 5", 0, 5},
	} {
		h.asks, h.suspended = nil, false
		Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, tc.line))
		if len(h.asks) != 1 {
			t.Fatalf("%s: asked %d decisions, want one", tc.line, len(h.asks))
		}
		opts := h.asks[0].Options
		if len(opts) == 0 || opts[0].Amount != tc.first || opts[len(opts)-1].Amount != tc.last {
			t.Fatalf("%s: options = %+v, want %d..%d", tc.line, opts, tc.first, tc.last)
		}
	}
}

// TestChooseNumberMinOnlyFloorsTheDefaultRange pins a Min$ without Max$:
// the default upper ceiling remains 12, but values below the card's floor are
// not offered.
func TestChooseNumberMinOnlyFloorsTheDefaultRange(t *testing.T) {
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(2))
	src := chooseNumberSrc(t, &h.fakeHost)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseNumber | Min$ 2"))
	if len(h.asks) != 1 {
		t.Fatalf("Min$ 2 asked %d decisions, want one", len(h.asks))
	}
	opts := h.asks[0].Options
	if len(opts) != chooseNumberMaxOffer-1 || opts[0].Amount != 2 || opts[len(opts)-1].Amount != chooseNumberMaxOffer {
		t.Fatalf("Min$ 2 options = %+v, want exactly 2..%d", opts, chooseNumberMaxOffer)
	}
	for i, o := range opts {
		if o.Amount != i+2 {
			t.Fatalf("option %d = %+v, want amount %d", i, o, i+2)
		}
	}
}

// TestChooseNumberUnresolvableInlineCountBoundFailsClosed pins the strict
// verdict for a direct Count$ expression: NumResolved's ordinary amount
// fallback is zero, but a bound cannot silently treat an unknown head as a
// genuine zero.
func TestChooseNumberUnresolvableInlineCountBoundFailsClosed(t *testing.T) {
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(2))
	src := chooseNumberSrc(t, &h.fakeHost)
	before := len(h.log)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseNumber | Max$ Count$DefinitelyUnmodelledHead"))
	if len(h.asks) != 0 {
		t.Fatalf("unknown inline bound posed %d asks, want none", len(h.asks))
	}
	var noted, chose bool
	for _, e := range h.log[before:] {
		if e.Kind == events.Note && strings.Contains(e.Text, "Count$DefinitelyUnmodelledHead") {
			noted = true
		}
		if e.Kind == events.Choose && e.Counter == "number" && e.Amount == 0 {
			chose = true
		}
	}
	if !noted || !chose {
		t.Fatalf("unknown inline bound events = %+v, want loud Note and deterministic Choose 0", h.log[before:])
	}
}

// TestChooseNumberUnresolvableBoundFailsClosed pins the fail-closed direction:
// a bound this context cannot resolve (an SVar name with no table entry) emits
// the loud Note naming the parameter, poses no ask, and records the
// deterministic 0 — never a list that could violate the unknown bound.
func TestChooseNumberUnresolvableBoundFailsClosed(t *testing.T) {
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(2))
	src := chooseNumberSrc(t, &h.fakeHost)
	before := len(h.log)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseNumber | Max$ UnboundName"))
	if len(h.asks) != 0 {
		t.Fatalf("an unresolvable bound posed %d asks, want none", len(h.asks))
	}
	var noted, chose bool
	for _, e := range h.log[before:] {
		switch {
		case e.Kind == events.Note && strings.Contains(e.Text, "Max$ UnboundName"):
			noted = true
		case e.Kind == events.Choose && e.Counter == "number" && e.Amount == 0:
			chose = true
		}
	}
	if !noted || !chose {
		t.Fatalf("fail-closed events = %+v, want a Note naming Max$ UnboundName and the Choose 0 fallback", h.log[before:])
	}
}

// TestChooseNumberBoundNoHostDegradesQuietly pins R-9 for the bounded ask: on
// a host that cannot ask, a legal bound still degrades to the deterministic
// first-legal-value (0) with NO extra Note — only the unresolvable-bound path
// is loud.
func TestChooseNumberBoundNoHostDegradesQuietly(t *testing.T) {
	h := newHost(t, 2)
	src := chooseNumberSrc(t, h)
	seedEnergy(t, h, 0, 4)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseNumber | Max$ Count$YourCountersEnergy"))
	if h.g.Obj(src).ChosenNumber != 0 {
		t.Fatalf("no-host fallback = %d, want the deterministic first legal value 0", h.g.Obj(src).ChosenNumber)
	}
	evs := chooseNumberEvents(h)
	if len(evs) != 1 || evs[0].Amount != 0 {
		t.Fatalf("no-host Choose events = %+v, want exactly one Amount 0", evs)
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Fatalf("unexpected Note on the no-host degradation: %+v", e)
		}
	}
}
