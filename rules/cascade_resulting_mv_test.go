package rules

// Task cascade-resulting-mv, fix round. The round-1 premise -- that a
// cascade free cast announces a chosen {X} whose resulting mana value must
// be rechecked against the cascade source's -- was false. CR 107.3b: when a
// player casts a spell with {X} in its mana cost without paying that cost
// (and X isn't defined by the spell's text), X is considered to be 0. The
// "announce X=1 on Villainous Wealth" scenario is therefore not a legal cast
// scenario at all, and the cascadeXAsk the round-1 commit added was itself
// the rules violation: it offered illegal X>0 choices in the election's
// place.
//
// The engine's behavior without that ask is already right, and CR 702.85a's
// strict inequality holds by construction: the exile-until scan compared the
// candidate at its X=0 printed value while it sat in the library (CR
// 202.3b), the free cast poses no X announcement and casts at X=0, so the
// resulting spell's mana value is the same X=0 value the scan qualified --
// strictly below the cascade source's. The regression pins exactly that on
// the real corpus pair: accepting the election poses CR 601.2c's target ask
// and no value-of-X choice anywhere in the cast, and the candidate resolves.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// cascadeIntoWealth sets up Bloodbraid Elf casting into Villainous Wealth
// and returns once the free-cast election is pending, asserting the shared
// preconditions: the candidate sat in the library at its printed X=0 mana
// value 3 when the scan ran (3 < 4 is why the scan qualified it), the source
// is on the stack at mana value 4 with X=0, and the found card is exiled.
func cascadeIntoWealth(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, int) {
	t.Helper()
	e, cfg := cascadeTestEngine(t, seed, "Bloodbraid Elf", []string{"Forest", "Villainous Wealth"}, nil)
	lib := e.G.Zone(state.ZLibrary, 0)
	forestID, wealthID := lib[0], lib[1]
	if o := e.G.Obj(wealthID); o == nil || o.Face() == nil || o.Face().Name != "Villainous Wealth" {
		t.Fatalf("precondition: window[1] = %v, want Villainous Wealth", e.G.Obj(wealthID))
	}
	if o := e.G.Obj(wealthID); o.Zone != state.ZLibrary {
		t.Fatalf("precondition: Villainous Wealth in %s, want the library before the cascade scan", o.Zone)
	}
	if face := e.G.Obj(wealthID).Face(); face.Cmc() != 3 || ParseCost(face.ManaCost).X != 1 {
		t.Fatalf("precondition: Villainous Wealth in library has printed mana value %d and cost %q, want 3 with one X symbol", face.Cmc(), face.ManaCost)
	}
	elfID := searchMoveByName(t, e, "Bloodbraid Elf", state.ZHand)
	if mv := e.G.Obj(elfID).Face().Cmc(); mv != 4 || mv != e.G.Obj(wealthID).Face().Cmc()+1 {
		t.Fatalf("precondition: cascade source mana value %d must be exactly one above candidate's printed %d", mv, e.G.Obj(wealthID).Face().Cmc())
	}
	addMana(t, e, 0, "GGRR")
	castFixture(t, e, elfID, -1)
	// The cascade source is on the stack while its trigger resolves: mana
	// value 4 (X=0), the bound the free cast's X=0 result is compared under.
	if o := e.G.Obj(elfID); o == nil || o.Zone != state.ZStack || o.X != 0 {
		t.Fatalf("precondition: cascade source zone=%v X=%v, want stack with X=0", o, o)
	}
	idx := cascadeElection(t, e, "Villainous Wealth")
	// The scan found the candidate at its X=0 printed value and exiled it.
	if o := e.G.Obj(wealthID); o.Zone != state.ZExile {
		t.Fatalf("precondition: found candidate in %s, want exile (found at X=0)", o.Zone)
	}
	if o := e.G.Obj(forestID); o.Zone != state.ZLibrary {
		t.Fatalf("precondition: the unmatched land in %s, want already bottomed", o.Zone)
	}
	return e, cfg, elfID, wealthID, idx
}

// TestCascadeFreeCastAnnouncesNoX pins CR 107.3b on the cascade free cast:
// accepting the election poses CR 601.2c's target ask and NO value-of-X
// choice anywhere in the cast -- the candidate is cast at X=0, its resulting
// mana value is the X=0 value the scan already qualified, and the spell
// resolves. This is the CR-legal regression for the round-1 finding: the
// cascadeXAsk that commit added posed a KChoose offering X values right
// here, and this test fails against it.
func TestCascadeFreeCastAnnouncesNoX(t *testing.T) {
	t.Parallel()
	e, cfg, elfID, wealthID, idx := cascadeIntoWealth(t, 9231)
	submitChoices(t, e, idx) // accept the free-cast election
	// CR 107.3b: no X announcement. The very next ask is the CR 601.2c
	// target ask (a free cast replaces the mana cost, so the ordinary xAsk
	// sees no {X} and never runs).
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after the election want CR 601.2c's target ask, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "x" {
			t.Fatalf("the cascade free cast offered a value-of-X choice (CR 107.3b makes X 0): %+v", d.Options)
		}
	}
	submitChoices(t, e, d.Options[0].Index)
	// Walk every remaining ask the cast poses and reject an X choice from
	// any of them, not just the first: the pin is on the whole cast, so a
	// later-stage announcement cannot slip past the first assertion.
	for i := 0; i < 25; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		if d.Kind == decision.KChoose {
			for _, o := range d.Options {
				if o.Kind == "x" {
					t.Fatalf("the cascade free cast offered a value-of-X choice (CR 107.3b makes X 0): %+v", d.Options)
				}
			}
		}
		if len(d.Options) == 0 {
			t.Fatalf("ask with no options mid-cast: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	// At the priority window after the cast, both spells are on the stack.
	// The free-cast X is 0 (CR 107.3b), so the strict comparison made in
	// the library remains true on the resulting spell; X=1 would make it
	// EQUAL to the source's value, but is not a legal free-cast choice.
	if candidate, source := e.G.Obj(wealthID), e.G.Obj(elfID); candidate.Zone != state.ZStack || source.Zone != state.ZStack || candidate.X != 0 ||
		candidate.Face().Cmc()+candidate.X >= source.Face().Cmc()+source.X {
		t.Fatalf("resulting cast: candidate zone=%s X=%d printedMV=%d; source zone=%s X=%d printedMV=%d; want two stack spells, candidate X=0 and MV < source MV",
			candidate.Zone, candidate.X, candidate.Face().Cmc(), source.Zone, source.X, source.Face().Cmc())
	}
	passUntilStackEmpty(t, e, 60)
	// The free cast pushed the candidate exactly once and it resolved: the
	// X=0 resulting mana value (3) was strictly below the source's (4), so
	// nothing reversed it and the cascade tail did not bottom it back.
	if n := putOnStackCount(e, wealthID); n != 1 {
		t.Fatalf("the free cast pushed the candidate %d time(s), want 1", n)
	}
	if got := e.G.Obj(wealthID).Zone; got != state.ZGraveyard {
		t.Fatalf("the X=0 candidate resolved into %s, want the graveyard", got)
	}
	if n := movedTo(t, e, wealthID, state.ZExile, state.ZLibrary); n != 0 {
		t.Fatalf("the legally cast candidate was bottomed %d time(s), want 0", n)
	}
	if got := e.G.Obj(elfID).Zone; got != state.ZBattlefield {
		t.Fatalf("resolved cascade source in %s, want the battlefield", got)
	}
	replayCheck(t, e, cfg)
}
