package rules

// Overlapping api:Clone units on ONE permanent, plus the two parameter
// remainders the api:Clone review named (SetColor$ Colorless, IntoPlayTapped$
// on the standalone route) and the graveyard-zone copy source.
//
// The defect these pin: state.Object.CopyFace is a SINGLE basis field while
// effClone registers one clone UNIT per activation, so an expiry that cleared
// the field outright destroyed a still-live unit's copy, and a sibling drop
// keyed on the become object alone took the survivor's modifier effects with
// it.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// twoCloneAbilities is a fixture with two independent AB$ Clone abilities so
// one permanent can carry two live clone units at once: ability 0 is a
// PERMANENT copy (no Duration$) that also grants Flying, ability 1 is an
// UntilEndOfTurn copy. Both are real Forge script shapes; the corpus has no
// single card with two, which is exactly why the overlap went unpinned.
const twoCloneAbilities = "Name:Fixture Twinmimic\nManaCost:2\nTypes:Artifact\n" +
	"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | AddKeywords$ Flying | GainThisAbility$ True | SpellDescription$ becomes a permanent copy.\n" +
	"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilEndOfTurn | GainThisAbility$ True | SpellDescription$ becomes a copy until end of turn.\n" +
	"Oracle:x\n"

const cloneOxSrc = "Name:Fixture Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/3\nOracle:x\n"
const cloneBruiserSrc = "Name:Fixture Bruiser\nManaCost:2 G\nTypes:Creature Beast\nPT:3/3\nOracle:x\n"

// activateCloneAbilityIdx is activateCloneAbility for a card with more than
// one AB$ Clone: it submits ability index idx and answers its target ask.
func activateCloneAbilityIdx(t *testing.T, e *Engine, mimic, target state.ObjID, idx int) {
	t.Helper()
	// Re-pose priority: a second activation in the same turn is offered only
	// once the pool has been re-funded and the decision rebuilt (the offer
	// loop withholds an unpayable ability).
	e.pending = nil
	e.priorityRound()
	opt := abilityOption(t, e, mimic, idx)
	submitChoices(t, e, opt.Index)
	i := cloneTargetOption(t, e, target)
	if i < 0 {
		t.Fatalf("clone target ask offers no option for %d: %+v", target, e.Pending())
	}
	submitChoices(t, e, i)
	passUntilStackEmpty(t, e, 40)
}

// TestOverlappingClonesTemporaryExpiryKeepsThePermanentCopy is the MAJOR
// regression: a PERMANENT clone made first and an UntilEndOfTurn clone made
// second are two units sharing one CopyFace basis. When the temporary one
// expires at cleanup the object must fall BACK to the permanent copy (CR
// 613.1a: the highest-timestamp surviving copy effect applies), not revert to
// its printed face -- and the permanent unit's own AddKeywords$ modifier must
// survive the sibling drop.
//
// Against the pre-fix build this fails twice over: EndOfTurnCleanup emitted
// ClonePermanent{Obj: id} with no IDs (an unconditional CopyFace clear) and
// dropped every effect whose CloneTarget was the object, modifiers included.
func TestOverlappingClonesTemporaryExpiryKeepsThePermanentCopy(t *testing.T) {
	t.Parallel()
	// The first copy keeps only its resolving ability, not Twinmimic's
	// second ability. The copied Ox supplies the temporary Clone instead.
	const oxWithClone = "Name:Fixture Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/3\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilEndOfTurn | SpellDescription$ becomes a copy until end of turn.\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 141, twoCloneAbilities, oxWithClone, cloneBruiserSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, oxWithClone, state.ZBattlefield)
	bruiser := moveSeeded(t, e, 0, cloneBruiserSrc, state.ZBattlefield)

	addMana(t, e, 0, "C")
	activateCloneAbilityIdx(t, e, id, ox, 0) // permanent copy of the Ox, +Flying
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Ox" {
		t.Fatalf("permanent copy name %v, want Fixture Ox", o.Face())
	}
	addMana(t, e, 0, "C")
	activateCloneAbilityIdx(t, e, id, bruiser, 0) // copied Ox's temporary Clone
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Bruiser" {
		t.Fatalf("temporary copy name %v, want Fixture Bruiser", o.Face())
	}
	replayCheck(t, e, cfg)

	e.pending = nil
	e.setStep(state.StepCleanup)
	e.priorityRound()

	// The temporary unit is gone; the permanent one is not.
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Ox" {
		t.Fatalf("after cleanup the object is %v, want the still-live permanent copy Fixture Ox", o.Face())
	}
	if !e.HasKeyword(id, "Flying") {
		t.Fatalf("the permanent unit's AddKeywords$ modifier was dropped with the temporary unit: %v",
			e.Derived(id).Keywords)
	}
	if d := e.Derived(id); d.Power != 2 || d.Toughness != 3 {
		t.Fatalf("after cleanup P/T %d/%d, want the Ox's 2/3", d.Power, d.Toughness)
	}
	// The permanent unit's marker and modifier are still registered.
	var markers, mods int
	for _, ce := range e.continuous {
		if ce.CloneTarget != id {
			continue
		}
		if ce.Layer == LCopy {
			markers++
		} else {
			mods++
		}
	}
	if markers != 1 || mods != 1 {
		t.Fatalf("after cleanup %d markers / %d modifiers for the object, want 1/1", markers, mods)
	}
	replayCheck(t, e, cfg)
}

