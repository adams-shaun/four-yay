package rules

// api:Clone (DB$ Clone) end-to-end pins on real corpus cards. Mirage Mirror
// is the standalone carrier: "{2}: Mirage Mirror becomes a copy of target
// artifact, creature, enchantment, or land until end of turn." Its AB$ Clone
// is the plainest measured shape (ValidTgts$ only, no Defined$/Choices$), so
// the SA's own target is the copy source and the default become operand is
// Self. The copy is a layer-1 basis (state.Object.CopyFace) plus duration
// bookkeeping, so the assertions read the same Derived surface the rest of
// the engine does.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// cloneTargetOption returns the target-decision option index for obj, or -1.
func cloneTargetOption(t *testing.T, e *Engine, obj state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending for clone target ask")
	}
	if d.Kind != decision.KTarget {
		t.Fatalf("pending kind %v, want KTarget", d.Kind)
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	return -1
}

// activateCloneAbility finds Mirage Mirror's ability option, submits it, and
// answers the target ask with bear. It leaves the ability on the stack for
// the caller to resolve.
func activateCloneAbility(t *testing.T, e *Engine, mirror, bear state.ObjID) {
	t.Helper()
	opt := abilityOption(t, e, mirror, 0)
	submitChoices(t, e, opt.Index)
	idx := cloneTargetOption(t, e, bear)
	if idx < 0 {
		t.Fatalf("clone target ask offers no option for bear %d: %+v", bear, e.Pending())
	}
	submitChoices(t, e, idx)
}

// TestMirageMirrorBecomesACopyOfTargetCreature is the standalone api:Clone
// pin: Mirage Mirror's activated ability turns the artifact into a copy of a
// target creature -- name, card type, creature subtype, colour and P/T all
// come from the copied face -- and end-of-turn cleanup reverts it to the
// printed 0/0 artifact.
func TestMirageMirrorBecomesACopyOfTargetCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Mirage Mirror", "Grizzly Bears")

	mirror := searchMoveByName(t, e, "Mirage Mirror", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	// The bear is seat 0's; Mirage Mirror is seat 0's artifact.
	if o := e.G.Obj(mirror); o == nil || o.Face().Name != "Mirage Mirror" {
		t.Fatalf("mirror fixture missing: %+v", o)
	}
	// A creature the artifact is NOT, before the copy.
	if e.IsCreature(mirror) {
		t.Fatal("Mirage Mirror is a creature before the copy")
	}

	addMana(t, e, 0, "CC")
	activateCloneAbility(t, e, mirror, bear)
	passUntilStackEmpty(t, e, 40)

	// The copy: name, type line, colour and P/T all from Grizzly Bears.
	f := e.G.Obj(mirror).Face()
	if f == nil || f.Name != "Grizzly Bears" {
		t.Fatalf("copy name %v, want Grizzly Bears", f)
	}
	if !e.IsCreature(mirror) {
		t.Fatal("copy is not a creature")
	}
	d := e.Derived(mirror)
	if d.Power != 2 || d.Toughness != 2 {
		t.Fatalf("copy P/T %d/%d, want 2/2", d.Power, d.Toughness)
	}
	if d.Colors != "G" {
		t.Fatalf("copy colours %q, want G", d.Colors)
	}
	var bear2 bool
	for _, typ := range d.Types {
		if typ == "Bear" {
			bear2 = true
		}
	}
	if !bear2 {
		t.Fatalf("copy types %v, want the Bear subtype", d.Types)
	}
	// The ability that made the copy is gone from the copied face's text (the
	// copy has Grizzly Bears' vanilla rules text, no Clone ability).
	replayCheck(t, e, cfg)

	// End the turn: the cleanup step (StepCleanup -> priorityRound's
	// cleanupStep -> cleanupBody -> EndOfTurnCleanup) drops the
	// UntilEndOfTurn copy and the artifact reverts to its printed face.
	e.pending = nil
	e.setStep(state.StepCleanup)
	e.priorityRound()
	if o := e.G.Obj(mirror); o.Face() == nil || o.Face().Name != "Mirage Mirror" {
		t.Fatalf("after cleanup the mirror is %v, want Mirage Mirror", o.Face())
	}
	if e.IsCreature(mirror) {
		t.Fatal("the reverted artifact is still a creature")
	}
}

