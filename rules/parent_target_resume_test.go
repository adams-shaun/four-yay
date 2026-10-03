package rules

// ParentTarget's nearest-targeting-link record (effects/parent_targets.go)
// must survive a resolution suspension. The record is scoped to one Resolve
// walk, so a chain whose LATER link poses a mid-resolution ask and re-enters
// with a fresh Ctx used to lose it and let the following untargeted reader's
// ParentTarget/ParentTargeted fall back to Ctx.Targets -- the ROOT's list --
// instead of the nearest targeting ancestor (Forge getParentTargetingCard).
//
// These carriers put an intervening ask of a DIFFERENT kind (a TgtChoose
// discard, ResumeKind "discard") between a targeting link and the
// ParentTarget reader. The ride under test is the parent-link record captured
// onto the pending resumePoint (resolution_ask.go resolutionParentLinks) and
// re-bound by resumeResolution (effects.Ctx.ResumeParentLinks).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const ptResumeBearSrc = "Name:ParentLink Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

const ptResumeAngelSrc = "Name:ParentLink Angel\nManaCost:4 W\nTypes:Creature Angel\nPT:4/4\nOracle:x\n"

// parentTargetBetweenScript is a root DealDamage (targeting link A), a DBTap
// sub (targeting link B), then a mid-resolution TgtChoose discard (the
// suspension), then an untargeted DBPump whose `Defined$ ParentTarget` must
// name DBTap's targets (link B), never the root's (link A).
func parentTargetBetweenScript() string {
	return "Name:Parent Link Between\nManaCost:R\nTypes:Sorcery\n" +
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1 | SubAbility$ DBTap\n" +
		"SVar:DBTap:DB$ Tap | ValidTgts$ Creature | SubAbility$ DBDiscard\n" +
		"SVar:DBDiscard:DB$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ DBPump\n" +
		"SVar:DBPump:DB$ Pump | Defined$ ParentTarget | KW$ Flying\n" +
		"Oracle:x\n"
}

// parentTargetEmptyLinkScript is the Min-0 twin: DBTap is "up to one target"
// and the test elects ZERO, so the recorded parent is an EMPTY list and the
// reader must pump nobody -- never fall through to the root's link A.
func parentTargetEmptyLinkScript() string {
	return "Name:Parent Link Empty\nManaCost:R\nTypes:Sorcery\n" +
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1 | SubAbility$ DBTap\n" +
		"SVar:DBTap:DB$ Tap | ValidTgts$ Creature | TargetMin$ 0 | TargetMax$ 1 | SubAbility$ DBDiscard\n" +
		"SVar:DBDiscard:DB$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ DBPump\n" +
		"SVar:DBPump:DB$ Pump | Defined$ ParentTarget | KW$ Flying\n" +
		"Oracle:x\n"
}

// advanceToDiscardAsk drives the fixture chain up to the intervening discard
// ask and returns it: the root's target ask is answered with root, the DBTap
// target ask with link (or NO target when link is zero), priority is passed,
// and the discard is left PENDING. It fails if the discard ask never appears
// -- the suspension is the test's own precondition.
func advanceToDiscardAsk(t *testing.T, e *Engine, root, link state.ObjID) *decision.Decision {
	t.Helper()
	targetAsk := 0
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while driving the chain (stack %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					submitChoices(t, e, o.Index)
					break
				}
			}
			continue
		}
		if d.ResumeKind == "discard" {
			return d
		}
		// A target ask, in chain order: the root's is answered with `root`,
		// the DBTap link's with `link` (or with no target when link is zero,
		// the Min-0 election).
		want := root
		if targetAsk > 0 {
			want = link
		}
		targetAsk++
		if want == 0 {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
				t.Fatalf("submit empty target answer: %v", err)
			}
			continue
		}
		pick := -1
		for _, o := range d.Options {
			if o.Obj == want {
				pick = o.Index
				break
			}
		}
		if pick < 0 {
			t.Fatalf("target ask #%d offers neither %d nor %d: %+v", targetAsk, root, link, d.Options)
		}
		submitChoices(t, e, pick)
	}
	t.Fatal("precondition: the intervening TgtChoose discard ask never appeared; the chain did not suspend between the targeting link and the ParentTarget reader")
	return nil
}

// driveParentLink answers the intervening discard and drains the stack.
func driveParentLink(t *testing.T, e *Engine, root, link state.ObjID) {
	t.Helper()
	d := advanceToDiscardAsk(t, e, root, link)
	answerDiscard(t, e, d)
	drainParentLink(t, e)
}

// drainParentLink passes priority until the stack is empty, failing on any
// unexpected ask.
func drainParentLink(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the chain (stack %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			for _, o := range d.Options {
				if o.Kind == "pass" {
					submitChoices(t, e, o.Index)
					break
				}
			}
			continue
		}
		t.Fatalf("unexpected ask while draining the chain: kind=%s resume=%q options=%+v", d.Kind, d.ResumeKind, d.Options)
	}
	t.Fatal("the stack never drained")
}

