package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpusCounterSA returns the REAL compiled Counter sub-ability of a named
// corpus card (Mana Leak, Counterspell, Runeboggle). The brief insists on
// real compiled SAs rather than a synthetic map[string]string fixture --
// the exact opposite of what shipped two bugs this week -- so the payer
// resolution and the ask/re-entry contract are asserted against the real
// card parameters (UnlessCost$ 3, no UnlessPayer$, a SubAbility$ chain),
// not a hand-built bag that could quietly differ.
func corpusCounterSA(t *testing.T, name string) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %q", name)
	}
	for _, f := range c.Faces {
		for _, a := range f.Abilities {
			if a.API == "Counter" {
				return a
			}
		}
	}
	t.Fatalf("corpus card %q has no Counter ability", name)
	return nil
}

// counterSource makes a throwaway object to stand in for the resolving
// counterspell -- only its ID feeds the ask's Source/option Obj, so it need
// not correspond to the real corpus card whose SA we resolve.
func counterSource(t *testing.T, h *fakeHost, ctlr state.PlayerID) state.ObjID {
	t.Helper()
	return h.g.AddObject(mkCard(t, "Name:Counterer\nManaCost:U\nTypes:Instant\nOracle:x\n"), ctlr).ID
}

// counterMoves counts how many times id left the stack for `to` via a
// MoveZone -- the "was the spell countered" observable.
func counterMoves(h *fakeHost, id state.ObjID, to state.Zone) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZStack && ev.To == to {
			n++
		}
	}
	return n
}

func hasNoteContaining(h *fakeHost, sub string) bool {
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, sub) {
			return true
		}
	}
	return false
}

// TestCounterWithoutUnlessCostCountersUnconditionally: Counterspell carries
// no UnlessCost$, so it must counter with NO ask posed (the guards around
// the unless-pay branch must not fire), and the spell hits the graveyard
// directly. This is the no-regression guard for the no-ask corpus shape; on
// both the unmodified and modified tree it counters unconditionally (that is
// why it is a guard, not a discriminative test -- the discriminator is
// TestCounterUnlessCostAsks... ).
func TestCounterWithoutUnlessCostCountersUnconditionally(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	src := counterSource(t, &h.fakeHost, 0)
	target := spellOnStack(t, &h.fakeHost, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 1)
	Resolve(h, &Ctx{Source: src, Controller: 0, Targets: []state.Target{{Obj: target.ID}}},
		corpusCounterSA(t, "Counterspell"))
	if h.asked != nil {
		t.Fatalf("Counterspell posed a pay decision despite having no UnlessCost$: %+v", h.asked)
	}
	if target.Zone != state.ZGraveyard {
		t.Fatalf("Counterspell target zone = %s, want Graveyard", target.Zone)
	}
}

// TestCounterNoAskHostDeclinesDeterministically: a host that cannot ask
// (fakeHost.Ask returns false -- the effects-package test double, or a
// rules context with no engine to drive) falls back to the deterministic
// decline: the spell is countered, and a Note records that the pay was never
// actually posed (R-9). The stand-in sits behind `if h.Ask(d) { return }`.
// On the unmodified tree effCounter never reached the ask branch, so no
// Note was emitted -- that is what makes this fail there.
func TestCounterNoAskHostDeclinesDeterministically(t *testing.T) {
	h := newHost(t, 2)
	src := counterSource(t, h, 0)
	target := spellOnStack(t, h, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 1)
	Resolve(h, &Ctx{Source: src, Controller: 0, Targets: []state.Target{{Obj: target.ID}}},
		corpusCounterSA(t, "Mana Leak"))
	if target.Zone != state.ZGraveyard {
		t.Fatalf("no-ask host must still counter (deterministic decline): zone %s", target.Zone)
	}
	if !hasNoteContaining(h, "declined") {
		t.Fatal("no Note recorded the no-ask decline stand-in")
	}
}

// corpusSwitchedCounterSA returns the REAL compiled Counter SA of a named
// corpus card that carries UnlessSwitched$ True. All five of the corpus's
// switched Counter shapes hang off a T: line's Execute$ SVar rather than a
// face ability, so unlike corpusCounterSA this walks Abilities, every
// Trigger.Effect, every Repl.With and each of their Sub chains. It also
// asserts that the SA it found really carries both UnlessCost$ and
// UnlessSwitched$ True: a synthetic map[string]string fixture would prove
// nothing about the corpus, and that shortcut has shipped a regression here
// before.
func corpusSwitchedCounterSA(t *testing.T, name string) *cards.SA {
	t.Helper()
	return corpusUnlessCounterSA(t, name, func(sa *cards.SA) bool {
		return strings.EqualFold(sa.Params["UnlessSwitched"], "True")
	})
}