// TestOverlappingClonesShortExpiryKeepsTheLongerCopy is the same defect with
// both units temporary: an UntilEndOfTurn copy made first and an
// UntilYourNextTurn copy made second. This turn's cleanup drops only the
// first, and the longer copy must still be in force afterwards.
func TestOverlappingClonesShortExpiryKeepsTheLongerCopy(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Twostep\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilEndOfTurn | GainThisAbility$ True | SpellDescription$ becomes a copy until end of turn.\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilYourNextTurn | SpellDescription$ becomes a copy until your next turn.\n" +
		"Oracle:x\n"
	const oxWithClone = "Name:Fixture Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/3\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilYourNextTurn | SpellDescription$ becomes a copy until your next turn.\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 142, src, oxWithClone, cloneBruiserSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, oxWithClone, state.ZBattlefield)
	bruiser := moveSeeded(t, e, 0, cloneBruiserSrc, state.ZBattlefield)

	addMana(t, e, 0, "C")
	activateCloneAbilityIdx(t, e, id, ox, 0) // until end of turn
	addMana(t, e, 0, "C")
	activateCloneAbilityIdx(t, e, id, bruiser, 0) // copied Ox's next-turn Clone
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Bruiser" {
		t.Fatalf("second copy name %v, want Fixture Bruiser", o.Face())
	}
	replayCheck(t, e, cfg)

	e.pending = nil
	e.setStep(state.StepCleanup)
	e.priorityRound()
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Bruiser" {
		t.Fatalf("after cleanup the object is %v, want the still-live UntilYourNextTurn copy Fixture Bruiser",
			o.Face())
	}
	// And the longer one really does expire on its own boundary afterwards.
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 2})
	e.EndOfTurnCleanup()
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	e.EndOfTurnCleanup()
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Twostep" {
		t.Fatalf("the UntilYourNextTurn copy survived its own boundary: %v", o.Face())
	}
	for _, ce := range e.continuous {
		if ce.CloneTarget == id {
			t.Fatalf("clone effect lingered after every unit expired: %+v", ce)
		}
	}
}

// TestCloneSetColorColorlessOverwritesToColourless pins the SetColor$
// remainder: colorLetters("Colorless") parses to an EMPTY letter slice with
// ok true, so registering the layer-5 overwrite on len(setColors) > 0 made
// "becomes a copy ... except it's colorless" a silent no-op. The copy must
// come out colourless, not keep the copied face's green.
func TestCloneSetColorColorlessOverwritesToColourless(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Devoidmimic\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilEndOfTurn | SetColor$ Colorless | SpellDescription$ becomes a colorless copy.\n" +
		"Oracle:x\n"
	e, cfg, id := newFixtureDeck(t, 143, src, cloneOxSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, cloneOxSrc, state.ZBattlefield)
	addMana(t, e, 0, "C")
	activateCloneAbilityIdx(t, e, id, ox, 0)

	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Ox" {
		t.Fatalf("copy name %v, want Fixture Ox", o.Face())
	}
	if c := e.Derived(id).Colors; c != "" {
		t.Fatalf("SetColor$ Colorless copy colours %q, want colourless (the Ox is green)", c)
	}
	replayCheck(t, e, cfg)
}

