package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// temptStackCopies counts the CR 707.10 copy objects on the stack by their
// controller. The original spell is not a copy and is not counted.
func temptStackCopies(e *Engine) (seat0, seat1 int) {
	for _, id := range e.G.Stack {
		o := e.G.Obj(id)
		if o == nil || !o.IsCopy {
			continue
		}
		switch o.Controller {
		case 0:
			seat0++
		case 1:
			seat1++
		}
	}
	return seat0, seat1
}

func stackHasObj(e *Engine, id state.ObjID) bool {
	for _, s := range e.G.Stack {
		if s == id {
			return true
		}
	}
	return false
}

// temptCorpusShape asserts the REAL compiled Tempt with Mayhem carries the two
// linked shapes the fix is about, so a corpus change cannot leave this test
// passing without exercising PlayerCountRememberedController or Controller$
// Remembered. It reads the compiled card from the registry, never script text.
func temptCorpusShape(t *testing.T, reg *cards.Registry) {
	t.Helper()
	c := mustCorpusCard(t, reg, "Tempt with Mayhem")
	var xBody, copyController, rememberCopies string
	for _, f := range c.Faces {
		if v, ok := f.SVars["X"]; ok {
			xBody = v
		}
		// DBCopy is SVar-defined (SVar:DBCopy:DB$ CopySpellAbility ...), so
		// resolve the face's SVars rather than walking only SubAbility chains.
		for name := range f.SVars {
			sub := cards.ResolveSVar(f.SVars, name)
			if sub != nil && sub.API == "CopySpellAbility" && sub.Params["Controller"] != "" {
				copyController = sub.Params["Controller"]
				rememberCopies = sub.Params["RememberCopies"]
			}
		}
	}
	if xBody != "PlayerCountRememberedController$Amount/Plus.1" {
		t.Fatalf("Tempt with Mayhem SVar:X = %q, want PlayerCountRememberedController$Amount/Plus.1", xBody)
	}
	if copyController != "Remembered" || rememberCopies != "True" {
		t.Fatalf("Tempt with Mayhem copy clause = Controller$ %q, RememberCopies$ %q; want Remembered / True",
			copyController, rememberCopies)
	}
}

// TestTemptWithMayhemCountsRememberedCopiers drives the real compiled Tempt
// with Mayhem end to end (no Forge script text): it resolves against an
// instant on the stack, one opponent accepts its copy offer, and the card's
// DBCopySelf must then make 1 + X copies where X reads
// PlayerCountRememberedController$Amount/Plus.1 -- the count of DISTINCT
// controllers of the copy objects DBCopy remembered. The opponent's copy is
// controlled by the offering seat (Controller$ Remembered), so without the
// fix the count is unresolved (X defaults to 1) and the opponent's copy is
// mis-owned by the caster; the two assertions below name both failures.
func TestTemptWithMayhemCountsRememberedCopiers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	temptCorpusShape(t, reg)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Tempt with Mayhem", "Dark Ritual"}, nil, nil, nil)
	addMana(t, e, 0, "BRRR")
	tempt := miscHandObj(t, e, 0, "Tempt with Mayhem")
	ritual := miscHandObj(t, e, 0, "Dark Ritual")

	// Put an instant on the stack (Dark Ritual has no targets), then respond
	// with Tempt targeting it.
	submitChoices(t, e, miscCastOption(t, e, ritual))
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the Dark Ritual spell before Tempt is cast", e.G.Stack)
	}
	target := e.G.Stack[0]
	if o := e.G.Obj(target); o == nil || o.IsCopy || o.Zone != state.ZStack {
		t.Fatalf("stack fixture %d is not the original Dark Ritual spell: %+v", target, o)
	}
	submitChoices(t, e, miscCastOption(t, e, tempt))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Tempt's target ask, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the Dark Ritual spell was not offered as Tempt's target: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// Resolve Tempt: the opponent (seat 1) accepts its copy offer, then Tempt's
	// DBCopySelf makes the additional copies. Stop at the first priority once
	// Tempt has left the stack and the copies are minted, BEFORE they resolve.
	accepted := false
	for i := 0; i < 80; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision pending while resolving Tempt (step %d)", i)
		}
		if d.Kind == decision.KPriority {
			if !stackHasObj(e, tempt) {
				seat0, seat1 := temptStackCopies(e)
				if seat0+seat1 >= 1 {
					break
				}
			}
			passPriority(t, e)
			continue
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "repeat_each_optional" {
			// PRECONDITION: the offer belongs to the opponent seat, not the
			// caster. A caster-owned offer would make the ownership assertion
			// below pass for the wrong reason.
			if d.Player != 1 {
				t.Fatalf("Tempt's copy offer went to seat %d, want the opponent seat 1", d.Player)
			}
			yes := -1
			for _, o := range d.Options {
				if o.Kind == "yes" {
					yes = o.Index
				}
			}
			if yes < 0 {
				t.Fatalf("Tempt's copy offer has no yes option: %+v", d.Options)
			}
			submitChoices(t, e, yes)
			accepted = true
			continue
		}
		if d.Kind == decision.KTarget {
			submitChoices(t, e, d.Options[0].Index)
			continue
		}
		var cs []int
		for j := 0; j < d.Min && j < len(d.Options); j++ {
			cs = append(cs, d.Options[j].Index)
		}
		submitChoices(t, e, cs...)
	}
	if !accepted {
		t.Fatal("seat 1 was never offered Tempt's per-opponent copy")
	}
	if stackHasObj(e, tempt) {
		t.Fatal("Tempt is still on the stack; the copy assertions below would read a pre-resolution board")
	}
	seat0Copies, seat1Copies := temptStackCopies(e)
	// PRECONDITION: something was actually copied. A zero here means the
	// scenario never reached the copy path and both counts below would be 0.
	if seat0Copies+seat1Copies == 0 {
		t.Fatal("no copy reached the stack; the scenario did not exercise Tempt's copy clause")
	}
	if seat1Copies != 1 {
		t.Fatalf("seat 1's copies = %d, want 1: Controller$ Remembered must give the offering seat its copy", seat1Copies)
	}
	if seat0Copies != 2 {
		t.Fatalf("seat 0's copies = %d, want 2: once plus one per remembered copier (X = PlayerCountRememberedController$Amount/Plus.1 = 1+1)", seat0Copies)
	}
	replayCheck(t, e, cfg)
}
