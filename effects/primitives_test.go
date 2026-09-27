package effects

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// mkCard parses and links a standalone card for a test's own board, applying
// intrinsics the same way board(t) does in filter_test.go.
func mkCard(t *testing.T, src string) *cards.Card {
	t.Helper()
	c, d := cards.ParseBytes("t.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	return c
}

// fillLibrary adds n fresh copies of c to p's library, in the order they end
// up on top (index 0 first), and returns their IDs.
func fillLibrary(g *state.Game, p state.PlayerID, c *cards.Card, n int) []state.ObjID {
	var ids []state.ObjID
	for i := 0; i < n; i++ {
		ids = append(ids, g.AddObject(c, p).ID)
	}
	g.SetZone(state.ZLibrary, p, ids)
	return ids
}

// twoFacedCard is a minimal double-faced fixture: parse.go starts a new Face
// on an "ALTERNATE" line.
func twoFacedCard(t *testing.T) *cards.Card {
	t.Helper()
	return mkCard(t, "Name:Front\nTypes:Creature\nPT:1/1\nOracle:x\n\nALTERNATE\n\nName:Back\nTypes:Creature\nPT:3/3\nOracle:x\n")
}

func TestDealDamageHitsATargetedPlayer(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[1].Life = 20
	c := &Ctx{Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}
	Resolve(h, c, sa(t, "SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3"))
	if h.g.Players[1].Life != 17 {
		t.Fatalf("life = %d, want 17", h.g.Players[1].Life)
	}
}

func TestDealDamageMarksAPermanent(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}}
	Resolve(h, c, sa(t, "SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2"))
	if got := g.Obj(ids["myBear"]).Damage; got != 2 {
		t.Fatalf("marked damage = %d, want 2", got)
	}
}

func TestDealDamageDefaultsMissingNumDmgToZero(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[1].Life = 20
	c := &Ctx{Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}}
	Resolve(h, c, sa(t, "SP$ DealDamage | ValidTgts$ Any"))
	if h.g.Players[1].Life != 20 {
		t.Fatalf("life = %d, want unchanged at 20", h.g.Players[1].Life)
	}
}

func TestAddManaFillsTheControllersPool(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0}
	Resolve(h, c, sa(t, "AB$ Mana | Cost$ T | Produced$ R | Amount$ 1"))
	if h.g.Players[0].Pool[state.MR] != 1 {
		t.Fatalf("pool = %v, want 1 red", h.g.Players[0].Pool)
	}
	// The other seat's pool must be untouched.
	if h.g.Players[1].Pool.Total() != 0 {
		t.Fatalf("player 1 pool = %v, want empty", h.g.Players[1].Pool)
	}
}

func TestAddManaDefaultsToColorlessAndAmountOne(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0}
	Resolve(h, c, sa(t, "AB$ Mana | Cost$ T"))
	if h.g.Players[0].Pool[state.MC] != 1 {
		t.Fatalf("pool = %v, want 1 colourless", h.g.Players[0].Pool)
	}
}

// TestDealDamageClampsNegativeNumDmgToZero is Ruling T14-f's regression test:
// NumDmg$ is Num()'s unclamped output, and events.Apply's Damage case is a
// plain subtraction from Life, so a negative NumDmg would otherwise heal
// instead of doing nothing. No real card text ever specifies this, but the
// inversion (a damage spell that heals) is stark enough to close directly.
func TestDealDamageClampsNegativeNumDmgToZero(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[1].Life = 20
	c := &Ctx{Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}
	Resolve(h, c, sa(t, "SP$ DealDamage | ValidTgts$ Any | NumDmg$ -5"))
	if h.g.Players[1].Life != 20 {
		t.Fatalf("life = %d, want unchanged at 20 (negative damage must not heal)", h.g.Players[1].Life)
	}
}

// TestAddManaClampsNegativeAmountToZero is Mana's half of Ruling T14-f: a
// negative Amount$ would otherwise drop the pool below zero.
func TestAddManaClampsNegativeAmountToZero(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0}
	Resolve(h, c, sa(t, "AB$ Mana | Cost$ T | Produced$ R | Amount$ -3"))
	if h.g.Players[0].Pool[state.MR] != 0 {
		t.Fatalf("pool = %v, want unchanged at 0 (negative amount must not go below zero)", h.g.Players[0].Pool)
	}
}

// TestPrimitivesAreRegistered guards against a silent unregistration: if any
// of these were ever dropped, Resolve would fall back to emitting a Note
// event ("unimplemented API ...") instead of the real effect, and the tests
// covering it would need to fail loudly rather than quietly pass on a no-op.
// Token and CopySpellAbility are deliberately absent -- see
// TestTokenAndCopySpellAbilityAreNotYetRegistered.
func TestPrimitivesAreRegistered(t *testing.T) {
	sup := Supported()
	for _, api := range []string{
		"DealDamage", "DamageAll", "Mana",
		"Draw", "Discard", "Mill", "Dig", "DigUntil", "Reveal", "RevealHand", "PeekAndReveal",
		"RearrangeTopOfLibrary", "Scry", "Surveil", "NameCard", "ChooseType", "ChooseNumber",
		"ChangeZone", "ChangeZoneAll", "Destroy", "DestroyAll", "Sacrifice", "Seek",
		"GainLife", "LoseLife", "GainControlVariant",
		"PutCounter", "RemoveCounterAll", "Regenerate",
		"Tap", "Pump", "PumpAll", "Animate", "AnimateAll", "Protection",
		"Effect", "Cleanup", "SetState", "Counter", "DelayedTrigger", "Repeat",
		"Charm", "Vote", "BecomeMonarch", "RestartGame", "Earthbend",
	} {
		if !sup["api:"+api] {
			t.Fatalf("api:%s not registered", api)
		}
	}
}

// ---------------------------------------------------------------------------
// damage.go

// TestDealDamageHitsPlayersAndPermanents is the brief's own worked-example
// regression test.
func TestDealDamageHitsPlayersAndPermanents(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}
	Resolve(h, c, sa(t, "SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3"))
	if g.Players[1].Life != 17 {
		t.Fatalf("life = %d, want 17", g.Players[1].Life)
	}
	c.Targets = []state.Target{{Obj: ids["theirBig"]}}
	Resolve(h, c, sa(t, "SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2"))
	if g.Obj(ids["theirBig"]).Damage != 2 {
		t.Fatalf("damage = %d", g.Obj(ids["theirBig"]).Damage)
	}
}

// TestDealDamageIgnoresObjectsOffTheBattlefield is the enhancement Task 18
// folds into Task 14's stopgap: a permanent that left the battlefield (in
// response, say) is no longer a legal recipient.
func TestDealDamageIgnoresObjectsOffTheBattlefield(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	moveTo(g, ids["myBear"], state.ZGraveyard)
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}},
		sa(t, "SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 3"))
	if g.Obj(ids["myBear"]).Damage != 0 {
		t.Fatal("damage applied to a card in the graveyard")
	}
}

func TestDamageAllHitsMatchingCreaturesOnly(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DamageAll | ValidCards$ Creature | NumDmg$ 1"))
	for _, name := range []string{"myBear", "myFlier", "theirBig"} {
		if got := g.Obj(ids[name]).Damage; got != 1 {
			t.Errorf("%s damage = %d, want 1", name, got)
		}
	}
	if g.Obj(ids["myLand"]).Damage != 0 {
		t.Error("DamageAll's explicit ValidCards$ Creature must not hit a land")
	}
}

