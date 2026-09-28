package rules

// The resolved-zero half of the dynamic TargetMin$/TargetMax$ bound task
// (agent-20260918T233200Z-e0817443). The X/Y resolver itself landed in
// b3786f11 and is pinned by rules/targetmax_x_test.go; what remained is the
// "instead" idiom, where BOTH bounds resolve to 0 and the engine's
// post-resolution max >= 1 clamp asked for a target the spell must not take.
// Tear Asunder writes exactly that: kicked, its main SA is
// TargetMin$ X | TargetMax$ X over SVar:X:Count$Kicked.0.1 (X = 0) and its
// chained sub is TargetMin$ Y | TargetMax$ Y over SVar:Y:Count$Kicked.1.0
// (Y = 1).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const resolvedZeroTearSrc = "Name:Tear Asunder\nManaCost:1 G\nTypes:Instant\n" +
	"K:Kicker:1 B\n" +
	"A:SP$ ChangeZone | Origin$ Battlefield | Destination$ Exile | TgtPrompt$ Select target artifact or enchantment | ValidTgts$ Artifact,Enchantment | TargetMin$ X | TargetMax$ X | SubAbility$ DBChangeZone | SpellDescription$ Exile target artifact or enchantment. If this spell was kicked, exile target nonland permanent instead.\n" +
	"SVar:DBChangeZone:DB$ ChangeZone | TargetMin$ Y | TargetMax$ Y | Origin$ Battlefield | Destination$ Exile | ValidTgts$ Permanent.nonLand | TgtPrompt$ Select target nonland permanent | Condition$ Kicked\n" +
	"SVar:X:Count$Kicked.0.1\n" +
	"SVar:Y:Count$Kicked.1.0\n" +
	"Oracle:x\n"

const resolvedZeroPestSrc = "Name:Pest Infestation\nManaCost:X X G\nTypes:Sorcery\n" +
	"A:SP$ Destroy | TargetMin$ 0 | TargetMax$ X | ValidTgts$ Artifact,Enchantment | TgtPrompt$ Select up to X target artifacts and/or enchantments | SpellDescription$ Destroy up to X target artifacts and/or enchantments.\n" +
	"SVar:X:Count$xPaid\n" +
	"Oracle:x\n"

const resolvedZeroArtifactSrc = "Name:Tin Can\nManaCost:1\nTypes:Artifact Creature Golem\nPT:2/2\nOracle:x\n"

// resolvedZeroTriggerSrc is an ETB trigger whose placement ask is bounded by a
// dynamic pair that resolves to 0 while a legal candidate exists: X counts the
// zero creatures you control, and the trigger targets an artifact. This is the
// askTarget (rules/stack.go) sibling of the cast-time ask.
const resolvedZeroTriggerSrc = "Name:Zero Trigger\nManaCost:1\nTypes:Enchantment\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ Trig | TriggerDescription$ When this enters, destroy up to X target artifacts.\n" +
	"SVar:Trig:DB$ Destroy | TargetMin$ X | TargetMax$ X | ValidTgts$ Artifact | SVar:X:Count$Valid Creature.YouCtrl\n" +
	"Oracle:x\n"

const resolvedZeroGearSrc = "Name:Spare Gear\nManaCost:1\nTypes:Artifact\nOracle:x\n"

// TestTriggerPlacementAskResolvedZeroPosesNothing pins the askTarget sibling:
// an ETB trigger whose TargetMin$ X | TargetMax$ X resolves to 0 must pose no
// placement ask. Without the max == 0 arm the engine panics ("decision target
// ... posed with only the empty answer legal (Min 0 Max 0, 1 options)").
func TestTriggerPlacementAskResolvedZeroPosesNothing(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 9204, resolvedZeroTriggerSrc, resolvedZeroGearSrc)
	// A pure artifact (not a creature) so the count is 0 but a target exists.
	gear := putCreature(t, e, 0, resolvedZeroGearSrc)
	if gear == 0 || e.IsCreature(gear) {
		t.Fatalf("precondition: want a non-creature artifact on the battlefield")
	}
	trigger := putCreature(t, e, 0, resolvedZeroTriggerSrc)
	if trigger == 0 {
		t.Fatal("precondition: trigger enchantment not placed")
	}
	if n := countType(t, e, 0); n != 0 {
		t.Fatalf("precondition: want 0 creatures you control (X=0), got %d", n)
	}
	// Drive the ETB trigger; the resolved-zero placement ask must pose
	// nothing, so the engine must not panic and must reach a priority ask.
	e.Advance()
	passAll(t, e, 12)
	replayCheck(t, e, cfg)
}

