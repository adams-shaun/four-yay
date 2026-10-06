package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestGatheringStoneMillsThePeekedTopCard is the ECL Gathering Stone
// regression. The script is a three-step chain:
//
//	SVar:TrigPeek:DB$ PeekAndReveal | PeekAmount$ 1 | RevealValid$ Card.ChosenType
//	  | RevealOptional$ True | RememberRevealed$ True | SubAbility$ DBToHand
//	SVar:DBToHand:DB$ ChangeZone | Defined$ Remembered | Origin$ Library | Destination$ Hand | SubAbility$ DBToGrave
//	SVar:DBToGrave:DB$ ChangeZone | Defined$ TopOfLibrary | Origin$ Library | Destination$ Graveyard
//	  | Optional$ True | ConditionDefined$ Remembered | ConditionPresent$ Card | ConditionCompare$ EQ0 | SubAbility$ DBCleanup
//
// When the peeked card does NOT match the chosen type, `RememberRevealed$`
// captures nothing, so DBToHand's `Defined$ Remembered` still holds only the
// trigger's fire-time self-referent (the artifact, on the battlefield). That
// fetch moves no card out of the library, but the direct Defined$ fetch path
// used to shuffle the owner's library anyway -- reordering it before
// DBToGrave's `Defined$ TopOfLibrary` read its top. The mill then took a
// random card from deeper in the library instead of the card that was
// peeked. The fix is in effects.moveDefinedLibraryObjects: a fetch that moved
// no card from that owner's library is not a search and shuffles nothing.
//
// The library is [Jace Beleren, Grizzly Bears, ...]; the chosen type is Bear.
// Jace does not match, so the graveyard must get Jace -- not a deeper card --
// and the library must shrink by exactly the one milled card.
func TestGatheringStoneMillsThePeekedTopCard(t *testing.T) {
	e := kr2Engine(t, 2)
	stone := kr2Put(t, e, 0, kr2Corpus(t, "Gathering Stone"), state.ZHand, false)
	// kr2Put(top=true) prepends, so the deeper Bear is placed first.
	deep := kr2Put(t, e, 0, kr2Corpus(t, "Grizzly Bears"), state.ZLibrary, true)
	peeked := kr2Put(t, e, 0, kr2Corpus(t, "Jace Beleren"), state.ZLibrary, true)

	// Preconditions the assertions below depend on: the peeked card really is
	// the top card and is not a Bear (so the chosen-type filter drops it),
	// and a distinct deeper card sits directly under it.
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < 2 || lib[0] != peeked || lib[1] != deep {
		t.Fatalf("precondition: library top two = %v, want peeked %d then deeper %d", lib, peeked, deep)
	}
	if peeked == deep {
		t.Fatal("precondition: the peeked and deeper cards must differ")
	}
	if o := e.G.Obj(peeked); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("precondition: peeked card zone = %v, want library", o)
	}
	before := len(lib)

	addMana(t, e, 0, "CCCC")
	submitChoices(t, e, castOptionFor(t, e, stone).Index)

	for i := 0; i < 200; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		choice := tapePick(d)
		switch d.ResumeKind {
		case "etb":
			// "As this artifact enters, choose a creature type."
			found := false
			for _, o := range d.Options {
				if o.Label == "Bear" {
					choice = []int{o.Index}
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("precondition: ETB type decision offers no %q option: %+v", "Bear", d.Options)
			}
		case "defined_library_optional":
			// "you may put it into your graveyard"
			choice = []int{kr2Kind(t, d, "yes")}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choice}); err != nil {
			t.Fatalf("submit %s: %v", d.ResumeKind, err)
		}
	}

	if o := e.G.Obj(stone); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Gathering Stone zone = %v, want battlefield (the trigger resolved)", o)
	}
	// The peeked card was milled.
	if o := e.G.Obj(peeked); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("peeked top card zone = %v, want graveyard", o)
	}
	// The deeper card must not be touched.
	if o := e.G.Obj(deep); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("deeper card zone = %v, want library (it must not be milled)", o)
	}
	// The library shrank by exactly the one milled card.
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != before-1 {
		t.Fatalf("library size = %d, want %d (exactly the peeked card removed)", got, before-1)
	}
}
