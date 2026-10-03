package rules

// Task cascade-approx (row ~48): the three real deviations the cascade1 row
// recorded, each pinned end to end on a real corpus card where one exists:
//
//   (c) CR 202.3b -- an {X} cascade spell's mana value is its printed value
//       plus the X announced at casting (it is on the stack), not its
//       printed face at X=0.
//   (b) CR 601.3 -- a Play effect's free cast must obey the same
//       casting-prohibition gate the ordinary cast offer runs (the
//       CantBeCast family); a prohibited card is refused, never cast.
//   (a) an Effect whose Triggers$ body is the self-exile-on-SpellCast idiom
//       (TARDIS's "the next spell you cast this turn has cascade") is the
//       Effect's cast-driven lifetime, not an inert delayed promise, so the
//       grant ends on the next qualifying cast instead of every one.
//
// The exiled bottom pile's existing-order stand-in for the CR's "random
// order" is the standing no-randomness contract and is deliberately left as
// it was; these tests do not touch it.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cascadeTriggerPushes counts the cascade triggers the log minted (the
// KeywordTriggerPush whose Counter payload is exactly the cascade keyword
// body). It is the observable "did this cast cascade" bit, independent of
// whether the exile-until scan then found a castable card.
func cascadeTriggerPushes(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.KeywordTriggerPush && strings.HasPrefix(ev.Counter, "__kwCascade") {
			n++
		}
	}
	return n
}

// putOnStackCount counts the CR 601.2a stack pushes of id.
func putOnStackCount(e *Engine, id state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.PutOnStack && ev.Obj == id {
			n++
		}
	}
	return n
}

// findCascadeGrant returns the registered layer-6 continuous effect granting
// the Cascade keyword, with its derived cast-driven lifetime.
func findCascadeGrant(e *Engine) (state.ContinuousEffect, bool) {
	for _, ce := range e.active() {
		for _, k := range ce.AddKeywords {
			if strings.EqualFold(k, "Cascade") {
				return ce, true
			}
		}
	}
	return state.ContinuousEffect{}, false
}

// TestCascadeXSpellUsesAnnouncedManaValue is CR 202.3b on the one real
// corpus K:Cascade carrier with an {X} cost, Let the Galaxy Burn ({X}{5}{R},
// printed mana value 6). Cast at X=2 its mana value on the stack is 8, so the
// exile-until scan must find a nonland card with mana value 7 (Platinum
// Angel) BEFORE the mana-value-5 Air Elemental beneath it. Reading only the
// printed face compares at 6 and finds Air Elemental instead.
func TestCascadeXSpellUsesAnnouncedManaValue(t *testing.T) {
	t.Parallel()
	e, cfg := cascadeTestEngine(t, 9221, "Let the Galaxy Burn",
		[]string{"Forest", "Platinum Angel", "Air Elemental"}, nil)
	lib := e.G.Zone(state.ZLibrary, 0)
	forestID, angelID, elementalID := lib[0], lib[1], lib[2]
	// Precondition: the two candidate mana values genuinely straddle the
	// two readings (6 < 7, but 7 < 8), and the land is skipped either way.
	if mv := e.G.Obj(angelID).Face().Cmc(); mv != 7 {
		t.Fatalf("precondition: Platinum Angel mana value = %d, want 7", mv)
	}
	if mv := e.G.Obj(elementalID).Face().Cmc(); mv != 5 {
		t.Fatalf("precondition: Air Elemental mana value = %d, want 5", mv)
	}
	galaxyID := searchMoveByName(t, e, "Let the Galaxy Burn", state.ZHand)
	if mv := e.G.Obj(galaxyID).Face().Cmc(); mv != 6 {
		t.Fatalf("precondition: Let the Galaxy Burn printed mana value = %d, want 6 (X counts 0)", mv)
	}
	// {X}{5}{R} with X=2 needs 8 mana.
	addMana(t, e, 0, "CCCCCCCR")
	d := castFixture(t, e, galaxyID, -1)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("want the X announcement ask, got %+v", d)
	}
	submitChoices(t, e, 2)
	d = passUntilNonPriority(t, e, 40)
	// The cascade spell is still on the stack while its trigger resolves, so
	// the announced X is live provenance (CR 202.3b).
	if o := e.G.Obj(galaxyID); o == nil || o.Zone != state.ZStack || o.X != 2 {
		t.Fatalf("precondition: cascade spell zone=%v X=%d, want stack/X=2", o, o.X)
	}
	_ = cascadeElection(t, e, "Platinum Angel")
	if got := e.G.Obj(angelID).Zone; got != state.ZExile {
		t.Fatalf("the mana-value-7 card is in %s, want exile (found at the announced X=2)", got)
	}
	if got := e.G.Obj(elementalID).Zone; got != state.ZLibrary {
		t.Fatalf("the mana-value-5 card is in %s, want untouched in the library", got)
	}
	if got := e.G.Obj(forestID).Zone; got != state.ZLibrary {
		t.Fatalf("the land is in %s, want already bottomed", got)
	}
	// Decline the free cast; the found card goes to the bottom.
	submitChoices(t, e)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(angelID).Zone; got != state.ZLibrary {
		t.Fatalf("declined found card in %s, want the library", got)
	}
	replayCheck(t, e, cfg)
}