// TestCloneModifiersApplyAtTheirOwnLayers pins the modifier half: a clone
// whose AddTypes$/SetColor$/AddKeywords$/SetPower$/SetToughness$ parameters
// must show them, and the modifier effects must expire with the copy. It uses
// a synthetic-but-real-script-shape fixture (an inline Forge script, never a
// .cards/ file) so every modifier path is exercised deterministically.
func TestCloneModifiersApplyAtTheirOwnLayers(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Mimic\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilEndOfTurn | AddTypes$ Shapeshifter & Rogue | SetColor$ Blue | AddKeywords$ Flying | SetPower$ 4 | SetToughness$ 5 | SpellDescription$ becomes a copy.\n" +
		"Oracle:x\n"
	const bruiser = "Name:Fixture Bruiser\nManaCost:2 G\nTypes:Creature Beast\nPT:3/3\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 88, src, bruiser)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	bear := moveSeeded(t, e, 0, bruiser, state.ZBattlefield)
	addMana(t, e, 0, "C")
	activateCloneAbility(t, e, id, bear)
	passUntilStackEmpty(t, e, 40)

	// IntoPlayTapped$ is deliberately NOT exercised here: it is an
	// ETB-route-only parameter (see
	// TestCloneIntoPlayTappedIsUnreadOnTheStandaloneRoute).
	d := e.Derived(id)
	if d.Power != 4 || d.Toughness != 5 {
		t.Fatalf("modified copy P/T %d/%d, want 4/5", d.Power, d.Toughness)
	}
	if d.Colors != "U" {
		t.Fatalf("modified copy colours %q, want U", d.Colors)
	}
	if !e.HasKeyword(id, "Flying") {
		t.Fatalf("modified copy keywords %v, want Flying", d.Keywords)
	}
	var shapeshifter, rogue bool
	for _, typ := range d.Types {
		if typ == "Shapeshifter" {
			shapeshifter = true
		}
		if typ == "Rogue" {
			rogue = true
		}
	}
	if !shapeshifter || !rogue {
		t.Fatalf("modified copy types %v, want Shapeshifter and Rogue added", d.Types)
	}
	replayCheck(t, e, cfg)
}

// TestCloneNewNameAndGainThisAbility pins the NewName$ and GainThisAbility$
// riders: the copy's displayed name is the NewName$, and with
// GainThisAbility$ True the original object's own abilities survive the copy
// (so the copy can clone again).
func TestCloneNewNameAndGainThisAbility(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Doppel\nManaCost:2 U\nTypes:Creature Shapeshifter\nPT:2/2\n" +
		"A:AB$ Clone | Cost$ 2 U | ValidTgts$ Creature | TgtPrompt$ Choose target creature | NewName$ Fixture Doppel | GainThisAbility$ True | Duration$ UntilEndOfTurn | SpellDescription$ becomes a copy, except its name is Fixture Doppel.\n" +
		"Oracle:x\n"
	const oxSrc = "Name:Fixture Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/3\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 91, src, oxSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, oxSrc, state.ZBattlefield)
	addMana(t, e, 0, "CCUU")
	activateCloneAbility(t, e, id, ox)
	passUntilStackEmpty(t, e, 40)

	f := e.G.Obj(id).Face()
	if f == nil || f.Name != "Fixture Doppel" {
		t.Fatalf("copy name %v, want Fixture Doppel", f)
	}
	// GainThisAbility$ True: the clone ability survives the copy, so the
	// permanent is offered its own AB$ Clone again. Re-fund the cost: the
	// first activation's payment left the pool short, and the offer loop's
	// totality gate correctly withholds an unpayable ability.
	addMana(t, e, 0, "CCUU")
	e.pending = nil
	e.priorityRound()
	if _, ok := findAbilityOption(e, id, 0); !ok {
		t.Fatalf("GainThisAbility$ True copy lost its clone ability: %+v", e.Pending())
	}
	replayCheck(t, e, cfg)
}