// ---------------------------------------------------------------------------
// cardflow.go

func TestDrawPutsTopLibraryCardsInHand(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	ids := fillLibrary(h.g, 0, bear, 3)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ Draw | Defined$ You | NumCards$ 2"))
	hand := h.g.Zone(state.ZHand, 0)
	if len(hand) != 2 || hand[0] != ids[0] || hand[1] != ids[1] {
		t.Fatalf("hand = %v, want [%d %d]", hand, ids[0], ids[1])
	}
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 1 || lib[0] != ids[2] {
		t.Fatalf("library = %v, want [%d]", lib, ids[2])
	}
}

func TestDrawFromEmptyLibraryLosesTheGame(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ Draw | Defined$ You"))
	if !h.g.Players[0].Lost {
		t.Fatal("drawing from an empty library must lose the game")
	}
}

func TestDiscardMovesFromHandToGraveyard(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	o := h.g.AddObject(bear, 0)
	o.Zone = state.ZHand
	h.g.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ Discard | Defined$ You | NumCards$ 1"))
	if len(h.g.Zone(state.ZHand, 0)) != 0 {
		t.Fatal("card was not discarded from hand")
	}
	if gy := h.g.Zone(state.ZGraveyard, 0); len(gy) != 1 || gy[0] != o.ID {
		t.Fatalf("graveyard = %v, want [%d]", gy, o.ID)
	}
}

func TestMillMovesLibraryTopToGraveyard(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	ids := fillLibrary(h.g, 0, bear, 3)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ Mill | Defined$ You | NumCards$ 2"))
	if gy := h.g.Zone(state.ZGraveyard, 0); len(gy) != 2 || gy[0] != ids[0] || gy[1] != ids[1] {
		t.Fatalf("graveyard = %v, want [%d %d]", gy, ids[0], ids[1])
	}
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 1 || lib[0] != ids[2] {
		t.Fatalf("library = %v, want [%d]", lib, ids[2])
	}
}

func TestDigMovesMatchingCardsToDestinationLeavingTheRestOnTop(t *testing.T) {
	h := newHost(t, 2)
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	g := h.g
	c0 := g.AddObject(bear, 0).ID
	c1 := g.AddObject(land, 0).ID
	c2 := g.AddObject(bear, 0).ID
	g.SetZone(state.ZLibrary, 0, []state.ObjID{c0, c1, c2})

	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | ChangeValid$ Land | DestinationZone$ Hand"))

	if hand := g.Zone(state.ZHand, 0); len(hand) != 1 || hand[0] != c1 {
		t.Fatalf("hand = %v, want [%d] (the land)", hand, c1)
	}
	if lib := g.Zone(state.ZLibrary, 0); len(lib) != 2 || lib[0] != c0 || lib[1] != c2 {
		t.Fatalf("library = %v, want [%d %d], unchanged relative order", lib, c0, c2)
	}
}

// TestDigChangeNumAllMovesEveryMatchingCardOfTheTopN is Task 5's regression
// test for "ChangeNum$ All" (e.g. Goblin Guide's own Dig): every matching
// card within the top DigNum cards moves, not just the first ChangeNum of
// them, with no cap short of DigNum itself. A card outside the DigNum window
// is never even inspected, let alone moved. The one-card remainder (the bear)
// takes the default bottom destination without an ask -- one card has exactly
// one possible order -- so it lands BELOW the untouched library.
func TestDigChangeNumAllMovesEveryMatchingCardOfTheTopN(t *testing.T) {
	h := newHost(t, 2)
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	g := h.g
	c0 := g.AddObject(land, 0).ID
	c1 := g.AddObject(bear, 0).ID
	c2 := g.AddObject(land, 0).ID
	c3 := g.AddObject(bear, 0).ID // outside DigNum$ 3, never inspected
	g.SetZone(state.ZLibrary, 0, []state.ObjID{c0, c1, c2, c3})

	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ All | ChangeValid$ Land | DestinationZone$ Hand"))

	if hand := g.Zone(state.ZHand, 0); len(hand) != 2 || hand[0] != c0 || hand[1] != c2 {
		t.Fatalf("hand = %v, want [%d %d] (both lands in the top 3)", hand, c0, c2)
	}
	if lib := g.Zone(state.ZLibrary, 0); len(lib) != 2 || lib[0] != c3 || lib[1] != c1 {
		t.Fatalf("library = %v, want [%d %d] (the bear remainder on the bottom, below the untouched card)", lib, c3, c1)
	}
}

// TestDigsSecretMoveNamesTheLibraryOwner is Task 23 §11's regression test: a
// pre-existing leak. effDig's MoveZone event for a card taken from a hidden
// library was emitted Secret with no Player at all, so under the redaction
// contract (Player names the seat whose secret this is) PlayerID(0) -- a
// real seat, not "nobody" -- ended up seeing every other seat's dug card
// while the actual owner lost their own. Player must be the library's
// owner, the same seat DrawFor (line 48) and rules/engine.go's Shuffle
// already carry.
func TestDigsSecretMoveNamesTheLibraryOwner(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	fillLibrary(h.g, 1, bear, 3)
	Resolve(h, &Ctx{Controller: 1},
		sa(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | ChangeValid$ Creature | DestinationZone$ Hand"))

	var found bool
	for _, e := range h.log {
		if e.Kind != events.MoveZone || !e.Secret {
			continue
		}
		found = true
		if e.Player != 1 {
			t.Fatalf("Dig's Secret MoveZone has Player %d, want the library's owner (1)", e.Player)
		}
	}
	if !found {
		t.Fatal("no Secret MoveZone event was emitted")
	}
}

// TestRearrangeTopOfLibraryNoteIsSecretToItsOwner is fix round 2's
// regression test (Ruling T23-w): the private look at the top of the
// library must be Secret, with Player naming its owner -- the same shape
// every other hidden-zone emitter in this file already uses (DrawFor,
// effDig above). Player 1 (non-zero), not 0, so a bug that silently reads
// PlayerID's zero value cannot pass by coincidence.
func TestRearrangeTopOfLibraryNoteIsSecretToItsOwner(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	fillLibrary(h.g, 1, bear, 3)
	Resolve(h, &Ctx{Controller: 1}, sa(t, "SP$ RearrangeTopOfLibrary | Defined$ You | NumCards$ 3"))

	var found bool
	for _, e := range h.log {
		if e.Kind != events.Note {
			continue
		}
		found = true
		if !e.Secret {
			t.Fatalf("RearrangeTopOfLibrary's Note is not Secret: %+v", e)
		}
		if e.Player != 1 {
			t.Fatalf("RearrangeTopOfLibrary's Note has Player %d, want the library's owner (1)", e.Player)
		}
	}
	if !found {
		t.Fatal("no Note event was emitted")
	}
}

func TestRevealRecordsIdentitiesWithoutMovingCards(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	o := h.g.AddObject(bear, 0)
	o.Zone = state.ZHand
	h.g.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ Reveal | Defined$ You | NumCards$ 1"))
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 1 || hand[0] != o.ID {
		t.Fatal("Reveal must not move the revealed card")
	}
	var found bool
	for _, e := range h.log {
		if e.Kind == events.Note && !e.Secret && len(e.IDs) == 1 && e.IDs[0] == o.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("no non-secret Note recorded the revealed card's identity")
	}
}

func TestPeekAndRevealLooksAtTheLibrary(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	ids := fillLibrary(h.g, 0, bear, 2)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ PeekAndReveal | Defined$ You | NumCards$ 1"))
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 2 {
		t.Fatal("PeekAndReveal must not move library cards")
	}
	var found bool
	for _, e := range h.log {
		if e.Kind == events.Note && len(e.IDs) == 1 && e.IDs[0] == ids[0] {
			found = true
		}
	}
	if !found {
		t.Fatal("no Note recorded the peeked card")
	}
}

func TestRearrangeTopOfLibraryKeepsExistingOrder(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	ids := fillLibrary(h.g, 0, bear, 3)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ RearrangeTopOfLibrary | Defined$ You | NumCards$ 3"))
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 3 || lib[0] != ids[0] || lib[1] != ids[1] || lib[2] != ids[2] {
		t.Fatalf("library = %v, want unchanged %v", lib, ids)
	}
}

// ---------------------------------------------------------------------------
// zone.go

func TestChangeZoneMovesTheTarget(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}, TargetsOffered: true},
		sa(t, "SP$ ChangeZone | ValidTgts$ Permanent | Destination$ Hand"))
	if g.Obj(ids["myBear"]).Zone != state.ZHand {
		t.Fatalf("zone = %v, want Hand", g.Obj(ids["myBear"]).Zone)
	}
}

