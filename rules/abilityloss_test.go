package rules

// CR 613.1f "loses all abilities" beyond keywords, and CR 613.1e colour
// setting seen by the filter grammar. Every card is a REAL corpus card; the
// Oracle scenarios in testdata/oracle/base-characteristics/witness-protection.json
// cover the enchanted creature's own triggered, activated, mana and static
// abilities. These tests pin the class around them: a lord's continuous
// effect ending with its ability, the one-shot Animate delivery, the
// timestamp rule for removers, and the resolution-time colour filter.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// lossOffers reports whether obj has a mana ability its controller could
// activate (the mana walk's membership, payability aside).
func lossOffers(t *testing.T, e *Engine, obj state.ObjID) bool {
	t.Helper()
	return len(e.appendAvailableManaAbilitiesGate(nil, nil, 0, obj, true)) > 0
}

// lossAttach puts the named Aura onto the battlefield attached to bearer.
func lossAttach(t *testing.T, e *Engine, aura string, p state.PlayerID, bearer state.ObjID) state.ObjID {
	t.Helper()
	// Entry and attachment in one step, as a resolving Aura spell does it: a
	// drain between the two would sweep the unattached Aura (CR 704.5m).
	var id state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, c := range e.G.Zone(z, p) {
			if o := e.G.Obj(c); id == 0 && o != nil && o.Face() != nil && o.Face().Name == aura {
				id = c
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
			}
		}
	}
	if id == 0 {
		t.Fatalf("%s is not in seat %d's hand or library", aura, p)
	}
	e.emit(events.Event{Kind: events.Attach, Obj: id, IDs: []state.ObjID{bearer}})
	e.pending = nil
	e.Advance()
	if got := e.G.Obj(id); got.Zone != state.ZBattlefield || got.AttachedTo != bearer {
		t.Fatalf("%s: zone %s attached to %d, want the battlefield on %d", aura, got.Zone, got.AttachedTo, bearer)
	}
	return id
}

// A lord that loses all abilities stops pumping: its layer-7 static is gone
// with the ability (and so is its own mana ability), while the other Elf's
// mana ability is untouched. When the Aura leaves, both come back.
func TestLordUnderWitnessProtectionStopsPumping(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Elvish Archdruid"), lookup(t, reg, "Llanowar Elves"),
		lookup(t, reg, "Witness Protection")}, nil)
	lord := moveCorpusCard(t, e, "Elvish Archdruid", 0, state.ZBattlefield)
	elf := moveCorpusCard(t, e, "Llanowar Elves", 0, state.ZBattlefield)
	lossNextTurn(e)
	if p, tt := e.Power(elf), e.Toughness(elf); p != 2 || tt != 2 {
		t.Fatalf("precondition: Llanowar Elves beside the Archdruid is %d/%d, want 2/2", p, tt)
	}
	if !lossOffers(t, e, lord) || !lossOffers(t, e, elf) {
		t.Fatal("precondition: both Elves offer their mana abilities")
	}
	aura := lossAttach(t, e, "Witness Protection", 0, lord)
	if p, tt := e.Power(elf), e.Toughness(elf); p != 1 || tt != 1 {
		t.Errorf("Llanowar Elves is %d/%d beside an Archdruid with no abilities, want 1/1", p, tt)
	}
	if lossOffers(t, e, lord) {
		t.Error("the enchanted Archdruid still offers its mana ability")
	}
	if !lossOffers(t, e, elf) {
		t.Error("the unenchanted Llanowar Elves lost its mana ability")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: aura, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.Advance()
	if p, tt := e.Power(elf), e.Toughness(elf); p != 2 || tt != 2 {
		t.Errorf("Llanowar Elves is %d/%d once the Aura left, want 2/2", p, tt)
	}
	if !lossOffers(t, e, lord) {
		t.Error("the Archdruid's mana ability did not return with the Aura gone")
	}
	replayCheck(t, e, cfg)
}

