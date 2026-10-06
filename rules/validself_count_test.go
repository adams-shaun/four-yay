package rules

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestSetAudit_spm_Kraven_GreatestPowerDeathTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Kraven the Hunter", "Grizzly Bears", "Colossal Dreadmaw", "Gigantosaurus")
	kraven := crAbortMove(t, e, 0, "Kraven the Hunter", state.ZBattlefield)
	bear := crAbortMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	big := crAbortMove(t, e, 0, "Colossal Dreadmaw", state.ZBattlefield)
	// A creature on Kraven's OWN side, larger than the opponent's greatest.
	// A gate that scanned the dying creature's owner's battlefield (the move
	// resets its controller to its owner) instead of the player it last
	// belonged to would suppress the 6/6's death too, so this board makes the
	// two readings disagree. See the assertion below.
	own := crAbortMove(t, e, 0, "Gigantosaurus", state.ZBattlefield)
	// Put both tested creatures under one opposing controller, retaining the
	// real corpus faces and their distinct powers (2 versus 6).
	e.emit(events.Event{Kind: events.ControlChange, Obj: bear, Player: 1})
	e.emit(events.Event{Kind: events.ControlChange, Obj: big, Player: 1})
	kravenObj := e.G.Obj(kraven)
	if kravenObj == nil || kravenObj.Zone != state.ZBattlefield || kravenObj.Controller != 0 {
		t.Fatal("precondition: Kraven must be a seat-0 battlefield permanent")
	}
	if e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(big).Zone != state.ZBattlefield || e.G.Obj(bear).Controller != 1 || e.G.Obj(big).Controller != 1 {
		t.Fatal("precondition: the 2/2 and 6/6 must both be controlled by Kraven's opponent")
	}
	if e.G.Obj(own).Zone != state.ZBattlefield || e.G.Obj(own).Controller != 0 {
		t.Fatal("precondition: the 10/10 must be a seat-0 battlefield permanent (Kraven's side)")
	}
	if e.Power(bear) >= e.Power(big) {
		t.Fatalf("precondition: tested powers are not ordered: bear=%d dreadmaw=%d", e.Power(bear), e.Power(big))
	}
	if e.Power(own) <= e.Power(big) {
		t.Fatalf("precondition: Kraven's own creature must exceed the opponent's greatest, else the owner-vs-LKI distinction is invisible: gigantosaurus=%d dreadmaw=%d", e.Power(own), e.Power(big))
	}
	tr := crTriggerFixture(t, e, kraven, "ChangesZone", "Draw")
	if got := tr.Params["CheckOnTriggeredCard"]; got != "X GE1" {
		t.Fatalf("fixture precondition: CheckOnTriggeredCard = %q, want X GE1", got)
	}
	if got, ok := effects.EvalCountOK(e, &effects.Ctx{Source: kraven, Controller: 0, TriggerContext: effects.TriggerContext{TriggerCard: bear}}, kravenObj.Face().SVars["X"]); !ok || got != 0 {
		t.Fatalf("precondition: small creature's ValidSelf count = %d, resolved=%v; want 0, true", got, ok)
	}

	// A smaller opposing creature's death is suppressed while the 6/6 lives.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	if got := crTriggerStackCount(e, kraven); got != 0 {
		t.Fatalf("the 2/2's death fired Kraven while a 6/6 was greatest: stack count %d", got)
	}

	// The greatest-power creature's death satisfies the same live gate.
	e.emit(events.Event{Kind: events.MoveZone, Obj: big, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	if got := crTriggerStackCount(e, kraven); got != 1 {
		t.Fatalf("the greatest-power creature's death queued %d Kraven triggers, want 1", got)
	}
	before := len(e.G.Zone(state.ZHand, 0))
	crTriggerPassRound(t, e, "Kraven the Hunter")
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Fatalf("Kraven drew %d cards, want exactly 1", got-before)
	}
	if got := e.G.Obj(kraven).Counter("P1P1"); got != 1 {
		t.Fatalf("Kraven has %d +1/+1 counters, want 1", got)
	}
}