// TestChangeZoneSkipsObjectNoLongerAtOrigin is a CR 608.2b-flavoured guard:
// Origin$ is a precondition, so a target that already left where the effect
// expected it (destroyed in response, say) is skipped rather than moved from
// the wrong place. The ctx carries the TargetsOffered marker (task spcz1:
// changeZoneChosenTargets inherits pre-chosen targets only when the marker
// says they are this SA's own -- a live placement ask always sets it), so the
// chosen target is read instead of an ask being posed.
func TestChangeZoneSkipsObjectNoLongerAtOrigin(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	moveTo(g, ids["myBear"], state.ZGraveyard)
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}, TargetsOffered: true},
		sa(t, "SP$ ChangeZone | ValidTgts$ Permanent | Origin$ Battlefield | Destination$ Exile"))
	if g.Obj(ids["myBear"]).Zone != state.ZGraveyard {
		t.Fatalf("zone = %v, want unchanged Graveyard", g.Obj(ids["myBear"]).Zone)
	}
	for _, e := range h.log {
		if e.Kind == events.MoveZone {
			t.Fatal("a move event should not have been emitted at all")
		}
	}
}

func TestChangeZoneAllSweepsMatchingLibraryCards(t *testing.T) {
	h := newHost(t, 2)
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	g := h.g
	landID := g.AddObject(land, 0).ID
	bearID := g.AddObject(bear, 0).ID
	g.SetZone(state.ZLibrary, 0, []state.ObjID{landID, bearID})

	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"SP$ ChangeZoneAll | Origin$ Library | Destination$ Hand | ChangeType$ Land"))

	if hand := g.Zone(state.ZHand, 0); len(hand) != 1 || hand[0] != landID {
		t.Fatalf("hand = %v, want [%d]", hand, landID)
	}
	if lib := g.Zone(state.ZLibrary, 0); len(lib) != 1 || lib[0] != bearID {
		t.Fatalf("library = %v, want [%d] (bear untouched)", lib, bearID)
	}
}

// TestChangeZoneAllScope pins Forge ChangeZoneAllEffect's player scope: a
// ChangeZoneAll that declares a target or a Defined$ acts only on those
// players' zones; UseAllOriginZones$ True (and the no-target/no-Defined$
// default) sweeps every player. Before the fix effChangeZoneAll always walked
// g.AliveFrom(0), so "exile target player's graveyard" exiled every player's
// graveyard. Both seats are stocked in every case.
func TestChangeZoneAllScope(t *testing.T) {
	stock := func(t *testing.T) (*fakeHost, map[state.PlayerID]state.ObjID) {
		t.Helper()
		h := newHost(t, 2)
		card := mkCard(t, "Name:Relic\nTypes:Artifact\nOracle:x\n")
		ids := make(map[state.PlayerID]state.ObjID, 2)
		for p := state.PlayerID(0); p < 2; p++ {
			o := h.g.AddObject(card, p)
			o.Zone = state.ZGraveyard
			h.g.SetZone(state.ZGraveyard, p, []state.ObjID{o.ID})
			ids[p] = o.ID
		}
		return h, ids
	}
	cases := []struct {
		name    string
		line    string
		targets []state.Target
		moved   [2]bool // wanted: seat p's graveyard emptied?
	}{
		{"target player only",
			"SP$ ChangeZoneAll | ValidTgts$ Player | Origin$ Graveyard | Destination$ Exile | ChangeType$ Card",
			[]state.Target{{Player: 1, IsPlayer: true}}, [2]bool{false, true}},
		{"defined you only",
			"SP$ ChangeZoneAll | Defined$ You | Origin$ Graveyard | Destination$ Exile | ChangeType$ Card",
			nil, [2]bool{true, false}},
		{"flag sweeps all players",
			"SP$ ChangeZoneAll | Origin$ Graveyard | Destination$ Exile | ChangeType$ Card | UseAllOriginZones$ True",
			nil, [2]bool{true, true}},
		{"no target no defined sweeps all players",
			"SP$ ChangeZoneAll | Origin$ Graveyard | Destination$ Exile | ChangeType$ Card",
			nil, [2]bool{true, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, ids := stock(t)
			// TargetsOffered models the cast/trigger placement ask having
			// already filled Ctx.Targets: without it the generic ValidTgts$
			// pre-ask fires and its no-host stand-in picks candidate 0.
			Resolve(h, &Ctx{Controller: 0, Targets: tc.targets, TargetsOffered: true}, sa(t, tc.line))
			for p := state.PlayerID(0); p < 2; p++ {
				left := len(h.g.Zone(state.ZGraveyard, p))
				if tc.moved[p] {
					if left != 0 {
						t.Fatalf("seat %d graveyard not emptied: %d cards left", p, left)
					}
					if o := h.g.Obj(ids[p]); o == nil || o.Zone != state.ZExile {
						t.Fatalf("seat %d card %d not exiled: %+v", p, ids[p], o)
					}
				} else if left != 1 {
					t.Fatalf("seat %d graveyard over-swept: %d cards left, want 1 (card %d untouched)", p, left, ids[p])
				}
			}
		})
	}
}

func TestDestroyMovesToGraveyard(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}}, sa(t, "SP$ Destroy | ValidTgts$ Creature"))
	if g.Obj(ids["myBear"]).Zone != state.ZGraveyard {
		t.Fatal("destroyed permanent did not move to the graveyard")
	}
}

