package rules

// Mount Velus Manticore, end to end on the real compiled corpus (task
// cli-20260924T092757Z-74f4f54a): "At the beginning of combat on your turn,
// you may discard a card. When you do, CARDNAME deals X damage to any
// target, where X is the number of card types the discarded card has."
//
// The trigger's chain is `Discard | Mode$ TgtChoose | RememberDiscarded$
// True | SubAbility$ DBImmediateTrigger`, and the payoff SVar is
// `X:TriggerRemembered$CardTypes`. Before that reference was resolved the
// damage was 0 whatever was discarded; the assertion below pins the number
// of card types of the remembered (discarded) card as the damage.
//
// A second engine with a SINGLE-type discard runs the same chain, so the
// test cannot pass on "the trigger always deals some fixed amount": the two
// damages differ by exactly the difference in card types.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// manticoreFodderSrc is the two-card-type discard fixture: an Artifact
// Creature (two CR 205.1 card types; the creature type Golem is not one).
const manticoreFodderSrc = "Name:Fodder\nTypes:Artifact Creature Golem\nPT:1/1\nOracle:x\n"

// manticoreSingleTypeSrc is the one-card-type discard fixture.
const manticoreSingleTypeSrc = "Name:Fodder\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// runManticoreCombat places Mount Velus Manticore on seat 0's battlefield,
// seeds exactly the given discard fixture in seat 0's hand, drives to the
// beginning-combat trigger, answers the optional election "yes", chooses
// seat 1 as the damage target, and returns the pre-trigger life of seat 1
// and the damage it took. It fails loudly if any step of the chain is
// missing, so a silent no-op can never count as a pass.
func runManticoreCombat(t *testing.T, seed uint64, fodderSrc string) (eng *Engine, felt int32, manticoreID, fodderID state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	manticore := searchCorpusCard(t, reg, "Mount Velus Manticore")
	e, _ := tokenReplGame(t, seed, manticore)
	eng = e

	manticoreID = onBoardCard(t, e, 0, manticore)
	// PRECONDITION: the trigger's source is on the battlefield (its
	// TriggerZones$ Battlefield), not in a zone where the trigger cannot fire.
	if o := e.G.Obj(manticoreID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Mount Velus Manticore id %d zone = %v, want battlefield (vacuous setup)", manticoreID, o)
	}
	// Exactly one eligible discardable card in hand, so the TgtChoose discard
	// can only take it. clearHand first because the opening draw would
	// otherwise leave cards the discard might pick instead.
	emptyHandToLibrary(t, e, 0)
	fodderID = seedHand(t, e, 0, fodderSrc)
	if o := e.G.Obj(fodderID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("discard fixture id %d zone = %v, want hand (vacuous setup)", fodderID, o)
	}
	e.priorityRound()

	// Drive to the beginning-combat trigger's optional election. Every step
	// before it is an ordinary priority pass.
	d := e.Pending()
	for i := 0; i < 60 && d != nil && d.Kind == decision.KPriority; i++ {
		passOnce(t, e)
		d = e.Pending()
	}
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the beginning-combat optional election, got %+v", d)
	}
	// PRECONDITION: the election belongs to the Manticore and really offers
	// a yes/no pair (an empty or unrelated decision would make the rest of
	// the test meaningless).
	if d.Player != 0 || len(d.Options) != 2 || d.Options[0].Obj != manticoreID {
		t.Fatalf("optional election = %+v, want the Manticore's yes/no for seat 0", d)
	}
	submitChoices(t, e, d.Options[0].Index) // yes — discard

	// The chained Discard (Mode$ TgtChoose) leaves one card to take; the
	// ImmediateTrigger then poses the DealDamage target ask. Answer every
	// non-priority decision on the way, choosing seat 1 for the target.
	lifeBefore := e.G.Players[1].Life
	targeted := false
	for i := 0; i < 40; i++ {
		d = e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break // the trigger has fully resolved
			}
			passOnce(t, e)
			continue
		}
		if d.Kind == decision.KTarget {
			// CR 603.12: the reflexive "when you do" ability's own target,
			// chosen as it is put on the stack.
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "player" && o.Label == "b" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("seat 1 was not offered as a damage target: %+v", d.Options)
			}
			submitChoices(t, e, idx)
			targeted = true
			continue
		}
		// Any other mid-resolution ask (the discard pick among them): take
		// the first option and keep going.
		if len(d.Options) == 0 {
			t.Fatalf("decision with no options: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	// PRECONDITION: the target ask was actually reached — otherwise the
	// damage number below could not be attributed to this trigger.
	if !targeted {
		t.Fatal("the Manticore's damage target ask never appeared; the trigger chain did not run")
	}
	// The discard really happened: the fixture left the hand for the
	// graveyard, which is what armed TriggerRemembered$ for the SVar.
	if z := e.G.Obj(fodderID).Zone; z != state.ZGraveyard {
		t.Fatalf("discarded fixture zone = %v, want graveyard (the discard did not run; X would be unset)", z)
	}
	return eng, lifeBefore - e.G.Players[1].Life, manticoreID, fodderID
}

func TestMountVelusManticoreDealsCardTypeCountDamage(t *testing.T) {
	t.Parallel()
	e, felt, _, fodderID := runManticoreCombat(t, 9401, manticoreFodderSrc)
	// The fixture really has two card types (Artifact + Creature; Golem is a
	// creature type, not a card type). If this precondition failed, the
	// assertion below would be vacuous about the count.
	types := e.G.Obj(fodderID).Face().Types
	if !containsAll(types, "Artifact", "Creature", "Golem") || len(types) != 3 {
		t.Fatalf("fixture types = %v, want Artifact Creature Golem (vacuous test setup)", types)
	}
	if felt != 2 {
		t.Fatalf("Mount Velus Manticore dealt %d damage, want 2 (the two card types of the discarded card)", felt)
	}
}

func TestMountVelusManticoreSingleTypeDealsOne(t *testing.T) {
	t.Parallel()
	e, felt, _, fodderID := runManticoreCombat(t, 9402, manticoreSingleTypeSrc)
	types := e.G.Obj(fodderID).Face().Types
	if !containsAll(types, "Creature", "Bear") || len(types) != 2 {
		t.Fatalf("fixture types = %v, want Creature Bear (vacuous test setup)", types)
	}
	// The differential: a one-type discard deals one, so the two damages
	// below cannot both be a fixed constant or zero.
	if felt != 1 {
		t.Fatalf("Mount Velus Manticore dealt %d damage, want 1 (one card type on the discarded card)", felt)
	}
}

// containsAll reports whether every want is present in got.
func containsAll(got []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
