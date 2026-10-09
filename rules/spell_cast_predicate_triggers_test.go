package rules

// The three level-B spell-cast predicate triggers this ticket landed, each
// pinned end to end on REAL corpus carriers (CorpusRegistry, never
// cards.LoadRegistry) at a real cast:
//
//   - Spinerock Tyrant: `ValidSA$ Instant.singleTarget,Sorcery.singleTarget`
//     fires on a one-target instant cast (Shock at the Tyrant), never on a
//     zero-target sorcery (Divination) -- the singleTarget predicate.
//   - Codie, Ravenous Codex: `ValidCard$ Card.prepared` fires on the
//     CR 722.3c prepared-copy cast (the exile copy of the Whiplash
//     Wordsmith prepare spell, the engine's regression carrier), never on a
//     plain cast, and the StackCopy mint of the trigger's own copy carries
//     no FlagPreparedCopy (a copy is put on the stack, never cast --
//     CR 707.10) so it does not re-fire.
//   - Namor the Sub-Mariner: `ValidCard$
//     Card.nonCreature+ManaCostPartialBlue` fires on a blue noncreature
//     spell (Opt), never on a non-blue one (Shock) nor on a blue creature
//     (Faerie Seer) -- both halves of the conjunction are asserted.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// answerPlayerTarget submits the target ask's option that targets player p.
func answerPlayerTarget(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no target ask pending")
	}
	if d.Kind != decision.KTarget {
		t.Fatalf("pending decision is %s, want a target ask: %+v", d.Kind, d)
	}
	for _, o := range d.Options {
		if o.Player == p {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("target ask offers no p%d option: %+v", p, d.Options)
}

// precondition asserts a battlefield placement took (a vacuous setup must
// fail loudly, never pass the assertions silently).
func preconditionBattlefield(t *testing.T, e *Engine, id state.ObjID, name string) {
	t.Helper()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: %s %d is not on the battlefield: %+v", name, id, o)
	}
}

// drainAnsweringCopies drains the stack, answering the optional-trigger ask
// (yes) and a copy's new-target ask (keep the inherited targets), calling
// onCopy at each copy_targets ask so the test can assert on the mint while
// it is on the stack.
func drainAnsweringCopies(t *testing.T, e *Engine, limit int, onCopy func(d *decision.Decision)) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (stack depth %d)", len(e.G.Stack))
		}
		switch {
		case d.Kind == decision.KTarget && d.ResumeKind == "copy_targets":
			if onCopy != nil {
				onCopy(d)
			}
			submitChoices(t, e, 0)
		case d.Kind == decision.KTriggerOptional:
			submitChoices(t, e, 0) // yes, apply the optional trigger
		case d.Kind == decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("non-priority decision %+v while draining the stack", d)
		}
	}
}

func TestSpinerockTyrantFiresOnASingleTargetCastOnly(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tyrant := lookup(t, reg, "Spinerock Tyrant")
	shock := lookup(t, reg, "Shock")
	divination := lookup(t, reg, "Divination")
	e := corpusEngine(t, reg, []*cards.Card{tyrant, shock, divination}, nil)
	tyrantID := moveByName(t, e, 0, "Spinerock Tyrant", state.ZBattlefield)
	preconditionBattlefield(t, e, tyrantID, "Spinerock Tyrant")

	// Negative: Divination is a sorcery with zero targets.
	divID := moveByName(t, e, 0, "Divination", state.ZHand)
	addMana(t, e, 0, "CCU")
	castMode(t, e, divID, "")
	e.priorityRound()
	if stackAbilityForSource(e, tyrantID, "CopySpellAbility") != 0 {
		t.Fatal("zero-target Divination fired Spinerock Tyrant's singleTarget trigger")
	}
	passUntilStackEmpty(t, e, 60)
	if stackAbilityForSource(e, tyrantID, "CopySpellAbility") != 0 {
		t.Fatal("zero-target Divination fired the trigger (flushed at the drain)")
	}

	// Positive: Shock at the Tyrant itself -- exactly one chosen target.
	shockID := moveByName(t, e, 0, "Shock", state.ZHand)
	addMana(t, e, 0, "R")
	castMode(t, e, shockID, "")
	answerPlayerTarget(t, e, 0)
	if o := e.G.Obj(shockID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: the Shock cast never reached the stack: %+v", o)
	}
	if stackAbilityForSource(e, tyrantID, "CopySpellAbility") == 0 {
		t.Fatal("one-target Shock did not fire Spinerock Tyrant's singleTarget trigger")
	}
	// The OptionalDecider$ "may copy?" ask and the copy's own new-target ask
	// are answered by the drain (yes; keep the inherited target).
	drainAnsweringCopies(t, e, 60, nil)
	if stackAbilityForSource(e, tyrantID, "CopySpellAbility") != 0 {
		t.Fatal("the copy trigger is still on the stack after the drain")
	}
}

