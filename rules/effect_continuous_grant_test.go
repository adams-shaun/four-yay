package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// api:Effect delivering a general Mode$ Continuous static body is the bulk of
// the AGENTS.md "Known approximations" row this ticket closes: before it, only
// four hand-picked Continuous sub-shapes (GainsAbilitiesOfDefined$, MayPlay$,
// AddKeyword$ Cascade, SetMaxHandSize$) registered, and every other layer
// grant -- AddKeyword$/AddType$/AddPower$/SetColor$/RemoveAllAbilities$ -- was
// a bare "continuous effect Continuous unimplemented" Note. The general case
// now runs the SAME builder the printed-static scanner and the StaticEffect$
// move rider use, so an Effect-delivered grant reaches the layer walk.
//
// These tests use inline authored fixtures rather than corpus .txt (the
// licensing rule), and each asserts its own precondition -- the object is on
// the battlefield and does NOT already carry the granted characteristic -- so
// a vacuous setup fails loudly instead of passing.

// effectContinuousUnimplementedNotes collects the exact fallback Note the
// registration used to leave for a general Continuous body. A test that
// asserts only that a characteristic changed must also assert this is absent,
// or it would pass with the whole registration reverted to a no-op that
// happens to leave the board alone.
func effectContinuousUnimplementedNotes(e *Engine) []string {
	var out []string
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "continuous effect Continuous unimplemented") {
			out = append(out, ev.Text)
		}
	}
	return out
}

// TestEffectDeliveredContinuousGrantReachesLayerWalk pins the additive
// keyword half: DB$ Effect | StaticAbilities$ STFly, whose body is
// Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Flying.
func TestEffectDeliveredContinuousGrantReachesLayerWalk(t *testing.T) {
	t.Parallel()
	grant := card(t, "Name:GrantFlight\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | StaticAbilities$ STFly\n"+
		"SVar:STFly:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Flying\n"+
		"Oracle:x\n")
	e := handEngine(t, grant)
	e.G.Players[0].Pool[state.MU] = 1
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// Precondition: the object is on the battlefield and the granted
	// characteristic is genuinely absent before the grant resolves.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: bear not on the battlefield: %+v", o)
	}
	if e.G.Obj(bear).Face().HasKeyword("Flying") {
		t.Fatal("precondition: the bear's printed face already has Flying")
	}
	if e.HasKeyword(bear, "Flying") {
		t.Fatal("precondition: the bear already has Flying before the grant")
	}

	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Stack) != 0 {
		t.Fatalf("GrantFlight did not resolve: stack %v", e.G.Stack)
	}
	if notes := effectContinuousUnimplementedNotes(e); len(notes) != 0 {
		t.Fatalf("the general Continuous grant fell to the unimplemented Note: %v", notes)
	}
	if !e.HasKeyword(bear, "Flying") {
		t.Fatal("Effect-delivered Mode$ Continuous AddKeyword$ never reached the layer walk")
	}
}

// TestEffectDeliveredContinuousGrantScopesToRemembered pins the
// Remembered-binding half: Affected$ Card.IsRemembered must select exactly
// the objects the Effect captured (RememberObjects$ Targeted), not the whole
// battlefield. Without the layer walk binding the registered set, such a spec
// matches nobody; with an over-broad binding it would match everyone, so the
// test asserts BOTH the chosen creature gained the keyword and the untargeted
// one did not.
func TestEffectDeliveredContinuousGrantScopesToRemembered(t *testing.T) {
	t.Parallel()
	grant := card(t, "Name:MarkPrey\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | ValidTgts$ Creature | StaticAbilities$ STMark | RememberObjects$ Targeted\n"+
		"SVar:STMark:Mode$ Continuous | Affected$ Card.IsRemembered | AddKeyword$ Flying\n"+
		"Oracle:x\n")
	e := handEngine(t, grant)
	e.G.Players[0].Pool[state.MU] = 1
	chosen := onBoard(t, e, 0, "Name:Chosen\nManaCost:G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n")
	other := onBoard(t, e, 0, "Name:Other\nManaCost:G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n")

	if e.G.Obj(chosen).Face().HasKeyword("Flying") || e.G.Obj(other).Face().HasKeyword("Flying") {
		t.Fatal("precondition: a creature's printed face already has Flying")
	}
	if e.HasKeyword(chosen, "Flying") || e.HasKeyword(other, "Flying") {
		t.Fatal("precondition: a creature already has Flying before the grant")
	}

	e.askPriority(0)
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == chosen {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the chosen creature was not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)

	if notes := effectContinuousUnimplementedNotes(e); len(notes) != 0 {
		t.Fatalf("the general Continuous grant fell to the unimplemented Note: %v", notes)
	}
	if !e.HasKeyword(chosen, "Flying") {
		t.Fatal("the remembered (targeted) creature did not gain Flying: IsRemembered matched nobody")
	}
	if e.HasKeyword(other, "Flying") {
		t.Fatal("the untargeted creature gained Flying: Affected$ Card.IsRemembered matched everything")
	}
}
