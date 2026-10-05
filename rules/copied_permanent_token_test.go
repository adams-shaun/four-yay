package rules

// CR 707.10g: a copy of a permanent spell becomes a token. The copy is
// minted by events.StackCopy (which sets only IsCopy), so before the Move
// fold the resolved copy entered the battlefield with IsToken == false,
// IsCopy == true -- a battlefield object the filter grammar treated as a
// real permanent (effects/filter.go's zone-aware CR 707.10h guard) but that
// Ephemeral() reported as ceased-to-exist, hiding it from every zone
// projection. The fix folds the token entry inside events.Apply's Move case
// so a log-only replay reconstructs it; the tests here pin the entry and,
// symmetrically, that a copy of an instant/sorcery spell still rests in
// exile as a plain copy (CR 707.10h).
//
// Fixtures: the real corpus carrier archmageOfEchoesSrc from
// spellcast_permanent_test.go plus a synthetic instant-copy registrar (the
// corpus has no single-file instant copy trigger that fits without unrelated
// parameters -- the synthetic-pin convention).

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const instantCopyRegistrarSrc = "Name:Instant Copy Registrar\nManaCost:3\nTypes:Artifact\n" +
	"T:Mode$ SpellCast | ValidCard$ Instant | Execute$ TrigCopy | TriggerZones$ Battlefield | TriggerDescription$ Whenever a player casts an instant spell, copy it.\n" +
	"SVar:TrigCopy:DB$ CopySpellAbility | Defined$ TriggeredSpellAbility\n" +
	"Oracle:x\n"

const testBoltSrc = "Name:Test Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n"

// TestCopiedPermanentSpellEntersAsToken: cast a Faerie Wizard with Archmage
// of Echoes on the battlefield; the trigger copies the spell and the copy
// resolves onto the battlefield as a TOKEN (CR 707.10g) -- IsToken true,
// IsCopy cleared (so Ephemeral() reports a real permanent and the client
// projections render it), same face and targets as the original, and the
// original is a separate, un-tokened object.
func TestCopiedPermanentSpellEntersAsToken(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 71, []string{archmageOfEchoesSrc, faerieWizardSrc, nonXBeastSrc}, nil)
	moveSeeded(t, e, 0, archmageOfEchoesSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, faerieWizardSrc, state.ZHand)
	addMana(t, e, 0, "UU")
	castFirst(t, e, "cast")
	drainTriggerAsks(t, e, 40)

	// Both the original spell and its copy resolve onto the battlefield;
	// the copy is the higher-id mint (StackCopy appends after PutOnStack).
	var original, copy *state.Object
	nMatches := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Face() != nil && o.Face().Name == "Test Faerie" {
			nMatches++
			if original == nil || o.ID < original.ID {
				copy = original
				original = o
			} else {
				copy = o
			}
		}
	}
	if nMatches != 2 {
		t.Fatalf("expected the cast Faerie and its copy on the battlefield, got %d matches", nMatches)
	}
	if copy.ID == original.ID {
		t.Fatalf("copy and original share id %d", copy.ID)
	}
	if copy.IsToken != true {
		t.Errorf("copy obj id=%d IsToken=%v, want true (CR 707.10g)", copy.ID, copy.IsToken)
	}
	if copy.IsCopy != false {
		t.Errorf("copy obj id=%d IsCopy=%v, want false on the battlefield (the token must be a real, visible permanent)", copy.ID, copy.IsCopy)
	}
	if copy.Ephemeral() {
		t.Errorf("copy obj id=%d Ephemeral()=true on the battlefield -- invisible in every zone projection", copy.ID)
	}
	if copy.Face() == nil || original.Face() == nil || copy.Face().Name != original.Face().Name {
		t.Errorf("copy face %+v does not match original %+v", copy.Face(), original.Face())
	}
	if !reflect.DeepEqual(copy.Targets, original.Targets) {
		t.Errorf("copy targets %+v, want the original's %+v", copy.Targets, original.Targets)
	}
	if original.IsToken || original.IsCopy {
		t.Errorf("original obj id=%d IsToken=%v IsCopy=%v, want a plain card object", original.ID, original.IsToken, original.IsCopy)
	}
	replayCheck(t, e, cfg)
}

// TestCopiedInstantSpellCeasesWhenItLeavesTheStack: a copy of an
// instant/sorcery spell is NOT a token (CR 707.10g covers only permanent
// spells); when it resolves off the stack it ceases to exist (CR 707.10a,
// 704.5e) rather than resting in exile as a plain copy -- IsCopy true,
// IsToken false, zone ZCeased -- while the original goes to its owner's
// graveyard. This is the replay-byte-identical guard for every game that
// only copies instants/sorceries.
func TestCopiedInstantSpellCeasesWhenItLeavesTheStack(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 73, []string{instantCopyRegistrarSrc, testBoltSrc}, nil)
	moveSeeded(t, e, 0, instantCopyRegistrarSrc, state.ZBattlefield)
	addMana(t, e, 0, "R")
	castFirst(t, e, "cast")
	drainTriggerAsks(t, e, 40)

	var original, copy *state.Object
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Face() == nil || o.Face().Name != "Test Bolt" {
			continue
		}
		switch {
		case o.Zone == state.ZGraveyard:
			original = o
		case o.IsCopy:
			copy = o
		}
	}
	if original == nil || copy == nil {
		t.Fatalf("expected the original bolt in the graveyard and its copy (original=%v copy=%v)", original != nil, copy != nil)
	}
	if copy.ID == original.ID {
		t.Fatalf("copy and original share id %d", copy.ID)
	}
	if copy.Zone != state.ZCeased {
		t.Errorf("copy obj id=%d zone=%v, want ceased (CR 707.10a: a copy that leaves the stack ceases to exist)", copy.ID, copy.Zone)
	}
	if copy.IsCopy != true {
		t.Errorf("ceased copy obj id=%d IsCopy=%v, want true", copy.ID, copy.IsCopy)
	}
	if copy.IsToken != false {
		t.Errorf("ceased copy obj id=%d IsToken=%v, want false -- only a copy of a PERMANENT spell becomes a token", copy.ID, copy.IsToken)
	}
	if original.IsToken || original.IsCopy {
		t.Errorf("original obj id=%d IsToken=%v IsCopy=%v, want a plain card object", original.ID, original.IsToken, original.IsCopy)
	}
	replayCheck(t, e, cfg)
}
