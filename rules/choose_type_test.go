package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// The mid-resolution ChooseType ask (task ct1): a resolution-time
// "Choose a creature type" poses a real KChoose over the shared creature-type
// enumeration (rules/cast.go's creatureTypeOptions, the same list the
// cast-time "as this enters" ask builds), and the answered type is what the
// downstream ChosenType filters read. Pinned end to end on the real corpus
// carrier Haunting Voyage.
//
// The no-ask degenerate shapes (a single offerable type, a host that cannot
// ask) keep the deterministic fallback byte-identically — pinned on the
// effects side (effects/choose_type_ask_test.go); here the ask is real and
// the answer governs.

// TestHauntingVoyageForetoldReturnsTheChosenTypeNotTheFallback casts the
// foretelled Haunting Voyage over a graveyard stocked with Elves AND a
// Zombie and answers the type ask "Zombie" — the OPPOSITE of the pre-ct1
// object-id-order fallback ("Elf"). The foretold arm's ReturnAll must then
// move the Zombie and leave every Elf, proving the CHOSEN type governs.
func TestHauntingVoyageForetoldReturnsTheChosenTypeNotTheFallback(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Haunting Voyage"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	foretellIt(t, e, id)
	var elves [3]state.ObjID
	for i := range elves {
		elves[i] = graveCreature(t, e, 0, "Name:Elf"+string(rune('A'+i))+"\nManaCost:no cost\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	}
	zombie := graveCreature(t, e, 0, "Name:Zombie\nManaCost:no cost\nTypes:Creature Zombie\nPT:1/1\nOracle:x\n")
	driveToTurn3Main(t, e)
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MB] = 5, 2
	submitOption(t, e, "foretell_cast", "Cast Haunting Voyage (foretold)")
	// Pass priority once: the spell resolves and suspends on the
	// mid-resolution creature-type ask (ct1). Answer it "Zombie" — the
	// OPPOSITE of the pre-ct1 object-id-order fallback ("Elf") — so the
	// foretold arm's ReturnAll provably returns the CHOSEN type.
	passUntilAsk(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosetype" ||
		d.Player != 0 || d.Min != 1 || d.Max != 1 || d.Prompt != "Choose a creature type" {
		t.Fatalf("expected the mid-resolution creature-type ask, got %+v", d)
	}
	// CR 205.3m: every creature type is offered, the chooser's own (sorted)
	// first.
	if len(d.Options) != len(effects.CreatureTypeWordList()) || d.Options[0].Kind != "type" || d.Options[0].Label != "Elf" ||
		d.Options[1].Kind != "type" || d.Options[1].Label != "Zombie" {
		t.Fatalf("option list = %+v, want the sorted Elf/Zombie \"type\" options leading every creature type", d.Options[:min(3, len(d.Options))])
	}
	idx := optionByLabel(d.Options, "Zombie")
	if idx < 0 {
		t.Fatalf("no Zombie option in %+v", d.Options)
	}
	submitChoices(t, e, idx)
	finishCast(t, e, id)
	if o := e.G.Obj(zombie); o.Zone != state.ZBattlefield {
		t.Fatalf("the CHOSEN type's card stayed in %s, want battlefield", o.Zone)
	}
	for _, eid := range elves {
		if o := e.G.Obj(eid); o.Zone != state.ZGraveyard {
			t.Fatalf("%s moved to %s, want graveyard (the fallback's type, not the answer's)", o.Face().Name, o.Zone)
		}
	}
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("resolved voyage in %s, want graveyard", o.Zone)
	}
}