// TestDestroySkipsIndestructible is the brief's own worked-example test.
func TestDestroyRememberTargetsRecordsDestroyedSurvivorsOnly(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	source := g.Obj(ids["myLand"])
	if source == nil || source.Zone != state.ZBattlefield {
		t.Fatal("remembering source must start on the battlefield")
	}
	old := ids["theirBig"]
	source.Remembered = []state.Target{{Obj: old}}
	ctx := &Ctx{Source: source.ID, Controller: 0,
		Targets: []state.Target{{Obj: ids["myBear"]}, {Obj: ids["myFlier"]}}, TargetsOffered: true,
		Remembered: []state.Target{{Obj: old}}}
	g.Obj(ids["myFlier"]).Card.Faces[0].Keywords = append(g.Obj(ids["myFlier"]).Card.Faces[0].Keywords, "Indestructible")
	Resolve(h, ctx, sa(t, "SP$ Destroy | ValidTgts$ Creature | RememberTargets$ True | ForgetOtherTargets$ True"))
	if g.Obj(ids["myBear"]).Zone != state.ZGraveyard {
		t.Fatal("destroyed target did not leave the battlefield")
	}
	if g.Obj(ids["myFlier"]).Zone != state.ZBattlefield {
		t.Fatal("indestructible target was not spared")
	}
	if len(ctx.Remembered) != 1 || ctx.Remembered[0].Obj != ids["myBear"] {
		t.Fatalf("ctx remembered = %#v, want only destroyed target", ctx.Remembered)
	}
	if len(source.Remembered) != 1 || source.Remembered[0].Obj != ids["myBear"] {
		t.Fatalf("source remembered = %#v, want only destroyed target", source.Remembered)
	}
}

func TestDestroySkipsIndestructible(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	o := g.Obj(ids["myBear"])
	o.Card.Faces[0].Keywords = append(o.Card.Faces[0].Keywords, "Indestructible")
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: o.ID}}}, sa(t, "SP$ Destroy | ValidTgts$ Creature"))
	if o.Zone != state.ZBattlefield {
		t.Fatal("indestructible permanent was destroyed")
	}
	for _, e := range h.log {
		if e.Kind == events.MoveZone {
			t.Fatal("a move event should not have been emitted at all")
		}
	}
}

func TestDestroyAllSweepsSkippingIndestructible(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	g.Obj(ids["theirBig"]).Card.Faces[0].Keywords = append(g.Obj(ids["theirBig"]).Card.Faces[0].Keywords, "Indestructible")
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DestroyAll | ValidCards$ Creature"))
	if g.Obj(ids["myBear"]).Zone != state.ZGraveyard {
		t.Error("myBear should have been destroyed")
	}
	if g.Obj(ids["myFlier"]).Zone != state.ZGraveyard {
		t.Error("myFlier should have been destroyed")
	}
	if g.Obj(ids["theirBig"]).Zone != state.ZBattlefield {
		t.Error("indestructible theirBig should have survived")
	}
	if g.Obj(ids["myLand"]).Zone != state.ZBattlefield {
		t.Error("DestroyAll's Creature filter must not touch the land")
	}
}

func TestSacrificeIgnoresIndestructible(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	o := g.Obj(ids["myBear"])
	o.Card.Faces[0].Keywords = append(o.Card.Faces[0].Keywords, "Indestructible")
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: o.ID}}}, sa(t, "SP$ Sacrifice | ValidTgts$ Creature"))
	if o.Zone != state.ZGraveyard {
		t.Fatal("sacrifice must ignore Indestructible")
	}
}

// ---------------------------------------------------------------------------
// life.go

func TestGainLifeIncreasesLife(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 20
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ GainLife | Defined$ You | LifeAmount$ 3"))
	if h.g.Players[0].Life != 23 {
		t.Fatalf("life = %d, want 23", h.g.Players[0].Life)
	}
}

func TestLoseLifeDecreasesLife(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 20
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ LoseLife | Defined$ You | LifeAmount$ 5"))
	if h.g.Players[0].Life != 15 {
		t.Fatalf("life = %d, want 15", h.g.Players[0].Life)
	}
}

func TestGainLifeClampsNegativeAmountToZero(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 20
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ GainLife | Defined$ You | LifeAmount$ -4"))
	if h.g.Players[0].Life != 20 {
		t.Fatalf("life = %d, want unchanged at 20 (negative gain must not lose life)", h.g.Players[0].Life)
	}
}

func TestLoseLifeClampsNegativeAmountToZero(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 20
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ LoseLife | Defined$ You | LifeAmount$ -4"))
	if h.g.Players[0].Life != 20 {
		t.Fatalf("life = %d, want unchanged at 20 (negative loss must not gain life)", h.g.Players[0].Life)
	}
}

// ---------------------------------------------------------------------------
// counters.go

func TestPutCounterAddsToTarget(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}},
		sa(t, "SP$ PutCounter | ValidTgts$ Creature | CounterType$ P1P1 | CounterNum$ 2"))
	if got := g.Obj(ids["myBear"]).Counter("P1P1"); got != 2 {
		t.Fatalf("P1P1 counters = %d, want 2", got)
	}
}

func TestPutCounterDefaultsToOneP1P1(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}}, sa(t, "SP$ PutCounter | ValidTgts$ Creature"))
	if got := g.Obj(ids["myBear"]).Counter("P1P1"); got != 1 {
		t.Fatalf("P1P1 counters = %d, want 1", got)
	}
}

func TestRemoveCounterAllRemovesFromMatchingPermanentsOnly(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	g.Obj(ids["myBear"]).AddCounter("P1P1", 3)
	g.Obj(ids["myFlier"]).AddCounter("P1P1", 3)
	g.Obj(ids["theirBig"]).AddCounter("P1P1", 3)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ RemoveCounterAll | ValidCards$ Creature.YouCtrl | CounterType$ P1P1 | CounterNum$ 1"))
	if got := g.Obj(ids["myBear"]).Counter("P1P1"); got != 2 {
		t.Errorf("myBear P1P1 = %d, want 2", got)
	}
	if got := g.Obj(ids["myFlier"]).Counter("P1P1"); got != 2 {
		t.Errorf("myFlier P1P1 = %d, want 2", got)
	}
	if got := g.Obj(ids["theirBig"]).Counter("P1P1"); got != 3 {
		t.Errorf("theirBig P1P1 = %d, want unchanged at 3", got)
	}
}

func TestRemoveCounterAllHonoursAllCounters(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	g.Obj(ids["myBear"]).AddCounter("P1P1", 5)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ RemoveCounterAll | ValidCards$ Creature.YouCtrl | CounterType$ P1P1 | AllCounters$ True"))
	if got := g.Obj(ids["myBear"]).Counter("P1P1"); got != 0 {
		t.Fatalf("P1P1 counters = %d, want 0 (AllCounters$ True removes everything)", got)
	}
}

func TestRegenerateGrantsAShieldCounter(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}}, sa(t, "AB$ Regenerate | ValidTgts$ Creature"))
	if got := g.Obj(ids["myBear"]).Counter("Shield"); got != 1 {
		t.Fatalf("Shield counters = %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// combatfx.go

func TestTapTapsUntappedTargetOnly(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}}, sa(t, "AB$ Tap | ValidTgts$ Creature"))
	if !g.Obj(ids["myBear"]).Tapped {
		t.Fatal("target was not tapped")
	}
	before := len(h.log)
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}}, sa(t, "AB$ Tap | ValidTgts$ Creature"))
	if len(h.log) != before {
		t.Fatal("tapping an already-tapped permanent should not emit a second Tap event")
	}
}

