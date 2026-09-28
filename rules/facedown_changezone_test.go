package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// ChangeZone FaceDown$ / FaceDownSetType$ (CR 708.5) pins: a card that leaves
// the battlefield and "returns to the battlefield face down" must enter as a
// real face-down permanent -- derived types and P/T from the folded set type
// (defaulting to CR 708.5's 2/2 creature), its printed face hidden while face
// down (CR 708.8), redacted to everyone but its controller, and revealed when
// it leaves again (CR 708.9). The tests drive real compiled corpus cards
// (Yedora, Grave Gardener and Shorecrasher Elemental), so no Forge script
// text is committed here.
//
// No repo deck carries any of the 12 bare-FaceDown$ ChangeZone carriers
// (asserted by TestFaceDownCarriersAreNotInRepoDecks below), so these tests do
// not move the golden heads; TestHeads guards that independently.

// facedownEngine deals a seatZeroStart two-seat game where seat 0's hand
// holds one copy of each fixture name and the rest of both decks are basics.
func facedownEngine(t *testing.T, reg *cards.Registry, hand ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range hand {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 0, 40)
	for len(opp) < 40 {
		opp = append(opp, forest)
	}
	cfg := seatZeroStart(Config{Seed: 7713, Names: []string{"facedown", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// killCreature deals lethal damage to id, runs the state-based actions and
// drains the resulting triggers, answering any KTriggerOptional offer "yes".
func killCreature(t *testing.T, e *Engine, id state.ObjID, amount int32) {
	t.Helper()
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: amount, Player: 0})
	e.checkStateBased()
	e.putTriggersOnStack()
	drainStack(t, e, 30)
}

// TestYedoraReturnsADeadCreatureAsAFaceDownForestLand is the ticket's carrier
// end to end: Yedora's death trigger returns the dying creature face down and
// its folded FaceDownSetType$ makes it exactly a Forest land -- no creature
// type, no printed abilities, derived 0/0, and (CR 305.6) tapping for {G}.
func TestYedoraReturnsADeadCreatureAsAFaceDownForestLand(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := facedownEngine(t, reg, "Yedora, Grave Gardener", "Llanowar Elves")
	searchMoveByName(t, e, "Yedora, Grave Gardener", state.ZBattlefield)
	elves := searchMoveByName(t, e, "Llanowar Elves", state.ZBattlefield)

	killCreature(t, e, elves, 2)

	o := e.G.Obj(elves)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("returned creature zone = %s, want battlefield", o.Zone)
	}
	if !o.FaceDown {
		t.Fatal("Yedora's return did not enter face down")
	}
	if o.Controller != 0 || o.Owner != 0 {
		t.Fatalf("returned controller/owner = %d/%d, want 0/0", o.Controller, o.Owner)
	}
	if o.FaceDownSetType != "Land & Forest" {
		t.Fatalf("folded set type = %q, want %q", o.FaceDownSetType, "Land & Forest")
	}

	der := e.Derived(elves)
	if len(der.Types) != 2 || der.Types[0] != "Land" || der.Types[1] != "Forest" {
		t.Fatalf("derived types = %v, want exactly [Land Forest]", der.Types)
	}
	if e.IsCreature(elves) {
		t.Fatal("Yedora's face-down Forest is still a creature")
	}
	if o.EffectiveIsCreature() {
		t.Fatal("state.Object.EffectiveIsCreature must honour the non-Creature set type")
	}
	if der.Power != 0 || der.Toughness != 0 {
		t.Fatalf("derived P/T = %d/%d, want 0/0 for a non-creature set type", der.Power, der.Toughness)
	}
	// CR 708.8: the printed Llanowar Elves face is hidden -- no printed
	// keywords survive.
	if len(der.Keywords) != 0 {
		t.Fatalf("face-down keywords = %v, want none", der.Keywords)
	}

	// CR 305.6: the Forest subtype grants its intrinsic "{T}: Add {G}", and
	// the printed Llanowar Elves ability must not add a second one.
	abs := e.availableManaAbilities(0, elves)
	if len(abs) != 1 {
		t.Fatalf("face-down Forest mana abilities = %d, want exactly 1 (the intrinsic {G})", len(abs))
	}
	if got := abs[0].Params["Produced"]; got != "G" {
		t.Fatalf("intrinsic mana ability produces %q, want G", got)
	}

	// The view redacts the face-down permanent to the non-controller and shows
	// it to its controller (the CR 708.5 "owner may look" rule).
	oppView := view.Project(e.G, e, 1, nil)
	var blank *view.CardView
	for i := range oppView.Players[0].Battlefield {
		if oppView.Players[0].Battlefield[i].ID == elves {
			blank = &oppView.Players[0].Battlefield[i]
		}
	}
	if blank == nil || blank.Name != "" || !blank.FaceDown {
		t.Fatalf("opponent's view of the face-down land = %+v, want the redacted blank", blank)
	}
	ctrlView := view.Project(e.G, e, 0, nil)
	var own *view.CardView
	for i := range ctrlView.Players[0].Battlefield {
		if ctrlView.Players[0].Battlefield[i].ID == elves {
			own = &ctrlView.Players[0].Battlefield[i]
		}
	}
	if own == nil || own.Name != "Llanowar Elves" || !own.FaceDown {
		t.Fatalf("controller's view of its face-down land = %+v, want name Llanowar Elves", own)
	}

	// CR 708.9: leaving the battlefield reveals it and restores the printed
	// face.
	e.emit(events.Event{Kind: events.MoveZone, Obj: elves, From: state.ZBattlefield, To: state.ZGraveyard})
	back := e.G.Obj(elves)
	if back.FaceDown {
		t.Fatal("FaceDown did not clear when the face-down land left the battlefield")
	}
	if back.FaceDownSetType != "" {
		t.Fatalf("folded set type = %q after leaving, want cleared", back.FaceDownSetType)
	}
	if !e.IsCreature(elves) {
		t.Fatal("the revealed card is not the printed Llanowar Elves creature")
	}

	replayCheck(t, e, cfg)
}

// TestShorecrasherElementalReturnsItselfFaceDown pins the CorrectedSelf
// selector and the face-down entry together: activating Shorecrasher
// Elemental's {U} ability exiles it, and the chained DBReturn (Defined$
// CorrectedSelf) returns that same object face down as CR 708.5's 2/2
// creature.
func TestShorecrasherElementalReturnsItselfFaceDown(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := facedownEngine(t, reg, "Shorecrasher Elemental")
	id := searchMoveByName(t, e, "Shorecrasher Elemental", state.ZBattlefield)

	idx := -1
	for i, sa := range e.G.Obj(id).Face().Abilities {
		if sa.Kind == "AB" && sa.API == "ChangeZone" && sa.Params["Cost"] == "U" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("Shorecrasher Elemental has no {U} ChangeZone ability")
	}
	addMana(t, e, 0, "U")
	submitChoices(t, e, abilityOption(t, e, id, idx).Index)
	passUntilStackEmpty(t, e, 30)

	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("returned zone = %s, want battlefield", o.Zone)
	}
	if !o.FaceDown {
		t.Fatal("DBReturn (Defined$ CorrectedSelf) did not return the card face down")
	}
	if o.FaceDownSetType != "" {
		t.Fatalf("set type = %q, want the plain CR 708.5 face", o.FaceDownSetType)
	}
	der := e.Derived(id)
	if der.Power != 2 || der.Toughness != 2 {
		t.Fatalf("derived P/T = %d/%d, want 2/2", der.Power, der.Toughness)
	}
	if len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("derived types = %v, want exactly [Creature]", der.Types)
	}
	if !e.IsCreature(id) {
		t.Fatal("the returned face-down card is not a creature")
	}

	replayCheck(t, e, cfg)
}