// TestCascadeFreeCastRespectsCantBeCast is CR 601.3's prohibition half on a
// Play effect's cast. Maelstrom Colossus (mana value 8, one cascade) cascades
// into Rakdos, Lord of Riots, whose own CantBeCast static forbids casting it
// unless an opponent lost life this turn. With no life lost the prohibited
// card must never be OFFERED at the free-cast election (the election is
// filtered, not merely refused after a pick); the cascade tail bottoms it.
// After an opponent loses life the restriction lifts, the same card is
// offered, and it casts.
func TestCascadeFreeCastRespectsCantBeCast(t *testing.T) {
	t.Parallel()
	cascadeIntoRakdos := func(t *testing.T, seed uint64, opponentLosesLife bool) (*Engine, Config, state.ObjID) {
		t.Helper()
		e, cfg := cascadeTestEngineFiller(t, seed, "Maelstrom Colossus",
			[]string{"Forest", "Rakdos, Lord of Riots"}, nil, "Mountain")
		var rakdosID state.ObjID
		for _, id := range e.G.Zone(state.ZLibrary, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Rakdos, Lord of Riots" {
				rakdosID = id
			}
		}
		if rakdosID == 0 {
			t.Fatal("precondition: Rakdos, Lord of Riots not in seat 0's library")
		}
		// Precondition: Rakdos's mana value is below the Colossus's, so the
		// scan finds it, and its self-restriction is the thing under test.
		if mv := e.G.Obj(rakdosID).Face().Cmc(); mv != 4 {
			t.Fatalf("precondition: Rakdos mana value = %d, want 4", mv)
		}
		if o := e.G.Obj(rakdosID); o.Zone != state.ZLibrary {
			t.Fatalf("precondition: Rakdos in %s, want the library", o.Zone)
		}
		if opponentLosesLife {
			e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
		}
		colossusID := searchMoveByName(t, e, "Maelstrom Colossus", state.ZHand)
		hasCascade := false
		for _, kw := range e.G.Obj(colossusID).Face().Keywords {
			if strings.EqualFold(strings.TrimSpace(kw), "Cascade") {
				hasCascade = true
			}
		}
		if !hasCascade {
			t.Fatal("precondition: corpus Maelstrom Colossus face does not carry Cascade")
		}
		addMana(t, e, 0, "CCCCCCCC")
		pd := e.Pending()
		if pd == nil {
			t.Fatal("no decision pending for the cast")
		}
		cidx := -1
		for _, o := range pd.Options {
			if o.Kind == "cast" && o.Obj == colossusID {
				cidx = o.Index
			}
		}
		if cidx < 0 {
			t.Fatalf("no cast option for the Colossus: %+v", pd.Options)
		}
		submitChoices(t, e, cidx)
		if opponentLosesLife {
			// Permitted: the restriction lifted, so the election is still
			// offered and it names Rakdos.
			d := passUntilNonPriority(t, e, 20)
			if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
				t.Fatalf("want the cascade free-cast election, got %+v", d)
			}
			if len(d.Options) != 1 || d.Options[0].Obj != rakdosID {
				t.Fatalf("the permitted election %+v does not offer Rakdos %d", d.Options, rakdosID)
			}
			// The zone the CantBeCast static is read against: cascade casts
			// from EXILE, so that is where the self-restriction must be live
			// (both for the effPlay filter and for beginPlay's recheck).
			if got := e.G.Obj(rakdosID).Zone; got != state.ZExile {
				t.Fatalf("precondition: Rakdos in %s at the election, want exile (the zone the restriction reads)", got)
			}
			submitChoices(t, e, d.Options[0].Index)
		} else {
			// Prohibited: the candidate must be absent from the election
			// itself -- effPlay filters the CantBeCast statics out, so no play
			// ask is ever posed and the resolution completes with the found
			// card left for the cascade tail to bottom. Without the filter the
			// election IS posed here, naming Rakdos. Drive the whole window
			// (the trigger resolves while the Colossus itself is still on the
			// stack) and fail on any play ask -- or any other unexpected ask.
			sawElection := false
			for i := 0; i < 40 && !e.G.Over && len(e.G.Stack) > 0; i++ {
				d := e.Pending()
				if d == nil {
					break
				}
				if d.Kind == decision.KPriority {
					pidx := -1
					for _, o := range d.Options {
						if o.Kind == "pass" {
							pidx = o.Index
						}
					}
					if pidx < 0 {
						t.Fatalf("priority decision with no pass option: %+v", d)
					}
					submitChoices(t, e, pidx)
					continue
				}
				if d.ResumeKind == "play" {
					sawElection = true
					break
				}
				t.Fatalf("unexpected decision during the prohibited resolution: kind %v resume %q options %+v", d.Kind, d.ResumeKind, d.Options)
			}
			if sawElection {
				t.Fatal("the prohibited candidate was offered at the election")
			}
		}
		passUntilStackEmpty(t, e, 60)
		return e, cfg, rakdosID
	}

	t.Run("prohibited", func(t *testing.T) {
		e, cfg, rakdosID := cascadeIntoRakdos(t, 9222, false)
		if got := e.G.Obj(rakdosID).Zone; got == state.ZBattlefield {
			t.Fatalf("Rakdos reached the battlefield despite the CantBeCast restriction")
		}
		if got := e.G.Obj(rakdosID).Zone; got != state.ZLibrary {
			t.Fatalf("refused Rakdos in %s, want the library (bottomed by the cascade tail)", got)
		}
		// CR 601.3: a prohibited spell is never BEGUN, so it must never reach
		// the stack. CR 601.2a's push is a PutOnStack event (not a MoveZone),
		// and a CR 601.2e/733.1 recheck reversal would leave one here.
		if n := putOnStackCount(e, rakdosID); n != 0 {
			t.Fatalf("the prohibited card was put on the stack %d time(s); CR 601.3 forbids beginning the cast", n)
		}
		// The withholding happened at the ELECTION (effPlay filtered its
		// candidates, found none, and said so); beginPlay's own refusal was
		// never reached, so its Note must be absent.
		if !hasNote(e, "Play found no card to play") {
			t.Fatal("no Note recorded the filtered-empty election")
		}
		if hasNote(e, "cannot cast a restricted card") {
			t.Fatal("the beginPlay refusal fired; the candidate should have been filtered before the election")
		}
		replayCheck(t, e, cfg)
	})

	t.Run("permitted", func(t *testing.T) {
		e, cfg, rakdosID := cascadeIntoRakdos(t, 9223, true)
		if got := e.G.Obj(rakdosID).Zone; got != state.ZBattlefield {
			t.Fatalf("Rakdos in %s, want the battlefield (the restriction lifted after the life loss)", got)
		}
		if hasNote(e, "cannot cast a restricted card") {
			t.Fatal("the refusal Note fired although the restriction had lifted")
		}
		replayCheck(t, e, cfg)
	})
}
