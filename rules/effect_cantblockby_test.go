package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// effectCantBlockByCount counts the live registered CantBlockBy restrictions
// (the Effect-delivered route this ticket wires), so a test can assert the
// registration itself happened rather than only its shadow on canBlock.
func effectCantBlockByCount(e *Engine) int {
	n := 0
	for _, ce := range e.active() {
		if ce.Restriction == "CantBlockBy" {
			n++
		}
	}
	return n
}

// chooseTargetOption submits the pending target decision's option for obj.
func chooseTargetOption(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no target decision pending")
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit target %d: %v", obj, err)
			}
			return
		}
	}
	t.Fatalf("no target option for %d: %+v", obj, d.Options)
}

// TestEffectCantBlockBySuspiciousBookcaseUnblocksItsTarget pins the
// Effect-registered CantBlockBy route end to end on the real corpus card:
// Suspicious Bookcase's "{3},{T}: Target creature can't be blocked this turn"
// is an `AB$ Effect | StaticAbilities$ Unblockable` whose SVar body is
// `Mode$ CantBlockBy | ValidAttacker$ Card.IsRemembered` (the dominant
// unblockable template -- measured 246 corpus files carry the shape). Before
// this ticket effEffect's static-mode switch had no CantBlockBy case, the
// ability resolved as an unimplemented Note and the targeted creature stayed
// blockable. After it the ability resolves to a live CantBlockBy restriction
// that the blockRestricted combat walk consults beside the printed statics.
func TestEffectCantBlockBySuspiciousBookcaseUnblocksItsTarget(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Suspicious Bookcase")
	bookcase := searchMoveByName(t, e, "Suspicious Bookcase", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	other := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if bear == other {
		t.Fatal("fixture degenerate: the two bears are the same object")
	}
	blocker := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	// Preconditions the assertions below depend on.
	if o := e.G.Obj(bookcase); o == nil || o.Face() == nil || o.Face().Name != "Suspicious Bookcase" {
		t.Fatalf("bookcase fixture missing: %+v", o)
	}
	if len(e.G.Obj(bookcase).Face().Abilities) == 0 {
		t.Fatal("bookcase has no activated ability to pin")
	}
	// Control: with the bear attacking and NO effect, the Memnite can block.
	e.G.Obj(bear).IsAttacking, e.G.Obj(bear).Attacking = true, 1
	if !e.canBlock(blocker, bear) {
		t.Fatal("precondition failed: the Memnite cannot block the bear before the effect")
	}
	e.G.Obj(bear).IsAttacking, e.G.Obj(bear).Attacking = false, 0
	if n := effectCantBlockByCount(e); n != 0 {
		t.Fatalf("precondition failed: %d CantBlockBy effects registered before the ability resolved", n)
	}

	// Activate the real ability: {3},{T} targeting the bear.
	addMana(t, e, 0, "CCC")
	e.G.Obj(bookcase).SummonSick = false
	e.priorityRound()
	opt := abilityOption(t, e, bookcase, 0)
	submitChoices(t, e, opt.Index)
	chooseTargetOption(t, e, bear)
	passUntilStackEmpty(t, e, 40)

	if n := effectCantBlockByCount(e); n != 1 {
		t.Fatalf("CantBlockBy effects registered after the ability resolved = %d, want 1", n)
	}
	// The readable body must have registered, NOT been refused: no
	// unimplemented-CantBlockBy note may carry the resolution (fails pre-fix,
	// where the stand-in Note is exactly what this ability emits).
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.Note && ev.Obj == bookcase &&
			strings.Contains(ev.Text, "CantBlockBy") && strings.Contains(ev.Text, "unimplemented") {
			t.Error("the readable ValidAttacker$ body was refused with an unimplemented CantBlockBy note")
			break
		}
	}

	// The targeted bear is unblockable; a bear the effect does not remember
	// is not (ValidAttacker$ Card.IsRemembered scopes to the remembered set).
	e.G.Obj(bear).IsAttacking, e.G.Obj(bear).Attacking = true, 1
	e.G.Obj(other).IsAttacking, e.G.Obj(other).Attacking = true, 1
	if e.canBlock(blocker, bear) {
		t.Error("the targeted bear was blockable while the CantBlockBy effect was live")
	}
	if !e.canBlock(blocker, other) {
		t.Error("an attacker outside the effect's remembered set was also restricted (over-broad)")
	}

	// End to end through the real combat steps: declare the bear as the sole
	// attacker; the defender's blockers decision must offer no pair against
	// it (askBlockers builds every option through canBlock).
	e.G.Obj(other).IsAttacking, e.G.Obj(other).Attacking = false, 0
	e.G.Obj(bear).SummonSick = false
	e.pending = nil // drop the main-phase priority ask; combat asks its own
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	e.askAttackers()
	submitAttackersOnly(t, e, bear)
	drainCombatPriority(t, e)
	d := e.Pending()
	if d != nil && d.Kind == decision.KBlockers {
		for _, o := range d.Options {
			if o.Obj == blocker {
				t.Errorf("blockers decision offered the Memnite against the unblockable bear: %+v", d.Options)
				break
			}
		}
	}

	// Lifetime (the absent-Duration$ read): Forge's no-Duration Effect is a
	// THIS-TURN effect, so end-of-turn cleanup drops it and the bear is
	// blockable again.
	e.EndOfTurnCleanup()
	if n := effectCantBlockByCount(e); n != 0 {
		t.Fatalf("CantBlockBy effects after end-of-turn cleanup = %d, want 0 (no Duration$ is a this-turn effect)", n)
	}
	if !e.canBlock(blocker, bear) {
		t.Error("the bear stayed unblockable after end-of-turn cleanup dropped the effect")
	}
}