// countType returns how many of p's battlefield permanents are creatures.
func countType(t *testing.T, e *Engine, p state.PlayerID) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if e.IsCreature(id) {
			n++
		}
	}
	return n
}

// TestResolvedTargetBoundsResolvedZeroIsHonoured pins the resolver contract
// this ticket changes: when BOTH TargetMin$ X and TargetMax$ X are dynamic and
// resolve to 0, the pair is honoured as written (0, 0), not clamped up to
// Max 1. This is the shared input to targetAsk, subTargetAsk AND askTarget, so
// it is the class fix's unit pin. A bare TargetMax$ X (no TargetMin) that
// resolves to 0 must still clamp up to the default Min 1 -- pinned by
// stack_test.go's "bare X zero clamps back to one" case.
func TestResolvedTargetBoundsResolvedZeroIsHonoured(t *testing.T) {
	t.Parallel()
	zeroPair := card(t, "Name:Bound Zero Pair\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ Draw | Defined$ You | NumCards$ 1 | ValidTgts$ Creature | TargetMin$ X | TargetMax$ X\n"+
		"SVar:X:Count$Valid Creature.YouCtrl\nOracle:x\n")
	e := handEngine(t, zeroPair)
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Face() == nil {
		t.Fatalf("precondition: card %d has no face", id)
	}
	// No creatures on the battlefield, so Count$Valid Creature.YouCtrl = 0.
	if n := len(e.G.Zone(state.ZBattlefield, 0)); n != 0 {
		t.Fatalf("precondition: want an empty battlefield, got %d permanents", n)
	}
	sa := e.G.Obj(id).Face().SpellAbility()
	min, max := e.resolvedTargetBounds(0, id, sa, 0)
	if min != 0 || max != 0 {
		t.Fatalf("resolvedTargetBounds = (%d, %d), want (0, 0) for a resolved-zero dynamic pair", min, max)
	}
}

// TestTearAsunderKickedTakesOnlyTheSubTarget asserts the kicked "instead"
// idiom: the main SA's X resolves to 0 so NO cast-time artifact/enchantment
// target is asked, and only the chained sub's single nonland permanent is
// exiled. Before the resolved-zero fix the main SA still demanded one, so the
// kicked spell exiled two permanents.
func TestTearAsunderKickedTakesOnlyTheSubTarget(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 9201, resolvedZeroTearSrc, resolvedZeroArtifactSrc, testBearSrc)
	artifact := putCreature(t, e, 0, resolvedZeroArtifactSrc)
	// A second, non-artifact nonland permanent for the sub to take, so the
	// two clauses' effects are attributable by object.
	bear := putCreature(t, e, 0, testBearSrc)
	if artifact == 0 || bear == 0 {
		t.Fatal("precondition: fixture permanents not placed")
	}
	addMana(t, e, 0, "GGBB")

	kickIdx := -1
	for _, o := range castOptions(t, e) {
		if o.Mode == "kicked" {
			kickIdx = o.Index
		}
	}
	if kickIdx < 0 {
		t.Fatalf("precondition: no kicked cast option offered")
	}
	submitChoices(t, e, kickIdx)

	// The kicked main SA must demand ZERO targets: no cast-time KTarget ask.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("kicked main SA asked for %d target(s) (max=%d); want none", d.Min, d.Max)
	}

	// Drive to resolution: the sub's target ask appears as a mid-resolution
	// picker for exactly one nonland permanent. passAll stops at it.
	passAll(t, e, 12)
	sub := e.Pending()
	if sub == nil || sub.Kind == decision.KPriority {
		t.Fatal("precondition: the kicked sub never asked for its nonland-permanent target")
	}
	if sub.Prompt != "Select target nonland permanent" {
		t.Fatalf("sub ask prompt = %q, want the nonland-permanent picker", sub.Prompt)
	}
	if sub.Min != 1 || sub.Max != 1 {
		t.Fatalf("kicked sub target bounds min=%d max=%d, want 1/1", sub.Min, sub.Max)
	}
	pickIdx, pick := -1, state.ObjID(0)
	for _, o := range sub.Options {
		if o.Obj == bear {
			pickIdx, pick = o.Index, o.Obj
		}
	}
	if pickIdx < 0 {
		t.Fatalf("precondition: sub ask does not offer the nonland permanent %d: %+v", bear, sub.Options)
	}
	submitChoices(t, e, pickIdx)
	passAll(t, e, 12)

	if o := e.G.Obj(pick); o == nil || o.Zone != state.ZExile {
		t.Fatalf("chosen nonland permanent %d not exiled: %+v", pick, o)
	}
	// Exactly one exile: the pre-fix bug exiled the artifact AND the sub's
	// pick because the main SA still demanded a target.
	if n := len(e.G.Zone(state.ZExile, 0)); n != 1 {
		t.Fatalf("exile zone has %d cards, want exactly 1", n)
	}
	// The artifact was NOT taken by the kicked main clause.
	if o := e.G.Obj(artifact); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("artifact %d must remain (kicked main SA targets nothing): %+v", artifact, o)
	}
	replayCheck(t, e, cfg)
}