// corpusUnlessCounterSA finds the first compiled Counter SA of a corpus card
// that carries a non-empty UnlessCost$ and satisfies extra, searching
// Abilities, every Trigger.Effect, every Repl.With and each of their Sub
// chains. It asserts the UnlessCost$ is really there, so a caller can never
// be handed something that only looks like the shape under test.
func corpusUnlessCounterSA(t *testing.T, name string, extra func(*cards.SA) bool) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %q", name)
	}
	var found *cards.SA
	walk := func(sa *cards.SA) {
		for ; sa != nil && found == nil; sa = sa.Sub {
			if sa.API != "Counter" || strings.TrimSpace(sa.Params["UnlessCost"]) == "" {
				continue
			}
			if extra == nil || extra(sa) {
				found = sa
				return
			}
		}
	}
	for _, f := range c.Faces {
		for _, a := range f.Abilities {
			walk(a)
		}
		for _, tr := range f.Triggers {
			walk(tr.Effect)
		}
		for _, r := range f.Repls {
			walk(r.With)
		}
	}
	if found == nil {
		t.Fatalf("corpus card %q has no compiled Counter SA with UnlessCost$ matching the predicate", name)
	}
	return found
}

// TestCounterUnlessSwitchedSuppressesTheAsk pins the suppression on all five
// REAL compiled corpus Counter SAs that carry UnlessSwitched$ True.
//
// UnlessSwitched$ True inverts the deal: paying CAUSES the counter. The
// engine does not implement that, and posing the ordinary ask on these cards
// is worse than posing nothing -- it is backwards, letting a player prevent
// a counter by paying for it. So effCounter must not pose the ask at all on
// a switched shape, and must counter unconditionally, which is exactly what
// main did before the UnlessCost$ ask existed.
//
// This FAILS without the `&& !switched` guard: every one of these SAs has a
// non-empty UnlessCost$, so the unguarded branch poses the decision and
// suspends instead of countering. Verified by running it on a tree without
// the guard, not asserted.
// TestCounterUnlessSwitchedAppliesOrientation pins the REAL switched
// semantics the shared unlessProceed gate gives every Counter carrying
// UnlessSwitched$ True: the ask IS posed (the pre-gate build suppressed it
// and countered unconditionally — an approximation this task closes), a
// recorded "pay" CAUSES the counter, and a "decline" lets the spell resolve.
// The five corpus carriers are Brain Gorgers, Dash Hopes, Ice Cave,
// Phantasmagorian and Temporal Extortion (5 compiled SAs; none in a repo
// deck). Whether the cost is actually payable is rules' decision (rules'
// unless-pay arm); this pin is the orientation the gate applies to the
// recorded answer.
func TestCounterUnlessSwitchedAppliesOrientation(t *testing.T) {
	for _, name := range []string{
		"Brain Gorgers", "Dash Hopes", "Ice Cave", "Phantasmagorian", "Temporal Extortion",
	} {
		t.Run(name, func(t *testing.T) {
			sa := corpusSwitchedCounterSA(t, name)
			// First pass: the switched ask is POSED, to the first
			// UnlessPayer$ player — every one of the five says "Player", so
			// the first payer is seat 0 in AliveFrom order.
			h := &askHost{}
			h.g = state.NewGame(names(2))
			src := counterSource(t, &h.fakeHost, 0)
			target := spellOnStack(t, &h.fakeHost, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 1)
			ctx := &Ctx{Source: src, Controller: 0,
				Remembered: []state.Target{{Obj: target.ID}}}
			Resolve(h, ctx, sa)
			if h.asked == nil {
				t.Fatalf("%s (UnlessCost$ %q, UnlessSwitched$ True) posed no pay ask — the switched shape must ask, not suppress",
					name, sa.Params["UnlessCost"])
			}
			if h.asked.Player != 0 {
				t.Fatalf("%s payer = seat %d, want seat 0 (UnlessPayer$ Player)", name, h.asked.Player)
			}
			if target.Zone != state.ZStack {
				t.Fatalf("%s: countered spell zone = %s during the suspended ask, want Stack (the body must not run before the answer)",
					name, target.Zone)
			}
			// Resume "decline": on the switched shape the decline stops the
			// effect. With UnlessPayer$ Player the gate moves on to the next
			// payer (seat 1); a host that cannot suspend there keeps the
			// decline, and the spell survives.
			h2 := &askHost{}
			h2.g = h.g
			ctx2 := &Ctx{Source: src, Controller: 0, UnlessPay: "decline", UnlessNext: 0,
				Remembered: []state.Target{{Obj: target.ID}}}
			Resolve(h2, ctx2, sa)
			if got := target.Zone; got != state.ZStack {
				t.Fatalf("%s: after an all-payer decline the countered spell zone = %s, want Stack (declined switched counter never fires)", name, got)
			}
			// Resume "pay": on the switched shape the pay CAUSES the counter.
			h3 := &askHost{}
			h3.g = h.g
			ctx3 := &Ctx{Source: src, Controller: 0, UnlessPay: "pay", UnlessNext: 0,
				Remembered: []state.Target{{Obj: target.ID}}}
			Resolve(h3, ctx3, sa)
			if target.Zone != state.ZGraveyard {
				t.Fatalf("%s: after the pay the countered spell zone = %s, want Graveyard (paid switched counter fires)", name, target.Zone)
			}
			if got := counterMoves(&h3.fakeHost, target.ID, state.ZGraveyard); got != 1 {
				t.Fatalf("%s: move-to-graveyard count = %d, want exactly 1", name, got)
			}
		})
	}
}