// TestMagarReturnsASpellAsA33FaceDownCreature pins the FaceDownPower$/
// FaceDownToughness$ pair riding the same set-type fold: Magar's activated
// ability puts a graveyard instant face down as a 3/3 creature.
func TestMagarReturnsASpellAsA33FaceDownCreature(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := facedownEngine(t, reg, "Magar of the Magic Strings", "Lightning Bolt")
	magar := searchMoveByName(t, e, "Magar of the Magic Strings", state.ZBattlefield)
	bolt := searchMoveByName(t, e, "Lightning Bolt", state.ZGraveyard)

	idx := -1
	for i, sa := range e.G.Obj(magar).Face().Abilities {
		if sa.Kind == "AB" && sa.API == "ChangeZone" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("Magar has no ChangeZone ability")
	}
	addMana(t, e, 0, "BR")
	addMana(t, e, 0, "1")
	d := e.Pending()
	opt, ok := findAbilityOption(e, magar, idx)
	if !ok {
		t.Fatalf("Magar's ability not offered: %+v", d.Options)
	}
	submitChoices(t, e, opt.Index)
	// The target ask names the graveyard instant.
	if td := e.Pending(); td != nil && td.Kind == decision.KTarget {
		tIdx := -1
		for _, o := range td.Options {
			if o.Obj == bolt {
				tIdx = o.Index
			}
		}
		if tIdx < 0 {
			t.Fatalf("Lightning Bolt not offered as a target: %+v", td.Options)
		}
		submitChoices(t, e, tIdx)
	}
	passUntilStackEmpty(t, e, 40)

	o := e.G.Obj(bolt)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("returned spell zone = %s, want battlefield", o.Zone)
	}
	if !o.FaceDown {
		t.Fatal("Magar's return did not enter face down")
	}
	if o.FaceDownSetType != "Creature" {
		t.Fatalf("folded set type = %q, want Creature", o.FaceDownSetType)
	}
	der := e.Derived(bolt)
	if der.Power != 3 || der.Toughness != 3 {
		t.Fatalf("derived P/T = %d/%d, want the folded 3/3", der.Power, der.Toughness)
	}
	if len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("derived types = %v, want exactly [Creature]", der.Types)
	}

	replayCheck(t, e, cfg)
}

