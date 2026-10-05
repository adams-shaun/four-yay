package rules

// The TargetUnique$ accumulator ("every target this resolution chooses must be
// different", CR 601.2c) must ride EVERY suspension, not only the shared
// target pre-ask. These carriers put an intervening ask of a DIFFERENT kind --
// the row names a dig/scry/arrange ask -- between two TargetUnique$ riders:
// the resumed Ctx rebuilt from the pending ask's ride (and, before that
// ticket, an ask that did not stamp that ride) dropped the accumulator, so
// the later rider re-offered the earlier rider's pick.
//
// The carriers are TRIGGERED abilities whose two rider links are now announced
// at the trigger's placement (CR 603.3d, KTarget/trig_sub) -- the second rider
// already excluding the first's pick through the placement accumulator. What
// is pinned here is that both announced answers survive the intervening Dig /
// Scry suspension and act on the named players, and that the Dig does not
// re-pose a rider's target. The spell forms of the same chains are pinned at
// their cast-time timing by TestSpellRidersAnnouncedOnCastSurviveASuspension.
//
// The target-bearing SA lines are the corpus TargetUnique$ parameter
// spellings (see target_unique_test.go's chainUniqueScript); the intervening
// Dig line is dig_ask_test.go's minimal shape.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// digBetweenRidersScript is an enters trigger whose body is a root Draw
// followed by two TargetUnique$ player riders with a Dig -- a "dig"-kind KChoose suspension, an ask kind OUTSIDE
// the shared target tail -- parked between them.
func digBetweenRidersScript() string {
	return "Name:Dig Between Riders\nManaCost:1\nTypes:Creature Bear\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigRoot | TriggerDescription$ x\n" +
		"SVar:TrigRoot:DB$ Draw | NumCards$ 1 | SubAbility$ R1\n" +
		"SVar:R1:DB$ Draw | NumCards$ 0 | ValidTgts$ Player | TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1 | SubAbility$ R2\n" +
		"SVar:R2:DB$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | SubAbility$ R3\n" +
		"SVar:R3:DB$ Draw | NumCards$ 0 | ValidTgts$ Player | TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1\n" +
		"Oracle:x\n"
}

// passPriorityUntilNonPriority answers "pass" to every priority decision until
// a non-priority ask is pending (the same drain the sibling TargetUnique$
// tests inline). It fatals if the stack never reaches an ask.
func passPriorityUntilNonPriority(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 30; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while awaiting an ask")
		}
		if d.Kind != decision.KPriority {
			return d
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
	}
	t.Fatal("no non-priority ask after 30 priority passes")
	return nil
}

// TestTargetUniqueSurvivesADifferentAskKindBetweenRiders pins the row's
// remaining gap: a Dig (ResumeKind "dig") parked between two TargetUnique$
// riders' announced answers must not erase the first rider's pick, and must
// not re-pose a rider's target. Pre-fix the Dig's decision carried no
// accumulator, so the resumed Ctx rebuilt it empty and the second announced
// rider over-offered seat 0.
func TestTargetUniqueSurvivesADifferentAskKindBetweenRiders(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 7008, digBetweenRidersScript())
	addMana(t, e, 0, "C")
	castFirst(t, e, "cast")

	// Both riders are announced at placement (CR 603.3d); the second already
	// excludes the first's pick through the placement accumulator.
	d := passPriorityUntilNonPriority(t, e)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" {
		t.Fatalf("first rider ask = %+v, want the placement KTarget trig_sub", d)
	}
	first := pendingPlayerIDs(t, e)
	if !first[0] || !first[1] {
		t.Fatalf("precondition: first rider should offer BOTH differing players: %+v", d.Options)
	}
	submitChoices(t, e, 0)

	d3 := e.Pending()
	if d3 == nil || d3.Kind != decision.KTarget || d3.ResumeKind != "trig_sub" {
		t.Fatalf("third rider ask = %+v, want the placement KTarget trig_sub", d3)
	}
	third := pendingPlayerIDs(t, e)
	if third[0] {
		t.Fatalf("third rider re-offers seat 0: the placement accumulator dropped the first pick: %+v", d3.Options)
	}
	if !third[1] {
		t.Fatalf("third rider offers no legal different player: %+v", d3.Options)
	}
	submitChoices(t, e, 0)

	// Pass priority so the trigger resolves; the intervening DIG ask (a
	// different ask kind, the suspension under test) surfaces mid-resolution.
	dig := e.Pending()
	for i := 0; i < 30 && dig != nil && dig.Kind == decision.KPriority; i++ {
		for _, o := range dig.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
		dig = e.Pending()
	}
	if dig == nil || dig.Kind != decision.KChoose || dig.ResumeKind != "dig" {
		t.Fatalf("intervening ask = %+v, want the Dig KChoose (resume kind \"dig\")", dig)
	}
	if len(dig.Options) < 2 {
		t.Fatalf("precondition: the Dig must offer a real choice, got %d options: %+v", len(dig.Options), dig.Options)
	}
	submitChoices(t, e, 0)

	// The Dig's remaining window cards offer their own bottom order (the
	// "dig_arrange" KArrange). Answer it in the offered order.
	if arr := e.Pending(); arr != nil && arr.Kind == decision.KArrange {
		submitArrange(t, e, arr, nil)
	}
	// The announced answers survive the Dig: no target is re-posed, and the
	// resolution drains.
	for i := 0; i < 30; i++ {
		dr := e.Pending()
		if dr == nil {
			break
		}
		if dr.Kind == decision.KTarget {
			t.Fatalf("a rider target was re-posed after the Dig: %+v", dr)
		}
		if dr.Kind == decision.KArrange {
			submitArrange(t, e, dr, nil)
			continue
		}
		if err := e.Submit(decision.Intent{Seq: dr.Seq, Player: dr.Player, Choices: tapePick(dr)}); err != nil {
			t.Fatalf("submit %s/%s: %v", dr.Kind, dr.ResumeKind, err)
		}
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not empty after resolution: %d", len(e.G.Stack))
	}
	replayCheck(t, e, cfg)
}