// TestCloneIntoPlayTappedIsUnreadOnTheStandaloneRoute pins the parameter's
// scope: IntoPlayTapped$ means "the copy ENTERS tapped", which only has a
// referent on the ETB-replacement route. On the standalone route the become
// object is already on the battlefield, so tapping it invents a cost the card
// never charges. It must be recorded as unread (the loud combined Note) and
// leave the permanent untapped.
func TestCloneIntoPlayTappedIsUnreadOnTheStandaloneRoute(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Tapmimic\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilEndOfTurn | IntoPlayTapped$ True | SpellDescription$ becomes a copy.\n" +
		"Oracle:x\n"
	e, cfg, id := newFixtureDeck(t, 144, src, cloneOxSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, cloneOxSrc, state.ZBattlefield)
	addMana(t, e, 0, "C")
	activateCloneAbilityIdx(t, e, id, ox, 0)

	if o := e.G.Obj(id); o.Tapped {
		t.Fatal("a standalone IntoPlayTapped$ clone tapped a permanent that never entered")
	}
	var noted bool
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "IntoPlayTapped") {
			noted = true
		}
	}
	if !noted {
		t.Fatal("IntoPlayTapped$ was dropped without the loud unread-parameter Note")
	}
	replayCheck(t, e, cfg)
}

// TestShiftingWoodlandCopiesAGraveyardCard is the real-corpus graveyard-zone
// pin the ratchet shrink owes: Shifting Woodland's "Delirium — {2}{G}{G}:
// CARDNAME becomes a copy of target permanent card in your graveyard until
// end of turn" is a genuine STANDALONE A:AB$ Clone whose copy source lives in
// a zone the battlefield sweep never reaches (TgtZone$ Graveyard).
func TestShiftingWoodlandCopiesAGraveyardCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Shifting Woodland", "Grizzly Bears",
		"Lightning Bolt", "Sol Ring", "Forest")

	land := searchMoveByName(t, e, "Shifting Woodland", state.ZBattlefield)
	// Delirium: four card types among cards in the graveyard.
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	searchMoveByName(t, e, "Lightning Bolt", state.ZGraveyard)
	searchMoveByName(t, e, "Sol Ring", state.ZGraveyard)
	searchMoveByName(t, e, "Forest", state.ZGraveyard)

	if e.IsCreature(land) {
		t.Fatal("Shifting Woodland is a creature before the copy")
	}
	addMana(t, e, 0, "CCGG")
	e.pending = nil
	e.priorityRound()
	// Ability index 1 is the AB$ Clone (index 0 is the {T} mana ability,
	// which is not offered through the ordinary ability loop).
	opt, ok := findAbilityOption(e, land, 1)
	if !ok {
		t.Fatalf("Shifting Woodland's delirium clone ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	idx := cloneTargetOption(t, e, bear)
	if idx < 0 {
		t.Fatalf("graveyard clone target ask offers no option for the Bears: %+v", e.Pending())
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(land); o.Face() == nil || o.Face().Name != "Grizzly Bears" {
		t.Fatalf("graveyard copy name %v, want Grizzly Bears", o.Face())
	}
	if !e.IsCreature(land) {
		t.Fatal("the graveyard copy is not a creature")
	}
	if d := e.Derived(land); d.Power != 2 || d.Toughness != 2 {
		t.Fatalf("graveyard copy P/T %d/%d, want 2/2", d.Power, d.Toughness)
	}
	// The copied card stays in the graveyard -- a copy moves nothing.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the copied card left the graveyard: %+v", o)
	}
	replayCheck(t, e, cfg)
}