// TestPumpRegistersLayerContinuousEffectsOnTheTarget is the Task 19c
// regression test: Pump must build real state.ContinuousEffect values and
// hand them to the Host, not just log a Note, so the layer system Task 19
// built actually has a caller. Power/Toughness and a granted keyword go on
// separate layers (7c and 6), so they are two effects, not one.
func TestPumpRegistersLayerContinuousEffectsOnTheTarget(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}},
		sa(t, "AB$ Pump | ValidTgts$ Creature | NumAtt$ +2 | NumDef$ +1 | KW$ Flying"))
	if len(h.continuous) != 2 {
		t.Fatalf("continuous = %+v, want 2 effects", h.continuous)
	}
	pt := h.continuous[0]
	if pt.Source != ids["myBear"] || pt.Affects != "Card.Self" || pt.Layer != state.LPT ||
		pt.Sub != state.SubModify || pt.AddPower != 2 || pt.AddToughness != 1 || !pt.UntilEOT {
		t.Fatalf("P/T continuous effect = %+v", pt)
	}
	kw := h.continuous[1]
	if kw.Source != ids["myBear"] || kw.Affects != "Card.Self" || kw.Layer != state.LAbilities ||
		len(kw.AddKeywords) != 1 || kw.AddKeywords[0] != "Flying" || !kw.UntilEOT {
		t.Fatalf("keyword continuous effect = %+v", kw)
	}
}

// TestPumpKeywordListReadersUseTheSharedParser covers both effects that read
// KW$. Their only KW$ read lives in registerPumpEffects, which calls
// cards.SplitKeywordList; keeping the two routes in one test guards the
// structural wiring as well as the resulting grants.
func TestPumpKeywordListReadersUseTheSharedParser(t *testing.T) {
	const kw = "Protection:Spell.Instant,Spell.Sorcery:instant spells and from sorcery spells & Lifelink"
	want := []string{"Protection:Spell.Instant,Spell.Sorcery:instant spells and from sorcery spells", "Lifelink"}
	for _, tc := range []struct {
		name  string
		sa    string
		ctx   func(map[string]state.ObjID) *Ctx
		count int
	}{
		{
			name: "Pump",
			sa:   "DB$ Pump | ValidTgts$ Creature | KW$ " + kw,
			ctx: func(ids map[string]state.ObjID) *Ctx {
				return &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}}
			},
			count: 1,
		},
		{
			name:  "PumpAll",
			sa:    "DB$ PumpAll | ValidCards$ Creature.YouCtrl | KW$ " + kw,
			ctx:   func(map[string]state.ObjID) *Ctx { return &Ctx{Controller: 0} },
			count: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, ids := board(t)
			h := &fakeHost{g: g}
			Resolve(h, tc.ctx(ids), sa(t, tc.sa))
			if len(h.continuous) != tc.count {
				t.Fatalf("continuous effects = %d, want %d", len(h.continuous), tc.count)
			}
			for _, ce := range h.continuous {
				if ce.Layer != state.LAbilities || !reflect.DeepEqual(ce.AddKeywords, want) {
					t.Errorf("continuous effect = %+v, want keywords %q", ce, want)
				}
			}
		})
	}
}

// TestPumpRequiresTheBattlefield mirrors the old Note-based zone guard: a
// target that has left the battlefield gets no continuous effect registered.
func TestPumpRequiresTheBattlefield(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	moveTo(g, ids["myBear"], state.ZGraveyard)
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}, TargetsOffered: true},
		sa(t, "AB$ Pump | ValidTgts$ Creature | NumAtt$ +2 | NumDef$ +1"))
	if len(h.continuous) != 0 {
		t.Fatalf("continuous = %+v, want none off the battlefield", h.continuous)
	}
}

// TestPumpAllRegistersOneEffectPerMatchingCreatureDeterministically covers
// both the CR 611.2c requirement that a PumpAll bakes in its affected set at
// resolution time (one continuous effect per matching object, scoped with
// Affects$ "Card.Self", rather than one shared filter effect that would also
// catch creatures entering later) and the no-nondeterminism constraint: the
// order must be the same every run, since it comes from AliveFrom/Zone
// (fixed APNAP + slice order), never a map.
func TestPumpAllRegistersOneEffectPerMatchingCreatureDeterministically(t *testing.T) {
	run := func() []state.ObjID {
		g, _ := board(t)
		h := &fakeHost{g: g}
		Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ PumpAll | ValidCards$ Creature.YouCtrl | NumAtt$ +1 | NumDef$ +1"))
		var order []state.ObjID
		for _, ce := range h.continuous {
			order = append(order, ce.Source)
		}
		return order
	}
	_, ids := board(t)
	first := run()
	if len(first) != 2 || first[0] != ids["myBear"] || first[1] != ids["myFlier"] {
		t.Fatalf("order = %v, want [myBear myFlier] (registration order)", first)
	}
	for i := 0; i < 10; i++ {
		if got := run(); len(got) != len(first) || got[0] != first[0] || got[1] != first[1] {
			t.Fatalf("run %d: order changed: %v vs %v", i, got, first)
		}
	}
}

// TestPumpAllSkipsNonMatchingCreatures is the filter-scoping half of the old
// test: YouCtrl must exclude the opponent's creature.
func TestPumpAllSkipsNonMatchingCreatures(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ PumpAll | ValidCards$ Creature.YouCtrl | NumAtt$ +1 | NumDef$ +1"))
	for _, ce := range h.continuous {
		if ce.Source == ids["theirBig"] {
			t.Fatal("PumpAll with YouCtrl must not touch the opponent's creature")
		}
	}
}

// TestAnimateRegistersASetContinuousEffectEvenOffTheBattlefield keeps the
// original off-battlefield coverage (Forge's own Animate targets a graveyard
// card as often as a permanent) while asserting the real layer-7b "setting"
// registration Task 19c adds.
func TestAnimateRegistersASetContinuousEffectEvenOffTheBattlefield(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	moveTo(g, ids["myBear"], state.ZGraveyard)
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}, TargetsOffered: true},
		sa(t, "DB$ Animate | ValidTgts$ Creature | Power$ 4 | Toughness$ 4"))
	if len(h.continuous) != 1 {
		t.Fatalf("continuous = %+v, want 1", h.continuous)
	}
	ce := h.continuous[0]
	if ce.Source != ids["myBear"] || ce.Layer != state.LPT || ce.Sub != state.SubSet ||
		!ce.HasSet || ce.SetPower != 4 || ce.SetToughness != 4 || !ce.UntilEOT {
		t.Fatalf("continuous effect = %+v", ce)
	}
}

// TestAnimateWithNoPowerOrToughnessOnlyGrantsTypes covers the real corpus
// shape (e.g. Kitesail Larcenist, Kami of Industry) where DB$ Animate omits
// Power$/Toughness$ entirely to grant only a type or keyword change. Without
// this guard, Num's zero default would register a SubSet effect setting the
// object to 0/0 -- turning a type-granting Animate into an accidental kill
// spell.
func TestAnimateWithNoPowerOrToughnessOnlyGrantsTypes(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}},
		sa(t, "DB$ Animate | ValidTgts$ Creature | Types$ Artifact Treasure"))
	if len(h.continuous) != 1 {
		t.Fatalf("continuous = %+v, want 1 (types only, no P/T set)", h.continuous)
	}
	ce := h.continuous[0]
	if ce.Layer != state.LType || len(ce.AddTypes) != 2 || ce.AddTypes[0] != "Artifact" || ce.AddTypes[1] != "Treasure" {
		t.Fatalf("continuous effect = %+v", ce)
	}
}

