package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Drafna's Restoration's TargetMax$ X reads Count$ValidGraveyard ...
// TargetedPlayer. That parent-relative dynamic bound cannot be resolved by the
// cast-time bound context, so the dependent target must remain a resolution ask
// rather than being recorded as an empty cast-time target set.
func TestDrafnasRestorationDynamicParentBoundDefersTargets(t *testing.T) {
	e, _, mine, theirs := cr601Board(t, 62005,
		map[string]state.Zone{"Drafna's Restoration": state.ZHand},
		map[string]state.Zone{"Ornithopter": state.ZGraveyard})
	spell, artifact := mine["Drafna's Restoration"], theirs["Ornithopter"]
	if o := e.G.Obj(artifact); o == nil || o.Zone != state.ZGraveyard || o.Controller != 1 {
		t.Fatalf("precondition: target artifact = %+v, want seat 1's graveyard", o)
	}
	if spell == artifact {
		t.Fatal("precondition: spell and graveyard artifact must be distinct")
	}
	root := e.G.Obj(spell).Face().Abilities[0]
	if root == nil || root.Sub == nil {
		t.Fatalf("precondition: Drafna's target-chain ability missing: %+v", root)
	}
	sa := root.Sub
	if got := subTargetingRootUnbindable(sa, e.G.Obj(spell).Face().SVars); !got {
		t.Fatal("Drafna's dynamic parent-relative TargetMax must be deferred")
	}
	if _, max := e.resolvedTargetBounds(0, spell, sa.Sub, 0); max != 1 {
		t.Fatalf("unbound cast-time TargetMax = %d, want unresolved default 1 (the parent player is not bound)", max)
	}

	addMana(t, e, 0, "U")
	edrSeatZeroPriority(t, e)
	cr601Cast(t, e, spell, "")
	rootAsk := e.Pending()
	if rootAsk == nil || rootAsk.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want Drafna's root player target", rootAsk)
	}
	rootAnswered := false
	for _, opt := range rootAsk.Options {
		if opt.Kind == "player" && opt.Player == 1 {
			submitChoices(t, e, opt.Index)
			rootAnswered = true
			break
		}
	}
	if !rootAnswered {
		t.Fatalf("root target ask does not offer seat 1: %+v", rootAsk.Options)
	}
	if d := e.Pending(); d != nil && d.ResumeKind == "cast_sub" {
		t.Fatalf("dynamic parent-relative target was incorrectly asked at cast: %+v", d)
	}

	// Pass priority to resolve the spell. Its target ask now has a bound based
	// on the chosen player's graveyard and must offer the artifact.
	for i := 0; i < 2 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("pending before resolution = %+v, want priority", d)
		}
		submitChoices(t, e, passIndex(t, d))
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending = %+v, want Drafna's resolution-time artifact target ask", d)
	}
	found := false
	for _, opt := range d.Options {
		if opt.Obj == artifact {
			found = true
			submitChoices(t, e, opt.Index)
			break
		}
	}
	if !found {
		t.Fatalf("Drafna's resolution ask does not offer seat 1's artifact %d: %+v", artifact, d.Options)
	}
	if o := e.G.Obj(artifact); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("target artifact after resolution = %+v, want library", o)
	}
}