func TestCountValidSelfUnreadableArgumentFailsClosed(t *testing.T) {
	// The head is RECOGNISED even for an argument the filter grammar cannot
	// read, and such an argument evaluates to 0 (an evaluated zero), never to
	// an unresolvable body a CheckSVar gate would fail open on and never to a
	// silent 1 that would make a negated predicate gate-true. A genuinely
	// unreadable predicate stays the fail-closed class; `Card.!IsPrepared` is
	// MODELLED now and is covered separately below.
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Kraven the Hunter")
	src := crAbortMove(t, e, 0, "Kraven the Hunter", state.ZBattlefield)
	svars := e.G.Obj(src).Face().SVars
	for _, arg := range []string{"Card.!someUnmodelledPredicate"} {
		body := "Count$ValidSelf " + arg
		if got, ok := effects.EvalCountOK(e, &effects.Ctx{Source: src, Controller: 0, SVars: svars}, body); !ok || got != 0 {
			t.Fatalf("Count$ValidSelf %q = %d, resolved=%v; want 0, true (fail closed)", arg, got, ok)
		}
	}
	// An empty argument is the degenerate unreadable case and fails closed the
	// same way, rather than panicking on a missing self binding.
	if got, ok := effects.EvalCountOK(e, &effects.Ctx{}, "Count$ValidSelf"); !ok || got != 0 {
		t.Fatalf("Count$ValidSelf with no argument = %d, resolved=%v; want 0, true", got, ok)
	}
	// The `Card$CreatureType` spelling is NOT unreadable: it is Diligent
	// Zookeeper's AffectedX property read and must keep reaching its own
	// property path, not the event-anchored matcher. Kraven's printed types
	// include non-subtype words (Creature, Legendary), so the count is the
	// number of CREATURE SUBTYPES among them; the important guard is that it is
	// a non-zero property read, because a regression to the event matcher's
	// fail-closed 0 here is the Zookeeper defect this assertion catches.
	if got, ok := effects.EvalCountOK(e, &effects.Ctx{Source: src, Controller: 0, SVars: svars}, "Count$ValidSelf Card$CreatureType"); !ok || got <= 0 {
		t.Fatalf("Count$ValidSelf Card$CreatureType = %d, resolved=%v; want > 0, true (the property read must not be preempted by the event-anchored matcher)", got, ok)
	}
	// `Card.!IsPrepared` is MODELLED too: Kraven is not prepared, so the
	// negation is a real evaluated 1 and `Card.IsPrepared` a real 0.
	if got, ok := effects.EvalCountOK(e, &effects.Ctx{Source: src, Controller: 0, SVars: svars}, "Count$ValidSelf Card.!IsPrepared"); !ok || got != 1 {
		t.Fatalf("Count$ValidSelf Card.!IsPrepared = %d, resolved=%v; want 1, true", got, ok)
	}
	if got, ok := effects.EvalCountOK(e, &effects.Ctx{Source: src, Controller: 0, SVars: svars}, "Count$ValidSelf Card.IsPrepared"); !ok || got != 0 {
		t.Fatalf("Count$ValidSelf Card.IsPrepared = %d, resolved=%v; want 0, true", got, ok)
	}
	// `Card.IsSuspected` is a MODELLED predicate and routes through the
	// event-anchored matcher: Suspected is not a status Kraven's source has,
	// so the count is a real evaluated 0, not a fail-closed refusal.
	if got, ok := effects.EvalCountOK(e, &effects.Ctx{Source: src, Controller: 0, SVars: svars}, "Count$ValidSelf Card.IsSuspected"); !ok || got != 0 {
		t.Fatalf("Count$ValidSelf Card.IsSuspected = %d, resolved=%v; want 0, true", got, ok)
	}
}

func TestCountValidSelfCorpusCensus(t *testing.T) {
	t.Parallel()
	// The census reads the gitignored GPL card scripts: with no .cards/
	// checkout the corpus-dependent convention is a Skip, not a hard failure.
	testutil.CorpusRegistry(t)
	// Count carrier files, not raw lines: this catches a new ValidSelf
	// mechanism sibling without depending on Forge text in a tracked fixture.
	root := filepath.Join("..", ".cards", "cardsfolder")
	files := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "Count$ValidSelf") {
			files[filepath.Base(path)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(files))
	for name := range files {
		got = append(got, name)
	}
	slices.Sort(got)
	want := []string{"diligent_zookeeper.txt", "frantic_scapegoat.txt", "kraven_the_hunter.txt", "paradox_shaper_omit_variables.txt", "stingerquill_voxmancer_vicious_verse.txt", "unique_charmed_pants.txt", "woodwork_prodigy_soul_tether.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("Count$ValidSelf carrier census = %v, want %v", got, want)
	}
}