// TestPermanentCloneEndsWhenTheBecomeObjectLeaves is the CR 400.7 lifetime
// pin: a no-Duration$ ("permanent") clone with an AddKeywords$ modifier stops
// being a copy AND loses the modifier the moment the become object leaves the
// battlefield, and the whole clone unit is removed from e.continuous rather
// than lingering -- the modifier-leak class where a re-entered object used to
// still carry the cloned Flying.
func TestPermanentCloneEndsWhenTheBecomeObjectLeaves(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Mimic\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | AddKeywords$ Flying | SpellDescription$ becomes a permanent copy.\n" +
		"Oracle:x\n"
	const bruiser = "Name:Fixture Bruiser\nManaCost:2 G\nTypes:Creature Beast\nPT:3/3\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 89, src, bruiser)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	bear := moveSeeded(t, e, 0, bruiser, state.ZBattlefield)
	addMana(t, e, 0, "C")
	activateCloneAbility(t, e, id, bear)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Bruiser" {
		t.Fatalf("permanent copy name %v, want Fixture Bruiser", o.Face())
	}
	if !e.HasKeyword(id, "Flying") {
		t.Fatalf("permanent copy keywords %v, want Flying", e.Derived(id).Keywords)
	}
	replayCheck(t, e, cfg)

	// CR 400.7: leaving the battlefield makes it a new object, so the copy and
	// its modifier both end, and the clone unit leaves e.continuous.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(id); o.CopyFace != nil {
		t.Fatal("copy basis survived leaving the battlefield")
	}
	for _, ce := range e.continuous {
		if ce.CloneTarget == id {
			t.Fatalf("clone effect lingered in e.continuous after the object left: %+v", ce)
		}
	}
	// Re-entering must be the printed face, not a copy still carrying Flying.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZBattlefield})
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Mimic" {
		t.Fatalf("re-entered object is %v, want Fixture Mimic", o.Face())
	}
	if e.HasKeyword(id, "Flying") {
		t.Fatalf("re-entered object kept the cloned modifier: %v", e.Derived(id).Keywords)
	}
	replayCheck(t, e, cfg)
}

// TestCloneUntilNextEndStepExpiresAtCleanup pins the duration that used to
// become a PERMANENT copy: cloneDuration left every lifetime field zero for
// UntilNextEndStep, so neither active() nor EndOfTurnCleanup ever dropped it.
// The carrier is niko_light_of_hope.
func TestCloneUntilNextEndStepExpiresAtCleanup(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Stepclone\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilNextEndStep | SpellDescription$ becomes a copy until your next end step.\n" +
		"Oracle:x\n"
	const oxSrc = "Name:Fixture Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/3\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 90, src, oxSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, oxSrc, state.ZBattlefield)
	addMana(t, e, 0, "C")
	activateCloneAbility(t, e, id, ox)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Ox" {
		t.Fatalf("UntilNextEndStep copy name %v, want Fixture Ox", o.Face())
	}
	replayCheck(t, e, cfg)

	e.pending = nil
	e.setStep(state.StepCleanup)
	e.priorityRound()
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Stepclone" {
		t.Fatalf("after cleanup the object is %v, want Fixture Stepclone", o.Face())
	}
}

// TestCloneUntilYourNextTurnSurvivesCleanupThenExpires pins the non-EOT
// turn-boundary duration: the copy survives the turn it was made in (it is
// NOT UntilEOT) and expires only at the controller's next turn's cleanup, via
// the UntilTurn boundary AddContinuous computes.
func TestCloneUntilYourNextTurnSurvivesCleanupThenExpires(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Turnclone\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilYourNextTurn | AddKeywords$ Flying | SpellDescription$ becomes a copy until your next turn.\n" +
		"Oracle:x\n"
	const oxSrc = "Name:Fixture Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/3\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 92, src, oxSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, oxSrc, state.ZBattlefield)
	addMana(t, e, 0, "C")
	activateCloneAbility(t, e, id, ox)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Ox" {
		t.Fatalf("UntilYourNextTurn copy name %v, want Fixture Ox", o.Face())
	}
	replayCheck(t, e, cfg)

	// It is not UntilEOT: this turn's cleanup keeps it.
	e.pending = nil
	e.setStep(state.StepCleanup)
	e.priorityRound()
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Ox" {
		t.Fatalf("UntilYourNextTurn copy expired a turn early at this cleanup: %v", o.Face())
	}
	// It expires at the controller's (seat 0's) next turn's cleanup.
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 2})
	e.EndOfTurnCleanup()
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	e.EndOfTurnCleanup()
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Turnclone" {
		t.Fatalf("UntilYourNextTurn copy survived the controller's next turn: %v", o.Face())
	}
}