// TestFaceDownExileHasNoExiledWithAssociation pins that a bare ChangeZone
// FaceDown$ True exile marks the card face down WITHOUT claiming the exiling
// source (Tezzeret's Reckoning: the line names no exiling source). It is done
// at the event decode so the whole shared marker path is covered.
func TestFaceDownExileHasNoExiledWithAssociation(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := facedownEngine(t, reg, "Grizzly Bears", "Shorecrasher Elemental")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)

	// The marker's bare-FaceDown$ exile spelling.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield,
		To: state.ZExile, Counter: "face_down"})
	o := e.G.Obj(bear)
	if !o.FaceDown {
		t.Fatal("bare FaceDown$ exile did not mark the card face down")
	}
	if o.ExiledWith != 0 {
		t.Fatalf("ExiledWith = %d, want 0: the bare spelling claims no exiling source", o.ExiledWith)
	}

	// ExileFaceDown$ keeps its source-carrying association.
	other := searchMoveByName(t, e, "Shorecrasher Elemental", state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: other, From: state.ZBattlefield,
		To: state.ZExile, Counter: "exiled_with_face_down", Amount: int32(bear)})
	if e.G.Obj(other).ExiledWith != bear {
		t.Fatalf("ExileFaceDown$ ExiledWith = %d, want %d", e.G.Obj(other).ExiledWith, bear)
	}
}

// TestFaceDownCarriersAreNotInRepoDecks asserts the discipline the head pins
// rely on: none of the 12 bare-FaceDown$ ChangeZone carriers is in a deck the
// golden seat-count games seat from, so these tests cannot move TestHeads.
//
// The pool checked is LegacyDeckNames -- the CLOSED 12-deck list
// rules/heads_test.go's TestHeads seats through playAcceptance -- not
// RepoDeckNames. The head pins are byte-identical games over those 12 only
// (acceptance_test.go: "The pool is LegacyDeckNames, never RepoDeckNames"),
// and a Commander precon imported later may legitimately carry a carrier
// without touching a single pinned game: the Deadly Disguise precon does
// (Ashcloud Phoenix, Deathmist Raptor, Yedora, Grave Gardener).
// TestFaceDownCarriersAreOutsideTheHeadPinnedPool pins that sweep from the
// deck import's side. This test fails loudly if a HEAD-PINNED deck ever gains
// one of the carriers, which is the regression it exists to catch.
func TestFaceDownCarriersAreNotInRepoDecks(t *testing.T) {
	t.Parallel()
	pool := testutil.LegacyDeckNames()
	// PRECONDITION: the head-pinned pool is non-empty, so the scan below
	// cannot pass vacuously if the pool ever empties.
	if len(pool) == 0 {
		t.Fatal("LegacyDeckNames() is empty; the head-pin carrier scan would pass vacuously")
	}
	for _, name := range pool {
		f, err := testutil.LoadRepoDeckFile(name)
		if err != nil {
			t.Fatalf("load deck %s: %v", name, err)
		}
		for _, c := range f.Cards {
			if faceDownChangeZoneCarriers[c.Name] {
				t.Errorf("deck %s carries face-down ChangeZone carrier %q; the face-down head pins are no longer safe", name, c.Name)
			}
		}
	}
}

// faceDownChangeZoneCarriers is the set the face-down pins act on: every
// corpus card that returns a permanent to the battlefield with a bare
// FaceDown$ True ChangeZone. Kept in one place so the head-pin discipline
// test and the deck-import sweep cannot drift apart.
var faceDownChangeZoneCarriers = map[string]bool{
	"Ashcloud Phoenix":            true,
	"Deathmist Raptor":            true,
	"Yedora, Grave Gardener":      true,
	"Yarus, Roar of the Old Gods": true,
	"Shorecrasher Elemental":      true,
	"Magar of the Magic Strings":  true,
	"Missy":                       true,
	"Tezzeret, Cruel Machinist":   true,
	"The Moonbase":                true,
	"The Cyber-Controller":        true,
	"Tezzeret's Reckoning":        true,
	"Cybership":                   true,
	"Death in Heaven":             true,
}