func TestProtectionRequiresTheBattlefield(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}, TargetsOffered: true}, sa(t, "AB$ Protection | ValidTgts$ Creature | Gains$ red"))
	if len(h.continuous) != 1 {
		t.Fatalf("continuous = %+v, want one registration", h.continuous)
	}
	ce := h.continuous[0]
	if ce.Source != ids["myBear"] || ce.Layer != state.LAbilities || len(ce.AddKeywords) != 1 ||
		ce.AddKeywords[0] != "Protection from red" || !ce.UntilEOT {
		t.Fatalf("continuous effect = %+v", ce)
	}
	moveTo(g, ids["myBear"], state.ZGraveyard)
	h.continuous = nil
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}, TargetsOffered: true}, sa(t, "AB$ Protection | ValidTgts$ Creature | Gains$ red"))
	if len(h.continuous) != 0 {
		t.Fatalf("continuous = %+v, want none off the battlefield", h.continuous)
	}
}

// TestProtectionChoiceDefaultsDeterministically covers Mother of Runes'
// shape (Gains$ Choice | Choices$ AnyColor): with no chooser mechanism yet
// (a real choice is Task 20's job, the same simplification effCharm and
// effVote already document), it must still resolve to a concrete, fixed
// quality rather than granting a nonsense "Protection from Choice" keyword.
func TestProtectionChoiceDefaultsDeterministically(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}},
		sa(t, "AB$ Protection | ValidTgts$ Creature | Gains$ Choice | Choices$ AnyColor"))
	if len(h.continuous) != 1 {
		t.Fatalf("continuous = %+v, want one registration", h.continuous)
	}
	if got := h.continuous[0].AddKeywords[0]; got != "Protection from white" {
		t.Fatalf("AddKeywords[0] = %q, want a concrete default colour", got)
	}
}

// ---------------------------------------------------------------------------
// misc.go

func TestEffectRecordsTheIntendedRegistration(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0, Source: 1}, sa(t, "DB$ Effect | StaticAbilities$ SNoCombatDamage | Duration$ UntilHostLeavesPlayOrEOT"))
	if len(h.log) != 1 || h.log[0].Kind != events.Note {
		t.Fatalf("log = %+v", h.log)
	}
}

// TestEffectDeliveredSetMaxHandSizeRegisters pins the Effect-delivery half of
// the SetMaxHandSize$ read: a DB$ Effect whose StaticAbilities$ SVar carries
// "Mode$ Continuous | Affected$ You | SetMaxHandSize$ Unlimited" (Finale of
// Revelation's STHandSize, Wrenn and Seven's UnlimitedHand) must register a
// continuous effect carrying the value, not fall to the unimplemented Note.
// The registration is what rules' maxHandSizeFor consults for the cleanup
// gate, so without it the effect is invisible and the CR 514.1 discard still
// asks.
func TestEffectDeliveredSetMaxHandSizeRegisters(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0, Source: 1, SVars: map[string]string{
		"STHandSize": "Mode$ Continuous | Affected$ You | SetMaxHandSize$ Unlimited | Description$ You have no maximum hand size.",
	}}
	Resolve(h, c, sa(t, "DB$ Effect | StaticAbilities$ STHandSize | Duration$ Permanent"))
	if len(h.continuous) != 1 {
		t.Fatalf("continuous = %+v, want one SetMaxHandSize registration", h.continuous)
	}
	ce := h.continuous[0]
	if ce.SetMaxHandSize != "Unlimited" {
		t.Fatalf("SetMaxHandSize = %q, want Unlimited", ce.SetMaxHandSize)
	}
	if ce.Affects != "You" {
		t.Fatalf("Affects = %q, want You", ce.Affects)
	}
	if !ce.Permanent {
		t.Fatalf("Duration$ Permanent registration = %+v, want a game-lasting effect", ce)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "SetMaxHandSize") {
			t.Fatalf("SetMaxHandSize still reports unimplemented: %q", ev.Text)
		}
	}
}

// TestEffectDeliveredSetMaxHandSizeFailClosed pins the other direction: a
// line carrying a condition gate this registration path does not evaluate
// (Kruphix-style CheckSVar$ or a Delirium Condition$) must NOT register
// blanket -- it reports the unimplemented Note instead, so the permissive
// direction for the grant is refused. A dynamic value (an SVar name) is
// likewise refused, matching rules' printed read.
func TestEffectDeliveredSetMaxHandSizeFailClosed(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"condition gate", "Mode$ Continuous | Condition$ Delirium | Affected$ Opponent | SetMaxHandSize$ 3"},
		{"dynamic value", "Mode$ Continuous | Affected$ You | SetMaxHandSize$ X"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t, 2)
			c := &Ctx{Controller: 0, Source: 1, SVars: map[string]string{"STHandSize": tc.body}}
			Resolve(h, c, sa(t, "DB$ Effect | StaticAbilities$ STHandSize | Duration$ Permanent"))
			for _, ce := range h.continuous {
				if ce.SetMaxHandSize != "" {
					t.Fatalf("unreadable line registered: %+v", ce)
				}
			}
			// The unimplemented Note is the honest report; assert it fired so a
			// silently ignored line cannot pass as "no registration".
			found := false
			for _, ev := range h.log {
				if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
					found = true
				}
			}
			if !found {
				t.Fatalf("unreadable SetMaxHandSize line produced no unimplemented Note: %+v", h.log)
			}
		})
	}
}

func TestCleanupClearRememberedEmitsTheClearAndEmptiesTheList(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Src\nTypes:Instant\nOracle:x\n"), 0)
	mem := h.g.AddObject(mkCard(t, "Name:Mem\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	// Seed the source's event-backed remembered list, then clear it: the
	// clear is a real event (a replay must fold the same empty list), and
	// the ctx-level list goes with it.
	h.Emit(events.Event{Kind: events.Choose, Obj: src.ID, Counter: "remembered", IDs: []state.ObjID{mem.ID}})
	c := &Ctx{Controller: 0, Source: src.ID, Remembered: []state.Target{{Obj: mem.ID}}}
	Resolve(h, c, sa(t, "DB$ Cleanup | ClearRemembered$ True"))
	if len(h.g.Obj(src.ID).Remembered) != 0 {
		t.Fatalf("source remembered = %+v, want empty", h.g.Obj(src.ID).Remembered)
	}
	if len(c.Remembered) != 0 {
		t.Fatalf("ctx remembered = %+v, want empty", c.Remembered)
	}
	if len(h.log) != 2 || h.log[1].Kind != events.Choose || h.log[1].Counter != "clear-remembered" {
		t.Fatalf("log = %+v", h.log)
	}
}

func TestCleanupWithoutClearRememberedStillRecordsANote(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0, Source: 1}, sa(t, "DB$ Cleanup"))
	if len(h.log) != 1 || h.log[0].Kind != events.Note {
		t.Fatalf("log = %+v", h.log)
	}
}

func TestSetStateFlipsToTheOtherFace(t *testing.T) {
	h := newHost(t, 2)
	two := twoFacedCard(t)
	o := h.g.AddObject(two, 0)
	o.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})

	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: o.ID}}}, sa(t, "DB$ SetState | ValidTgts$ Permanent | Mode$ Transform"))
	if o.FaceIdx != 1 || o.Face().Name != "Back" {
		t.Fatalf("FaceIdx = %d, name = %q", o.FaceIdx, o.Face().Name)
	}
}

func TestSetStateNoOpsOnASingleFaceCard(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: ids["myBear"]}}}, sa(t, "DB$ SetState | ValidTgts$ Permanent | Mode$ Flip"))
	if len(h.log) != 0 {
		t.Fatalf("log = %+v, want no events for a single-face card", h.log)
	}
}