// scryBetweenRidersScript is the same shape with a Scry (the "arrange" ask
// kind the row names) between the riders, so the class is covered for a
// second ask kind than Dig.
func scryBetweenRidersScript() string {
	return "Name:Scry Between Riders\nManaCost:1\nTypes:Creature Bear\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigRoot | TriggerDescription$ x\n" +
		"SVar:TrigRoot:DB$ Draw | NumCards$ 1 | SubAbility$ R1\n" +
		"SVar:R1:DB$ Draw | NumCards$ 0 | ValidTgts$ Player | TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1 | SubAbility$ R2\n" +
		"SVar:R2:DB$ Scry | ScryNum$ 2 | SubAbility$ R3\n" +
		"SVar:R3:DB$ Draw | NumCards$ 0 | ValidTgts$ Player | TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1\n" +
		"Oracle:x\n"
}

// TestTargetUniqueSurvivesAScryBetweenRiders is the arrange-kind twin of the
// Dig test: both riders are announced at placement, and the announced answers
// survive the intervening Scry suspension without a re-pose.
func TestTargetUniqueSurvivesAScryBetweenRiders(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 7009, scryBetweenRidersScript())
	addMana(t, e, 0, "C")
	castFirst(t, e, "cast")

	d := passPriorityUntilNonPriority(t, e)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" {
		t.Fatalf("first rider ask = %+v, want the placement KTarget trig_sub", d)
	}
	if !pendingPlayerIDs(t, e)[0] || !pendingPlayerIDs(t, e)[1] {
		t.Fatalf("precondition: first rider should offer BOTH differing players: %+v", d.Options)
	}
	submitChoices(t, e, 0)

	d3 := e.Pending()
	if d3 == nil || d3.Kind != decision.KTarget || d3.ResumeKind != "trig_sub" {
		t.Fatalf("third rider ask = %+v, want the placement KTarget trig_sub", d3)
	}
	third := pendingPlayerIDs(t, e)
	if third[0] {
		t.Fatalf("third rider re-offers seat 0: the placement accumulator dropped the first pick: %+v", d3.Options)
	}
	if !third[1] {
		t.Fatalf("third rider offers no legal different player: %+v", d3.Options)
	}
	submitChoices(t, e, 0)

	// Pass priority so the trigger resolves; the Scry's KArrange surfaces.
	arrange := e.Pending()
	for i := 0; i < 30 && arrange != nil && arrange.Kind == decision.KPriority; i++ {
		for _, o := range arrange.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
		arrange = e.Pending()
	}
	if arrange == nil || arrange.Kind != decision.KArrange {
		t.Fatalf("intervening ask = %+v, want the Scry KArrange", arrange)
	}
	submitArrange(t, e, arrange, nil)

	// The announced answers survive the Scry: no target is re-posed.
	for i := 0; i < 30; i++ {
		dr := e.Pending()
		if dr == nil {
			break
		}
		if dr.Kind == decision.KTarget {
			t.Fatalf("a rider target was re-posed after the Scry: %+v", dr)
		}
		if dr.Kind == decision.KArrange {
			submitArrange(t, e, dr, nil)
			continue
		}
		if err := e.Submit(decision.Intent{Seq: dr.Seq, Player: dr.Player, Choices: tapePick(dr)}); err != nil {
			t.Fatalf("submit %s/%s: %v", dr.Kind, dr.ResumeKind, err)
		}
	}
	passUntilStackEmpty(t, e, 30)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not empty after resolution: %d", len(e.G.Stack))
	}
	replayCheck(t, e, cfg)
}

// submitArrange answers a KArrange by keeping `top` (option indices) on top in
// the given order; a nil top keeps every offered card on top in the offered
// order. The answer must satisfy the decision's own Min/Max, so nil fills
// every option.
func submitArrange(t *testing.T, e *Engine, d *decision.Decision, top []int) {
	t.Helper()
	if top == nil {
		top = make([]int, len(d.Options))
		for i := range d.Options {
			top[i] = d.Options[i].Index
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: top}); err != nil {
		t.Fatalf("submit arrange: %v", err)
	}
}
