package effects

// Charm mode labels follow the printed SpellDescription$ (task
// agent-20260928T074145Z-c832b15a). The rule: a mode's label is the mode
// body's own SpellDescription$, else the first SpellDescription$ found
// walking the body's SubAbility$ chain, else the raw SVar name. The
// corpus's three chain-only carriers are What Must Be Done's Release Juno
// mode (description rides DBChangeZone, one hop down) and Varchild's
// War-Riders' two upkeep modes (descriptions ride SurvivorDistribution and
// Sacrifice); the rules-level tests pin those on the real cards. This file
// pins the rule itself at the primitive level, with the mid-resolution
// KModes ask effCharm poses.
//
// Labels are display only — no mode run changes. Body-first precedence
// keeps the 1926/1929 corpus modes that already labelled by their own
// SpellDescription$ byte-identical.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestCharmModeLabelFollowsThePrintedDescription drives a three-mode Charm
// through effCharm's KModes ask: mode 0 carries its own SpellDescription$,
// mode 1 carries none but its SubAbility$ chain does, mode 3 carries none
// anywhere (the legit SVar-name fallback, no corpus instance but the
// documented last resort). The options must read, in order, the body
// description, the SUB's description, and the raw SVar name.
func TestCharmModeLabelFollowsThePrintedDescription(t *testing.T) {
	h := &fakeHost{}
	h.g = state.NewGame(names(2))
	h.g.Players[0].Life = 20
	svars := map[string]string{
		"BodyMode": "DB$ GainLife | Defined$ You | LifeAmount$ 1 | SpellDescription$ Body description.",
		"SubMode":  "DB$ GainLife | Defined$ You | LifeAmount$ 2 | SubAbility$ SubTail",
		"SubTail":  "DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Tail description.",
		"BareMode": "DB$ GainLife | Defined$ You | LifeAmount$ 3",
	}
	src := sa(t, "SP$ Charm | Choices$ BodyMode,SubMode,BareMode")
	// Precondition: the middle mode's description really does sit one hop
	// down its SubAbility$ chain.
	subMode := cards.ResolveSVar(svars, "SubMode")
	if subMode == nil || subMode.Sub == nil {
		t.Fatal("precondition: SubMode did not link its SubAbility$ chain")
	}
	Resolve(h, &Ctx{Controller: 0, SVars: svars}, src)

	if h.askCount == 0 || h.lastAsk == nil || h.lastAsk.Kind != decision.KModes {
		t.Fatalf("precondition: the charm posed no KModes ask (lastAsk=%+v)", h.lastAsk)
	}
	if len(h.lastAsk.Options) != 3 {
		t.Fatalf("precondition: options = %+v, want the three Choices$ modes", h.lastAsk.Options)
	}
	want := []string{"Body description.", "Tail description.", "BareMode"}
	for i, o := range h.lastAsk.Options {
		if o.Label != want[i] {
			t.Fatalf("mode %d label = %q, want %q (options: %+v)", i, o.Label, want[i], h.lastAsk.Options)
		}
	}
}

// TestCharmModeLabelHelperCoversTheShapeRule pins CharmModeLabel directly:
// nil sub -> fallback; body description wins over a chain description; a
// two-hop chain finds the description on the second sub; a chain with no
// description anywhere falls back to the SVar name.
func TestCharmModeLabelHelperCoversTheShapeRule(t *testing.T) {
	both := sa(t, "DB$ GainLife | Defined$ You | LifeAmount$ 1 | SpellDescription$ Body wins.")
	tail := sa(t, "DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Chain loses.")
	both.Sub = tail
	if got := CharmModeLabel(both, "SVar"); got != "Body wins." {
		t.Fatalf("body-first precedence: label = %q, want the body description", got)
	}

	head := sa(t, "DB$ GainLife | Defined$ You | LifeAmount$ 1")
	mid := sa(t, "DB$ GainLife | Defined$ You | LifeAmount$ 6")
	end := sa(t, "DB$ GainLife | Defined$ You | LifeAmount$ 7 | SpellDescription$ Two hops down.")
	head.Sub = mid
	mid.Sub = end
	if got := CharmModeLabel(head, "SVar"); got != "Two hops down." {
		t.Fatalf("two-hop chain: label = %q, want the tail description", got)
	}

	none := sa(t, "DB$ GainLife | Defined$ You | LifeAmount$ 1")
	bare := sa(t, "DB$ GainLife | Defined$ You | LifeAmount$ 2")
	none.Sub = bare
	if got := CharmModeLabel(none, "SVarName"); got != "SVarName" {
		t.Fatalf("no description anywhere: label = %q, want the SVar-name fallback", got)
	}
	if got := CharmModeLabel(nil, "SVarName"); got != "SVarName" {
		t.Fatalf("nil sub: label = %q, want the fallback", got)
	}
}