func TestCounterMovesTheTargetedSpellToItsOwnersGraveyard(t *testing.T) {
	h := newHost(t, 2)
	bolt := mkCard(t, "Name:Bolt\nTypes:Instant\nOracle:x\n")
	o := h.g.AddObject(bolt, 1)
	o.Zone = state.ZStack
	o.Controller = 1
	h.g.Stack = []state.ObjID{o.ID}
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: o.ID}}}, sa(t, "SP$ Counter | ValidTgts$ Spell"))
	if o.Zone != state.ZGraveyard {
		t.Fatalf("zone = %v, want Graveyard", o.Zone)
	}
	if gy := h.g.Zone(state.ZGraveyard, 1); len(gy) != 1 || gy[0] != o.ID {
		t.Fatalf("owner's graveyard = %v, want [%d]", gy, o.ID)
	}
}

// TestCounterIgnoresATargetNoLongerOnTheStack is CR 608.2b's canonical case
// for this primitive: the targeted spell already resolved (or was itself
// countered) before this Counter got to resolve.
func TestCounterIgnoresATargetNoLongerOnTheStack(t *testing.T) {
	h := newHost(t, 2)
	bolt := mkCard(t, "Name:Bolt\nTypes:Instant\nOracle:x\n")
	o := h.g.AddObject(bolt, 1)
	o.Zone = state.ZGraveyard
	o.Owner, o.Controller = 1, 1
	h.g.SetZone(state.ZGraveyard, 1, []state.ObjID{o.ID})
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: o.ID}}}, sa(t, "SP$ Counter | ValidTgts$ Spell"))
	if len(h.log) != 0 {
		t.Fatalf("log = %+v, want no events for a target already off the stack", h.log)
	}
}

// TestDelayedTriggerPhaseRegisters pins the Mode$ Phase branch: a Phase
// delayed trigger now emits a DelayedRegister event (folded by events.Apply
// into state.Game.Delayed, so the registration survives replay) rather than
// the M1 Note-only recording. The event carries the source, controller, the
// phase step resolved from Phase$, the Execute$ SVar name and the Remembered;
// Event-matched modes use the same event-backed registration shape.
func TestDelayedTriggerPhaseRegisters(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0, Source: 1,
		Remembered: []state.Target{{Obj: 7}}}
	Resolve(h, c, sa(t, "DB$ DelayedTrigger | Mode$ Phase | Phase$ End of Turn | Execute$ X"))
	if len(h.log) != 1 || h.log[0].Kind != events.DelayedRegister {
		t.Fatalf("log = %+v", h.log)
	}
	ev := h.log[0]
	if ev.Obj != 1 || ev.Player != 0 || ev.Step != state.StepEnd || ev.Counter != "X" ||
		len(ev.IDs) != 1 || ev.IDs[0] != 7 {
		t.Fatalf("DelayedRegister = %+v", ev)
	}
}

// TestDelayedTriggerNonPhaseRegisters pins the event-backed registration
// shape used by all non-Phase modes.
func TestDelayedTriggerNonPhaseRegisters(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0, Source: 1}, sa(t, "DB$ DelayedTrigger | Mode$ ChangesZone | Execute$ X"))
	if len(h.log) != 1 || h.log[0].Kind != events.DelayedRegister {
		t.Fatalf("log = %+v", h.log)
	}
	if h.log[0].Text != "ChangesZone:Mode$ ChangesZone" || h.log[0].Step != state.StepUntap {
		t.Fatalf("registration = %+v", h.log[0])
	}
}

func TestRepeatRunsTheSubAbilityRepeatNumTimes(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 10
	c := &Ctx{Controller: 0, SVars: map[string]string{
		"Boost": "DB$ GainLife | Defined$ You | LifeAmount$ 1",
	}}
	Resolve(h, c, sa(t, "SP$ Repeat | RepeatSubAbility$ Boost | RepeatNum$ 3"))
	if h.g.Players[0].Life != 13 {
		t.Fatalf("life = %d, want 13 (three runs of +1)", h.g.Players[0].Life)
	}
}

func TestRepeatDoesNothingWithoutSVars(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ Repeat | RepeatSubAbility$ Boost | RepeatNum$ 3"))
	if len(h.log) != 0 {
		t.Fatalf("log = %+v, want no events when no SVars are bound", h.log)
	}
}

func TestCharmRunsOnlyTheFirstChoice(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 10
	c := &Ctx{Controller: 0, SVars: map[string]string{
		"DoGain": "DB$ GainLife | Defined$ You | LifeAmount$ 5",
		"DoLose": "DB$ LoseLife | Defined$ You | LifeAmount$ 5",
	}}
	Resolve(h, c, sa(t, "SP$ Charm | Choices$ DoGain,DoLose"))
	if h.g.Players[0].Life != 15 {
		t.Fatalf("life = %d, want 15 (only the first choice runs)", h.g.Players[0].Life)
	}
	// A host that cannot ask keeps the deterministic first-mode stand-in
	// WITH the Note that records why the richer path did not run (the R-9
	// fallback, M2d-2: real asks now go to an engine host instead).
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "first mode") {
			found = true
		}
	}
	if !found {
		t.Fatal("no Note recorded the first-mode fallback")
	}
}

// askHost is a fakeHost whose Ask captures the posed decision and returns
// true -- the effects-package stand-in for a rules.Engine that can suspend
// a resolution. The test then inspects what was asked, and simulates the
// engine's re-entry by re-running the same ability with the answer (Ctx.Modes
// / Ctx.UnlessPay) attached, which is exactly what rules' resumeResolution
// does.
type askHost struct {
	fakeHost
	asked *decision.Decision
}

func (h *askHost) Ask(d *decision.Decision) bool {
	cp := *d
	cp.Options = append([]decision.Option(nil), d.Options...)
	h.asked = &cp
	return true
}

func TestCharmAsksForItsModeBeforeAnySubAbilityRuns(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	h.g.Players[0].Life = 10
	src := "SP$ Charm | Choices$ DoGain,DoLose"
	c := &Ctx{Controller: 0, SVars: map[string]string{
		"DoGain": "DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life",
		"DoLose": "DB$ LoseLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Lose 5 life",
	}}
	Resolve(h, c, sa(t, src))
	if h.asked == nil {
		t.Fatal("no KModes decision was posed")
	}
	d := h.asked
	if d.Kind != decision.KModes || d.Min != 1 || d.Max != 1 || d.Player != 0 {
		t.Fatalf("decision = %+v, want a Min==Max==1 KModes for the controller", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "mode" ||
		d.Options[0].Label != "Gain 5 life" || d.Options[1].Label != "Lose 5 life" {
		t.Fatalf("mode options: %+v", d.Options)
	}
	// The resolution suspended BEFORE executing any mode sub-ability.
	if h.g.Players[0].Life != 10 {
		t.Fatalf("life = %d, want 10: a mode ran before the choice was made", h.g.Players[0].Life)
	}
	// Re-entry, the engine's contract: Ctx.Modes carries the chosen SVar
	// names in execution order; the chosen mode — the SECOND one, to prove
	// the choice is honoured — is what runs.
	Resolve(h, &Ctx{Controller: 0, SVars: c.SVars, Modes: []string{"DoLose"}}, sa(t, src))
	if h.g.Players[0].Life != 5 {
		t.Fatalf("life = %d, want 5 (the chosen Lose 5 life ran)", h.g.Players[0].Life)
	}
}

func TestVoteRecordsANotePerVotingPlayer(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ Vote | Defined$ Player | Choices$ Sickness,Psychosis"))
	// The fixed-Choices$ ballot poses a private per-voter KChoose (the
	// votepb1 ask machinery); the fake host cannot answer it, so each voter
	// takes the deterministic first option (R-9) and records the loud
	// no-host Note beside its "votes for" reveal — the same fallback shape
	// the player-ballot and card-ballot no-host paths emit. One "votes for
	// Sickness" Note per voting player is still the contract under test.
	fallbacks, votes := 0, 0
	for _, e := range h.log {
		switch {
		case e.Kind == events.Note && strings.Contains(e.Text, "no engine host to ask"):
			fallbacks++
		case e.Kind == events.Note && e.Text == "votes for Sickness":
			votes++
		default:
			t.Fatalf("event = %+v, want only no-host fallback and votes-for Notes", e)
		}
	}
	if fallbacks != 2 || votes != 2 {
		t.Fatalf("%d no-host fallback and %d votes-for Notes, want one of each per voting player (2): log %+v", fallbacks, votes, h.log)
	}
}

func TestBecomeMonarchRecordsTheTargetPlayer(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}, sa(t, "AB$ BecomeMonarch | ValidTgts$ Player"))
	if len(h.log) != 1 || h.log[0].Kind != events.MonarchChange || h.log[0].Player != 1 || !h.g.IsMonarch(1) {
		t.Fatalf("log = %+v, monarch = %v/%d", h.log, h.g.HasMonarch, h.g.Monarch)
	}
}