// TestEffectCantBlockByRegistrationScopesReadableAndUnreadableBodies pins the
// registration gate in BOTH directions. Phase 1 is the FIX-SENSITIVE control
// (the readable Suspicious Bookcase body MUST register: count 1, no
// unimplemented note -- fails against reverting the fix, where the count is 0
// and the note is exactly what stands in); phase 2 refuses the unreadable
// shape: an Effect body whose CantBlockBy SVar carries a parameter the
// consumption path cannot evaluate (space_beleren's ValidBlockerRelative$
// sector grammar) must NOT register blanket -- it resolves as the loud
// unimplemented Note and the creature stays blockable, the permissive
// direction for a restriction.
func TestEffectCantBlockByRegistrationScopesReadableAndUnreadableBodies(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Suspicious Bookcase", "Space Beleren")
	bookcase := searchMoveByName(t, e, "Suspicious Bookcase", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	walker := searchMoveByName(t, e, "Space Beleren", state.ZBattlefield)
	if o := e.G.Obj(walker); o == nil || o.Face() == nil || o.Face().Name != "Space Beleren" {
		t.Fatalf("walker fixture missing: %+v", o)
	}
	if o := e.G.Obj(bookcase); o == nil || o.Face() == nil || o.Face().Name != "Suspicious Bookcase" {
		t.Fatalf("bookcase fixture missing: %+v", o)
	}
	if n := effectCantBlockByCount(e); n != 0 {
		t.Fatalf("precondition failed: %d CantBlockBy effects registered before either ability resolved", n)
	}

	// Phase 1 (control, fix-sensitive): the readable ValidAttacker$
	// Card.IsRemembered body registers exactly one effect and is not refused.
	addMana(t, e, 0, "CCC")
	e.G.Obj(bookcase).SummonSick = false
	e.priorityRound()
	opt := abilityOption(t, e, bookcase, 0)
	submitChoices(t, e, opt.Index)
	chooseTargetOption(t, e, bear)
	passUntilStackEmpty(t, e, 40)
	if n := effectCantBlockByCount(e); n != 1 {
		t.Fatalf("the readable body did not register: CantBlockBy effects = %d, want 1", n)
	}
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.Note && ev.Obj == bookcase &&
			strings.Contains(ev.Text, "CantBlockBy") && strings.Contains(ev.Text, "unimplemented") {
			t.Error("the readable ValidAttacker$ body was refused with an unimplemented CantBlockBy note")
			break
		}
	}

	// Phase 2 (refusal): the unreadable ValidBlockerRelative$ body registers
	// nothing -- the count stays at 1 -- and the loud note is emitted.
	e.G.Obj(walker).SummonSick = false
	e.priorityRound()
	// Space Beleren's sector ability is the +1 loyalty (index 0).
	submitChoices(t, e, abilityOption(t, e, walker, 0).Index)
	passUntilStackEmpty(t, e, 40)
	if n := effectCantBlockByCount(e); n != 1 {
		t.Fatalf("the ValidBlockerRelative$ body registered anyway (%d effects, want 1)", n)
	}
	noted := false
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.Note && ev.Obj == walker &&
			strings.Contains(ev.Text, "CantBlockBy") && strings.Contains(ev.Text, "unimplemented") {
			noted = true
			break
		}
	}
	if !noted {
		t.Error("no unimplemented CantBlockBy note was emitted for the unreadable body")
	}
}
