package effects

// TestTriggeredTargetPredicate pins Forge's bare TriggeredTarget /
// NotDefinedTriggeredTarget filter predicates: the candidate must be (or, for
// the negated spelling, not be) the object the triggering event captured in
// TriggerContext.TriggerTarget. The corpus carriers are Blade of Shared Souls
// (`Creature.YouCtrl+!TriggeredTarget`, "another target creature you
// control"), Toralf, God of Fury
// (`Creature.!TriggeredTarget,Player,Planeswalker.!TriggeredTarget`) and
// Pawpatch Recruit (`Creature.YouCtrl+NotDefinedTriggeredTarget`). The
// binding rides SpecContext.TriggerContext, which rules' push-time target ask
// (rules/targetSpecContext) binds from the pushed trigger's captured context;
// an absent or player-valued binding fails closed so the leading-'!' spelling
// cannot invert the absence into a match.

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestTriggeredTargetPredicate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	bearer := corpusObject(t, reg, g, "Grizzly Bears")
	other := corpusObject(t, reg, g, "Hill Giant")

	// Preconditions: the two compared objects differ, both are really on the
	// battlefield under the asking seat, and the context under test really
	// binds the bearer -- the assertions below are vacuous otherwise.
	if bearer.ID == other.ID {
		t.Fatalf("precondition failed: bearer and other share id %d", bearer.ID)
	}
	if g.Obj(bearer.ID).Zone != state.ZBattlefield || g.Obj(other.ID).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: zones = %s/%s, want both battlefield",
			g.Obj(bearer.ID).Zone, g.Obj(other.ID).Zone)
	}
	if g.Obj(bearer.ID).Controller != 0 || g.Obj(other.ID).Controller != 0 {
		t.Fatalf("precondition failed: controllers = %d/%d, want both seat 0",
			g.Obj(bearer.ID).Controller, g.Obj(other.ID).Controller)
	}
	bound := SpecContext{You: 0}
	bound.TriggerTarget = state.Target{Obj: bearer.ID}
	if bound.TriggerTarget.Obj != bearer.ID {
		t.Fatalf("precondition failed: bound.TriggerTarget = %+v, want bearer %d", bound.TriggerTarget, bearer.ID)
	}

	// Positive: TriggeredTarget matches exactly the captured object.
	if !MatchesObjectCtx(g, "Creature.YouCtrl+TriggeredTarget", bearer, bound) {
		t.Errorf("Creature.YouCtrl+TriggeredTarget must match the bound trigger target")
	}
	if MatchesObjectCtx(g, "Creature.YouCtrl+TriggeredTarget", other, bound) {
		t.Errorf("Creature.YouCtrl+TriggeredTarget must not match a different creature")
	}
	// The corpus's negated spelling excludes exactly that object.
	if MatchesObjectCtx(g, "Creature.YouCtrl+!TriggeredTarget", bearer, bound) {
		t.Errorf("Creature.YouCtrl+!TriggeredTarget must not match the bound trigger target")
	}
	if !MatchesObjectCtx(g, "Creature.YouCtrl+!TriggeredTarget", other, bound) {
		t.Errorf("Creature.YouCtrl+!TriggeredTarget must match another creature you control")
	}
	// NotDefinedTriggeredTarget is the one positive-evaluation spelling of the
	// same exclusion (Pawpatch Recruit).
	if MatchesObjectCtx(g, "Creature.YouCtrl+NotDefinedTriggeredTarget", bearer, bound) {
		t.Errorf("Creature.YouCtrl+NotDefinedTriggeredTarget must not match the bound trigger target")
	}
	if !MatchesObjectCtx(g, "Creature.YouCtrl+NotDefinedTriggeredTarget", other, bound) {
		t.Errorf("Creature.YouCtrl+NotDefinedTriggeredTarget must match another creature you control")
	}

	// An unbound (or player-valued) referent fails closed: the positive
	// spelling matches nothing and the negated spelling cannot invert the
	// absence into a match, the TriggeredCard convention.
	unbound := SpecContext{You: 0}
	if MatchesObjectCtx(g, "Creature.YouCtrl+TriggeredTarget", bearer, unbound) {
		t.Errorf("TriggeredTarget with no bound context must match nothing")
	}
	if MatchesObjectCtx(g, "Creature.YouCtrl+!TriggeredTarget", bearer, unbound) ||
		MatchesObjectCtx(g, "Creature.YouCtrl+!TriggeredTarget", other, unbound) {
		t.Errorf("!TriggeredTarget with no bound context must fail closed, not match")
	}
	playerCtx := SpecContext{You: 0}
	playerCtx.TriggerTarget = state.Target{Player: 1, IsPlayer: true}
	if MatchesObjectCtx(g, "Creature.YouCtrl+!TriggeredTarget", bearer, playerCtx) {
		t.Errorf("!TriggeredTarget with a player-valued binding must fail closed for objects")
	}

	// The matcher and the UnknownPredicates census agree on both spellings,
	// negated included.
	for _, spec := range []string{
		"Creature.TriggeredTarget",
		"Creature.!TriggeredTarget",
		"Creature.NotDefinedTriggeredTarget",
		"Creature.YouCtrl+!TriggeredTarget",
	} {
		if un := UnknownPredicates(spec); len(un) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want empty (TriggeredTarget is recognised)", spec, un)
		}
	}
}
