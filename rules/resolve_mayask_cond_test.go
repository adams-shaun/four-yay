package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The ask-free predicate's conditional verdicts (cards.MayAskCond*): a
// chain whose only reason to ask reads its chosen targets is settled by the
// stack object, with no predicate miss either way.

const (
	tapeCondBearSrc  = "Name:Tape Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	tapeCondEquipSrc = "Name:Tape Sword\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\nOracle:x\n"
	// Fiery Annihilation's shape: the sub's target is relative to the
	// parent's (not announced at cast), asked at resolution only when an
	// Equipment is attached to the damaged creature.
	tapeCondFierySrc = "Name:Tape Fiery\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ DealDamage | NumDmg$ 1 | ValidTgts$ Creature | SubAbility$ DBExile\n" +
		"SVar:DBExile:DB$ ChangeZone | Origin$ Battlefield | Destination$ Exile | ValidTgts$ Equipment.AttachedTo ParentTarget | TargetMin$ 0 | TargetMax$ 1\nOracle:x\n"
	// Sun-Blessed Healer's shape: a targeted return to the battlefield asks
	// only through the returned card's own entry.
	tapeCondReanimSrc = "Name:Tape Reanimate\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Battlefield | ValidTgts$ Creature.YouOwn | TgtPrompt$ x\nOracle:x\n"
	tapeCondChooserSrc = "Name:Tape Chooser\nManaCost:G\nTypes:Creature Elf\nPT:1/1\n" +
		"K:ETBReplacement:Other:DBChoose\nSVar:DBChoose:DB$ ChooseColor | Defined$ You | SpellDescription$ x\nOracle:x\n"
)

func TestKr8ParentSubTargetWithoutCandidateIsExempt(t *testing.T) {
	_, st := kr8Kernel(t, 2, 9301, func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Tape Bear", state.ZBattlefield)
		tapeCastAndResolve(t, e, "Tape Fiery", "R")
	}, tapeCondFierySrc, tapeCondBearSrc)
	if st.Misses != 0 || st.Checkpoints != 0 || st.Exempt == 0 {
		t.Fatalf("no Equipment on the battlefield: the resolution should be exempt: %+v", st)
	}
}

func TestKr8ParentSubTargetWithCandidateIsCheckpointed(t *testing.T) {
	_, st := kr8Kernel(t, 2, 9302, func(t *testing.T, e *Engine) {
		bear := moveByName(t, e, 0, "Tape Bear", state.ZBattlefield)
		sword := moveByName(t, e, 0, "Tape Sword", state.ZBattlefield)
		e.emit(events.Event{Kind: events.Attach, Obj: sword, IDs: []state.ObjID{bear}})
		tapeCastAndResolve(t, e, "Tape Fiery", "R")
		if o := e.G.Obj(sword); o.Zone != state.ZExile {
			t.Fatalf("the attached Equipment was not exiled (zone %v)", o.Zone)
		}
	}, tapeCondFierySrc, tapeCondBearSrc, tapeCondEquipSrc)
	if st.Misses != 0 || st.Checkpoints != 1 || st.Served == 0 {
		t.Fatalf("an attached Equipment: the sub-target ask must be checkpointed and served: %+v", st)
	}
}

func TestKr8TargetEntryWithoutEntryAskIsExempt(t *testing.T) {
	_, st := kr8Kernel(t, 2, 9303, func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Tape Bear", state.ZGraveyard)
		tapeCastAndResolve(t, e, "Tape Reanimate", "B")
	}, tapeCondReanimSrc, tapeCondBearSrc)
	if st.Misses != 0 || st.Checkpoints != 0 || st.Exempt == 0 {
		t.Fatalf("a vanilla creature returned: the resolution should be exempt: %+v", st)
	}
}

func TestKr8TargetEntryWithEntryAskIsCheckpointed(t *testing.T) {
	_, st := kr8Kernel(t, 2, 9304, func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Tape Chooser", state.ZGraveyard)
		tapeCastAndResolve(t, e, "Tape Reanimate", "B")
	}, tapeCondReanimSrc, tapeCondChooserSrc)
	if st.Misses != 0 || st.Checkpoints != 1 {
		t.Fatalf("a returned creature with an as-enters choice: the resolution must be checkpointed: %+v", st)
	}
}