// TestBecomeMonarchRepeatOnReigningMonarchEmitsNothing pins the transition
// semantics (CR 720.2): a repeat BecomeMonarch naming the seat that already
// holds the designation is a no-op. MonarchChange is what
// rules' trig:BecomeMonarch matcher reads, so an unconditional emit fired
// "whenever YOU become the monarch" a second time for the reigning player.
func TestBecomeMonarchRepeatOnReigningMonarchEmitsNothing(t *testing.T) {
	h := newHost(t, 2)
	line := sa(t, "AB$ BecomeMonarch | ValidTgts$ Player")
	ctx := func() *Ctx {
		return &Ctx{Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}
	}
	Resolve(h, ctx(), line)
	if !h.g.IsMonarch(1) {
		t.Fatalf("precondition: the first BecomeMonarch did not make seat 1 the monarch (%v/%d)", h.g.HasMonarch, h.g.Monarch)
	}
	Resolve(h, ctx(), line)
	monarch := 0
	for _, ev := range h.log {
		if ev.Kind == events.MonarchChange {
			monarch++
		}
	}
	if monarch != 1 {
		t.Fatalf("MonarchChange emitted %d times, want 1 (the repeat is a no-op)", monarch)
	}
}

// TestBecomeMonarchMovesTheDesignationBetweenSeats is the control the guard
// above needs: a naming of a DIFFERENT seat is a real transition and must
// still emit. Without it the repeat test would pass with the primitive
// suppressed outright.
func TestBecomeMonarchMovesTheDesignationBetweenSeats(t *testing.T) {
	h := newHost(t, 2)
	line := sa(t, "AB$ BecomeMonarch | ValidTgts$ Player")
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Player: 0, IsPlayer: true}}, TargetsOffered: true}, line)
	Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}, line)
	if !h.g.IsMonarch(1) {
		t.Fatalf("designation did not move to seat 1 (%v/%d)", h.g.HasMonarch, h.g.Monarch)
	}
	monarch := 0
	for _, ev := range h.log {
		if ev.Kind == events.MonarchChange {
			monarch++
		}
	}
	if monarch != 2 {
		t.Fatalf("MonarchChange emitted %d times, want 2 (both moves are real transitions)", monarch)
	}
}

// TestRestartGameEndsTheGame is the fix-round-2 regression test for the
// re-review's N3: effRestartGame's own comment and Text both say the game
// ends as a draw, but the GameOver event it emitted left Amount at its zero
// value, which events.Apply's GameOver case (Task 22 fix round 1) reads as
// "Amount 0: a win" -- and Player, also left at zero, validates as seat 0.
// So despite every piece of this primitive's own documentation, it was
// actually encoding a seat-0 win. Asserting Draw (not just Over) is what
// catches that; asserting Over alone, as this test used to, cannot tell a
// win from a draw at all.
func TestRestartGameEndsTheGame(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "AB$ RestartGame"))
	if !h.g.Over {
		t.Fatal("RestartGame must end the game")
	}
	if !h.g.Draw {
		t.Fatal("RestartGame ends the game as a draw (its own Text says so), not a win for seat 0")
	}
}

func TestMana(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "AB$ Mana | Cost$ T | Produced$ R | Amount$ 2"))
	if h.g.Players[0].Pool[state.MR] != 2 {
		t.Fatalf("pool = %v, want 2 red", h.g.Players[0].Pool)
	}
}

// TestCopySpellAbilityIsNotYetRegistered documented the pre-Task-17 gap
// (StackCopy existed as of Task 12 but nothing wired the primitive to it).
// Task 17 implemented effCopySpellAbility (effects/copy.go); the primitive's
// behaviour is now covered by effects/copy_test.go and rules/storm_test.go,
// so this interim marker is removed rather than kept asserting the opposite.

// TestCardflowAPIsGuardOutOfRangePlayerID is Ruling T18-a's regression test.
// Every one of cardflow.go's nine registered APIs eventually reads a zone via
// PlayerOf's result (a target's raw Player field) or, for NameCard,
// Ctx.Controller directly. Neither is validated before this task's fix, and
// Game.Zone computes int(p)*numZones+int(z) with no bounds check of its own
// before indexing a fixed-size slice -- an out-of-range PlayerID reached that
// arithmetic and panicked. Each API is exercised two ways: an out-of-range
// target Player (PlayerOf returns t.Player unchecked), and an out-of-range
// Ctx.Controller reaching the same code path via "Defined$ You" (NameCard
// reads Ctx.Controller directly regardless of Defined$, so it skips that
// suffix). Both must be a total no-op: no panic, and the game state
// afterwards is reflect.DeepEqual to a clone taken beforehand.
func TestCardflowAPIsGuardOutOfRangePlayerID(t *testing.T) {
	apis := []string{"Draw", "Discard", "Mill", "Dig", "DigUntil",
		"Reveal", "RevealHand", "PeekAndReveal", "RearrangeTopOfLibrary", "NameCard"}

	run := func(t *testing.T, line string, c *Ctx) {
		t.Helper()
		g, _ := board(t)
		h := &fakeHost{g: g}
		before := g.Clone()
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked resolving %q: %v", line, r)
				}
			}()
			Resolve(h, c, sa(t, line))
		}()
		if !reflect.DeepEqual(before, g) {
			t.Fatalf("state changed resolving %q with an out-of-range PlayerID", line)
		}
	}

	for _, api := range apis {
		api := api
		t.Run(api+"/target_player_out_of_range", func(t *testing.T) {
			run(t, "SP$ "+api+" | ValidTgts$ Player", &Ctx{Controller: 0,
				Targets: []state.Target{{Player: 250, IsPlayer: true}}, TargetsOffered: true})
		})
		t.Run(api+"/controller_out_of_range", func(t *testing.T) {
			line := "SP$ " + api
			if api != "NameCard" {
				line += " | Defined$ You"
			}
			run(t, line, &Ctx{Controller: 250})
		})
	}
}