// Humility removes a creature lord's ability whichever entered first: the
// lord's static begins in layer 7, so any removal ends it (CR 613.6 only
// preserves an effect that already began in an earlier layer). Humility's own
// static is not a creature's and keeps applying.
func TestHumilityEndsALordsPumpInEitherOrder(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, humilityFirst := range []bool{true, false} {
		e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Elvish Archdruid"), lookup(t, reg, "Llanowar Elves"),
			lookup(t, reg, "Humility")}, nil)
		elf := moveCorpusCard(t, e, "Llanowar Elves", 0, state.ZBattlefield)
		var lord state.ObjID
		if humilityFirst {
			moveCorpusCard(t, e, "Humility", 0, state.ZBattlefield)
			lord = moveCorpusCard(t, e, "Elvish Archdruid", 0, state.ZBattlefield)
		} else {
			lord = moveCorpusCard(t, e, "Elvish Archdruid", 0, state.ZBattlefield)
			moveCorpusCard(t, e, "Humility", 0, state.ZBattlefield)
		}
		lossNextTurn(e)
		for _, id := range []state.ObjID{elf, lord} {
			if p, tt := e.Power(id), e.Toughness(id); p != 1 || tt != 1 {
				t.Errorf("humilityFirst=%t: %s is %d/%d under Humility, want 1/1", humilityFirst, e.G.Obj(id).Face().Name, p, tt)
			}
			if lossOffers(t, e, id) {
				t.Errorf("humilityFirst=%t: %s still offers its mana ability under Humility", humilityFirst, e.G.Obj(id).Face().Name)
			}
		}
		replayCheck(t, e, cfg)
	}
}

// The one-shot Animate delivery (Turn to Frog): the target has no mana
// ability until end of turn, and has it again after cleanup.
func TestTurnToFrogRemovesTheManaAbilityUntilEndOfTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Llanowar Elves"), lookup(t, reg, "Turn to Frog")}, nil)
	elf := moveCorpusCard(t, e, "Llanowar Elves", 0, state.ZBattlefield)
	lossNextTurn(e)
	if !lossOffers(t, e, elf) {
		t.Fatal("precondition: Llanowar Elves has its mana ability")
	}
	moveCorpusCard(t, e, "Turn to Frog", 0, state.ZHand)
	addMana(t, e, 0, "CU")
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Turn to Frog's target ask, got %+v", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == elf {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("Llanowar Elves is not a Turn to Frog target: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 40)
	if p, tt := e.Power(elf), e.Toughness(elf); p != 1 || tt != 1 || e.Colors(elf) != "U" {
		t.Fatalf("the Frog is %d/%d %q, want a blue 1/1", p, tt, e.Colors(elf))
	}
	if lossOffers(t, e, elf) {
		t.Error("the Frog still offers Llanowar Elves' mana ability")
	}
	e.EndOfTurnCleanup()
	if !lossOffers(t, e, elf) {
		t.Error("Llanowar Elves' mana ability did not return after cleanup")
	}
	replayCheck(t, e, cfg)
}

// A resolving effect's colour filter reads the layer-5 colours: Perish
// (destroy all green creatures) destroys a black creature Witness Protection
// made green and white, and spares a green one Frogify made blue.
func TestPerishReadsTheDerivedColours(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Perish"), lookup(t, reg, "Witness Protection"),
		lookup(t, reg, "Frogify")}, []*cards.Card{lookup(t, reg, "Walking Corpse"), lookup(t, reg, "Grizzly Bears")})
	corpse := moveCorpusCard(t, e, "Walking Corpse", 1, state.ZBattlefield)
	bears := moveCorpusCard(t, e, "Grizzly Bears", 1, state.ZBattlefield)
	lossAttach(t, e, "Witness Protection", 0, corpse)
	lossAttach(t, e, "Frogify", 0, bears)
	if got := e.Colors(corpse); got != "WG" {
		t.Fatalf("the enchanted Walking Corpse is %q, want WG", got)
	}
	if got := e.Colors(bears); got != "U" {
		t.Fatalf("the enchanted Grizzly Bears is %q, want U", got)
	}
	moveCorpusCard(t, e, "Perish", 0, state.ZHand)
	addMana(t, e, 0, "CCB")
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 40)
	if z := e.G.Obj(corpse).Zone; z != state.ZGraveyard {
		t.Errorf("the green-and-white Walking Corpse is in %s after Perish, want the graveyard", z)
	}
	if z := e.G.Obj(bears).Zone; z != state.ZBattlefield {
		t.Errorf("the blue Grizzly Bears is in %s after Perish, want the battlefield", z)
	}
	replayCheck(t, e, cfg)
}

// lossNextTurn logs a real TurnChange, which clears CR 302.6 summoning
// sickness on the log replayCheck reads from.
func lossNextTurn(e *Engine) {
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
}