// answerDiscard takes the first TgtChoose option of the pending discard ask.
func answerDiscard(t *testing.T, e *Engine, d *decision.Decision) {
	t.Helper()
	pick := -1
	for _, o := range d.Options {
		if o.Kind == "discard" {
			pick = o.Index
			break
		}
	}
	if pick < 0 {
		t.Fatalf("discard ask offers no discard option: %+v", d.Options)
	}
	submitChoices(t, e, pick)
}

// TestParentTargetSurvivesALaterLinkSuspension is the ticket's main carrier:
// DBTap's targets must be what the post-suspension DBPump reads.
func TestParentTargetSurvivesALaterLinkSuspension(t *testing.T) {
	e, cfg, _ := newFixtureDeck(t, 7101, parentTargetBetweenScript(),
		ptResumeBearSrc, ptResumeAngelSrc)
	bear := moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
	angel := moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: root target Bear not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(angel); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: link target Angel not on the battlefield: %+v", o)
	}
	spell := fixtureInHand(t, e, "Parent Link Between")
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	driveParentLink(t, e, bear, angel)

	if e.HasKeyword(angel, "Flying") != true {
		t.Fatalf("ParentLink Angel (the DBTap link's target) lacks Flying: the resumed DBPump fell back to the root's targets instead of the nearest targeting link")
	}
	if e.HasKeyword(bear, "Flying") {
		t.Fatalf("ParentLink Bear (the ROOT's target) gained Flying: the resumed DBPump read Ctx.Targets, the root's list, not DBTap's targets")
	}
	replayCheck(t, e, cfg)
}

// TestParentTargetEmptyLinkSurvivesASuspension keeps the deliberate Min-0
// behaviour across the suspension: DBTap elected ZERO targets, so the parent
// is a RECORDED empty and DBPump pumps nobody -- never the root's Bear.
func TestParentTargetEmptyLinkSurvivesASuspension(t *testing.T) {
	e, cfg, _ := newFixtureDeck(t, 7102, parentTargetEmptyLinkScript(),
		ptResumeBearSrc, ptResumeAngelSrc)
	bear := moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
	angel := moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
	spell := fixtureInHand(t, e, "Parent Link Empty")
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	driveParentLink(t, e, bear, 0)

	if e.HasKeyword(bear, "Flying") {
		t.Fatalf("ParentLink Bear gained Flying: an EMPTY elected parent fell through to the root's targets across the suspension")
	}
	if e.HasKeyword(angel, "Flying") {
		t.Fatalf("ParentLink Angel gained Flying: DBTap elected no target, so the parent must be empty")
	}
	replayCheck(t, e, cfg)
}

// TestParentTargetLinkRecordClonesWithSuspension proves the ride is
// DEEP-copied, not aliased: a Clone taken while the discard ask is pending
// must own the parent-link record, and both engines must still resume to the
// nearest targeting link when the SAME answer is served to each.
func TestParentTargetLinkRecordClonesWithSuspension(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 7103, parentTargetBetweenScript(),
		ptResumeBearSrc, ptResumeAngelSrc)
	bear := moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
	angel := moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
	spell := fixtureInHand(t, e, "Parent Link Between")
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	d := advanceToDiscardAsk(t, e, bear, angel)

	// Precondition: the suspended frame carries a real record, so the clone
	// has something to alias -- a clone with an empty record would prove
	// nothing.
	if e.resume == nil || len(e.resume.parentLinks) == 0 {
		t.Fatalf("precondition: the pending discard frame carries no parent-link record: %+v", e.resume)
	}
	c := e.Clone()
	if c.resume == nil || len(c.resume.parentLinks) != len(e.resume.parentLinks) {
		t.Fatalf("clone lost the parent-link record: got %+v, want %d entries", c.resume, len(e.resume.parentLinks))
	}
	if len(c.resume.parentLinks[0]) > 0 && len(e.resume.parentLinks[0]) > 0 &&
		&c.resume.parentLinks[0][0] == &e.resume.parentLinks[0][0] {
		t.Fatal("clone aliases the parent-link backing array (shallow copy)")
	}

	// The same discard answer must drive both engines to the link target.
	answerDiscard(t, e, d)
	answerDiscard(t, c, d)
	drainParentLink(t, e)
	drainParentLink(t, c)
	for name, eng := range map[string]*Engine{"original": e, "clone": c} {
		if !eng.HasKeyword(angel, "Flying") {
			t.Fatalf("%s: ParentLink Angel lacks Flying after the suspension", name)
		}
		if eng.HasKeyword(bear, "Flying") {
			t.Fatalf("%s: ParentLink Bear gained Flying (fell back to the root's targets)", name)
		}
	}
}

// fixtureInHand returns the fixture card's id in seat 0's hand, failing if it
// is not there -- a precondition for the cast, so a setup that silently
// leaves the card in the library cannot pass.
func fixtureInHand(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("fixture %q is not in seat 0's hand", name)
	return 0
}