func TestCodieFiresOnThePreparedCopyCastOnly(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	codie := lookup(t, reg, "Codie, Ravenous Codex")
	whiplash := lookup(t, reg, "Whiplash Wordsmith")
	divination := lookup(t, reg, "Divination")
	e := corpusEngine(t, reg, []*cards.Card{codie, whiplash, divination}, nil)
	codieID := moveByName(t, e, 0, "Codie, Ravenous Codex", state.ZBattlefield)
	whipID := moveByName(t, e, 0, "Whiplash Wordsmith", state.ZBattlefield)
	preconditionBattlefield(t, e, codieID, "Codie")
	// CR 722.3c: the Wordsmith entered prepared, so its exile copy exists.
	var copyID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.IsCopy && o.PreparedSource == whipID {
			copyID = id
		}
	}
	if copyID == 0 {
		t.Fatal("precondition: the Wordsmith's prepared exile copy was never minted")
	}
	if o := e.G.Obj(copyID); o.Face() == nil || o.Face().Name != "Vicious Verse" {
		t.Fatalf("precondition: exile copy %d does not carry the prepare face: %+v", copyID, o)
	}

	// Negative: a plain cast carries no prepared provenance.
	divID := moveByName(t, e, 0, "Divination", state.ZHand)
	addMana(t, e, 0, "CCU")
	castMode(t, e, divID, "")
	e.priorityRound()
	if stackAbilityForSource(e, codieID, "CopySpellAbility") != 0 {
		t.Fatal("a plain cast fired Codie's prepared trigger")
	}
	passUntilStackEmpty(t, e, 60)
	if stackAbilityForSource(e, codieID, "CopySpellAbility") != 0 {
		t.Fatal("a plain cast fired Codie's prepared trigger (flushed at the drain)")
	}

	// Positive: cast the exile copy through the prepared_copy mode.
	addMana(t, e, 0, "B")
	castMode(t, e, copyID, "prepared_copy")
	// The {B/R} hybrid pip paid with {B} poses the pay-time choice first.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		submitChoices(t, e, 0)
	}
	answerPlayerTarget(t, e, 1)
	if o := e.G.Obj(copyID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: the prepared copy never reached the stack: %+v", o)
	}
	if o := e.G.Obj(copyID); o.CastFlags&state.FlagPreparedCopy == 0 {
		t.Fatal("precondition: the prepared-copy cast emitted no FlagPreparedCopy provenance")
	}
	if stackAbilityForSource(e, codieID, "CopySpellAbility") == 0 {
		t.Fatal("the prepared-copy cast did not fire Codie's trigger")
	}

	// Resolve the trigger: it mints a StackCopy of the prepared copy. That
	// mint is PUT on the stack, never cast (CR 707.10), so it carries no
	// FlagPreparedCopy and must not re-fire the trigger.
	mintChecked := false
	drainAnsweringCopies(t, e, 80, func(d *decision.Decision) {
		var mint state.ObjID
		for _, id := range e.G.Stack {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Vicious Verse" && id != copyID {
				mint = id
			}
		}
		if mint == 0 {
			t.Fatal("the trigger's copy mint is not on the stack at its target ask")
		}
		if o := e.G.Obj(mint); o.CastFlags&state.FlagPreparedCopy != 0 {
			t.Fatal("the StackCopy mint inherited FlagPreparedCopy (CR 707.10)")
		}
		mintChecked = true
	})
	if !mintChecked {
		t.Fatal("the trigger's copy never asked its new-target ask (the drain saw no mint)")
	}
	if stackAbilityForSource(e, codieID, "CopySpellAbility") != 0 {
		t.Fatal("the copied spell re-fired Codie's trigger")
	}
}

func TestNamorFiresOnABlueNoncreatureSpellOnly(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	namor := lookup(t, reg, "Namor the Sub-Mariner")
	opt := lookup(t, reg, "Opt")
	shock := lookup(t, reg, "Shock")
	seer := lookup(t, reg, "Faerie Seer")
	e := corpusEngine(t, reg, []*cards.Card{namor, opt, shock, seer}, nil)
	namorID := moveByName(t, e, 0, "Namor the Sub-Mariner", state.ZBattlefield)
	preconditionBattlefield(t, e, namorID, "Namor")

	// Negative: Shock ({R}, noncreature, no blue pip).
	shockID := moveByName(t, e, 0, "Shock", state.ZHand)
	addMana(t, e, 0, "R")
	castMode(t, e, shockID, "")
	answerPlayerTarget(t, e, 1)
	e.priorityRound()
	if stackAbilityForSource(e, namorID, "Token") != 0 {
		t.Fatal("a non-blue spell fired Namor's trigger")
	}
	passUntilStackEmpty(t, e, 60)

	// Negative: Faerie Seer ({1}{U}, blue CREATURE) fails the nonCreature half.
	seerID := moveByName(t, e, 0, "Faerie Seer", state.ZHand)
	addMana(t, e, 0, "CU")
	castMode(t, e, seerID, "")
	e.priorityRound()
	if stackAbilityForSource(e, namorID, "Token") != 0 {
		t.Fatal("a blue creature spell fired Namor's nonCreature trigger")
	}

	// Positive: Opt ({U}, noncreature, blue).
	optID := moveByName(t, e, 0, "Opt", state.ZHand)
	addMana(t, e, 0, "U")
	castMode(t, e, optID, "")
	e.priorityRound()
	if o := e.G.Obj(optID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: the Opt cast never reached the stack: %+v", o)
	}
	if stackAbilityForSource(e, namorID, "Token") == 0 {
		t.Fatal("a blue noncreature spell did not fire Namor's trigger")
	}
}