// TestTearAsunderUnkickedStillTargetsArtifact is the control: unkicked, X
// resolves to 1, so the main SA demands exactly one artifact/enchantment and
// the sub (Condition$ Kicked) is skipped. This pins that the resolved-zero
// path did not widen into the ordinary case.
func TestTearAsunderUnkickedStillTargetsArtifact(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 9202, resolvedZeroTearSrc, resolvedZeroArtifactSrc, testBearSrc)
	artifact := putCreature(t, e, 0, resolvedZeroArtifactSrc)
	putCreature(t, e, 0, testBearSrc)
	if artifact == 0 {
		t.Fatal("precondition: fixture artifact not placed")
	}
	addMana(t, e, 0, "GG")

	plainIdx := -1
	for _, o := range castOptions(t, e) {
		if o.Mode == "" && o.Kind == "cast" {
			plainIdx = o.Index
		}
	}
	if plainIdx < 0 {
		t.Fatalf("precondition: no plain cast option offered")
	}
	submitChoices(t, e, plainIdx)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("unkicked main SA must ask for its artifact/enchantment target, got %+v", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("unkicked main SA bounds min=%d max=%d, want 1/1", d.Min, d.Max)
	}
	submitChoices(t, e, d.Options[0].Index)
	passAll(t, e, 12)
	if n := len(e.G.Zone(state.ZExile, 0)); n != 1 {
		t.Fatalf("unkicked spell exiled %d cards, want exactly 1", n)
	}
	if o := e.G.Obj(artifact); o == nil || o.Zone != state.ZExile {
		t.Fatalf("unkicked main SA did not exile the artifact: %+v", o)
	}
	replayCheck(t, e, cfg)
}

// TestPestInfestationZeroXAsksNothing asserts the other resolved-zero shape:
// Pest Infestation's TargetMin$ 0 | TargetMax$ X with X paid as 0 ("destroy up
// to 0") poses no target ask at all, rather than the old clamp's up-to-1 ask.
func TestPestInfestationZeroXAsksNothing(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 9203, resolvedZeroPestSrc, resolvedZeroArtifactSrc)
	putCreature(t, e, 0, resolvedZeroArtifactSrc)
	addMana(t, e, 0, "G") // XXG with X = 0 costs one green.

	pestIdx := -1
	for _, o := range castOptions(t, e) {
		if obj := e.G.Obj(o.Obj); obj != nil && obj.Face() != nil && obj.Face().Name == "Pest Infestation" {
			pestIdx = o.Index
		}
	}
	if pestIdx < 0 {
		t.Fatalf("precondition: Pest Infestation not castable")
	}
	submitChoices(t, e, pestIdx)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("precondition: expected the X announcement ask, got %+v", d)
	}
	x0 := -1
	for _, o := range d.Options {
		if o.Label == "X = 0" {
			x0 = o.Index
		}
	}
	if x0 < 0 {
		t.Fatalf("precondition: X = 0 not offered: %+v", d.Options)
	}
	submitChoices(t, e, x0)

	// No target ask: the next decision is not a KTarget.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("Pest Infestation with X=0 asked for %d target(s) (max=%d); want none", d.Min, d.Max)
	}
	passAll(t, e, 12)
	replayCheck(t, e, cfg)
}