// TestCounterUnlessCostPromptNeverLeaksScriptSyntax pins the rendering of the
// ask on REAL compiled corpus SAs whose UnlessCost$ is not a mana cost.
// decision.Decision crosses to every seat, human ones included, so the prompt
// and the option labels must not carry raw Forge script -- neither a bare
// SVar name (Mausoleum Wanderer's UnlessCost$ X, whose value the engine never
// reads) nor a bracket form (Reality Smasher's Discard<1/Card>). Both cards
// ship in repo decks (mono-blue-tempo / uw-tempo and eldrazi-stompy).
//
// This FAILS without unlessCostLabel: the unmodified tree interpolates the
// raw UnlessCost$ into both strings. Verified by running it on a tree without
// the helper, not asserted. Plain mana costs are unaffected -- Mana Leak's
// "Pay 3" is pinned by TestCounterUnlessCostAsksTheCounteredSpellsController
// above, and that is what keeps the acceptance chain heads still.
func TestCounterUnlessCostPromptNeverLeaksScriptSyntax(t *testing.T) {
	for _, tc := range []struct{ card, cost string }{
		{"Mausoleum Wanderer", "X"},
		{"Reality Smasher", "Discard<1/Card>"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			sa := corpusUnlessCounterSA(t, tc.card, nil)
			if got := strings.TrimSpace(sa.Params["UnlessCost"]); got != tc.cost {
				t.Fatalf("%s UnlessCost$ = %q, want %q -- the corpus changed under this test", tc.card, got, tc.cost)
			}
			h := &askHost{}
			h.g = state.NewGame(names(2))
			src := counterSource(t, &h.fakeHost, 0)
			target := spellOnStack(t, &h.fakeHost, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 1)
			Resolve(h, &Ctx{Source: src, Controller: 0,
				Targets:    []state.Target{{Obj: target.ID}},
				Remembered: []state.Target{{Obj: target.ID}}}, sa)

			if h.asked == nil {
				t.Fatalf("%s posed no pay decision for UnlessCost$ %s", tc.card, tc.cost)
			}
			shown := []string{h.asked.Prompt}
			for _, o := range h.asked.Options {
				shown = append(shown, o.Label)
			}
			for _, s := range shown {
				if strings.Contains(s, tc.cost) {
					t.Fatalf("%s: %q leaks the raw script cost %q to the seat", tc.card, s, tc.cost)
				}
			}
			if h.asked.Prompt != "Pay the cost to save the spell, or decline" {
				t.Fatalf("%s prompt = %q", tc.card, h.asked.Prompt)
			}
			if h.asked.Options[0].Label != "Pay the cost — don't counter" {
				t.Fatalf("%s pay label = %q", tc.card, h.asked.Options[0].Label)
			}
		})
	}
}
