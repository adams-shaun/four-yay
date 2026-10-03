package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// fx42AskHost is a fakeHost whose Ask records every posed decision in order
// and marks the host Suspended — the effects-package stand-in for a
// rules.Engine that stops descending into a chained SubAbility after an ask.
// Unlike askHost (single last-ask slot, never suspended) and suspendHost
// (single slot, suspended), it records an ordered list so a test can assert
// that a NESTED ask fires after an outer one, and can clear Suspended to
// simulate the engine's resume.
type fx42AskHost struct {
	fakeHost
	asks      []*decision.Decision
	suspended bool
}

func (h *fx42AskHost) Ask(d *decision.Decision) bool {
	cp := *d
	cp.Options = append([]decision.Option(nil), d.Options...)
	h.asks = append(h.asks, &cp)
	h.suspended = true
	return true
}

func (h *fx42AskHost) Suspended() bool { return h.suspended }

// realReentrantDiscardSA returns the REAL compiled outer Discard sub-ability
// of a named corpus card whose Sub chain reaches a SECOND Discard asker.
// It asserts the Sub really carries a second asking Discard, so a caller is
// never handed a shape that only looks like the reentrant case. Gruesome
// Discovery and Last Rites are the two corpus cards with this shape (measured
// with a walker over .cards/ir.gob.gz at the branch under test).
func realReentrantDiscardSA(t *testing.T, cardName string) (*cards.SA, map[string]string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(cardName)
	if !ok {
		t.Fatalf("corpus has no card %q", cardName)
	}
	for _, f := range c.Faces {
		for _, a := range f.Abilities {
			if a.API != "Discard" || a.Params["Mode"] != "TgtChoose" {
				continue
			}
			reaches := false
			for s := a.Sub; s != nil; s = s.Sub {
				if s.API == "Discard" && (s.Params["Mode"] == "RevealYouChoose" || s.Params["Mode"] == "TgtChoose") {
					reaches = true
				}
			}
			if reaches {
				return a, f.SVars
			}
		}
	}
	t.Fatalf("card %q has no TgtChoose Discard ability whose Sub reaches another Discard asker", cardName)
	return nil, nil
}

// TestNestedCounterDoesNotInheritOuterUnlessPayAnswer pins the fx42 defect
// synthetically. The corpus cannot reach it — a walker over the compiled
// corpus found ZERO unless_pay consumers (a Counter or CopySpellAbility with
// an UnlessCost$) whose same-walk graph reaches a second unless_pay consumer
// — so no real card exposes the leak. The fixture is a Counter with an
// UnlessCost$ whose SubAbility$ is a second Counter with an UnlessCost$, the
// shape a future card could introduce. On the outer counter's "pay" resume
// the answer rides in Ctx.UnlessPay; because effCounter and
// effCopySpellAbility never cleared it, the nested counter below inherits the
// outer's "pay" and skips its own counter instead of posing its own
// pay/decline decision.
func TestNestedCounterDoesNotInheritOuterUnlessPayAnswer(t *testing.T) {
	h := &fx42AskHost{}
	h.g = state.NewGame(names(2))
	src := counterSource(t, &h.fakeHost, 0)
	target := spellOnStack(t, &h.fakeHost, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 1)

	inner := &cards.SA{Kind: "DB", API: "Counter",
		Params: map[string]string{"UnlessCost": "1", "ValidTgts": "Spell"}}
	outer := &cards.SA{Kind: "SP", API: "Counter",
		Params: map[string]string{"UnlessCost": "3", "ValidTgts": "Spell"}, Sub: inner}
	ctx := &Ctx{Source: src, Controller: 0, Targets: []state.Target{{Obj: target.ID}}}

	// Pass 1: the outer counter poses the pay/decline ask to the controller of
	// the countered spell (seat 1), and suspends before the chained inner
	// counter runs.
	Resolve(h, ctx, outer)
	if len(h.asks) != 1 {
		t.Fatalf("outer counter posed %d decisions, want exactly 1 (the chained inner counter must not run before the pay choice)", len(h.asks))
	}
	if h.asks[0].Player != 1 {
		t.Fatalf("outer counter payer = seat %d, want the CONTROLLER OF THE COUNTERED SPELL (seat 1)", h.asks[0].Player)
	}

	// Engine resume: the payer paid (Ctx.UnlessPay == "pay"), the outer
	// counter skips its counter, and the walk descends to the chained inner.
	h.suspended = false
	ctx.UnlessPay = "pay"
	Resolve(h, ctx, outer)

	// The inner counter must now pose ITS OWN unless-pay decision. With the
	// defect it inherits the outer's "pay" and silently skips its own counter
	// — h.asks stays at one.
	if len(h.asks) != 2 {
		t.Fatalf("after the outer pay the chained counter posed %d decisions, want 2 (the inner counter must pose its own unless-pay ask, not inherit the outer's 'pay')", len(h.asks))
	}
	if h.asks[1].Player != 1 {
		t.Fatalf("inner counter payer = seat %d, want the controller of the countered spell (seat 1)", h.asks[1].Player)
	}
}
