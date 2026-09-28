package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// printedAddability_test.go is the leaf for CR 613.1f's printed-Continuous
// AddAbility$ grant: a `S:Mode$ Continuous | Affected$ <spec> | AddAbility$
// <SVar>` static (Ichormoon Gauntlet, Tazri, viridian_longbow, kusari_gama)
// used to emit no state.ContinuousEffect at all, so its non-mana ability was
// never offered. The structural fix lives in rules/layers.go (staticEffects
// emits the grant), rules/legal.go (grantedAbilities threads the grantor
// through decision.Option.GrantSource) and rules/speed.go
// (beginGrantedActivation resolves the body from the grantor while the minted
// ability's Source is the recipient, via events.GrantAbilityPush).

// printedAddabilityEngine builds a two-seat fixture whose protagonist is seat
// 0 and whose deck holds every argument card. Callers place cards by pointer.
// The corpus's token scripts are wired in (Config.Tokens) so a test that
// mints a token (Goldspan Dragon's Treasure) mints the REAL script the
// acceptance fixtures pass -- the pure superset changes nothing for the
// tests that never emit TokenCreate.
func printedAddabilityEngine(t *testing.T, list ...*cards.Card) *Engine {
	t.Helper()
	deck := append([]*cards.Card{}, list...)
	if len(deck) > 40 {
		t.Fatalf("fixture deck too large: %d", len(deck))
	}
	deck = append(deck, mountainDeck(t, 40-len(deck))...)
	cfg := seatZeroStart(Config{Seed: 7, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck, mountainDeck(t, 40)},
		Tokens: testutil.CorpusRegistry(t).Tokens})
	e := New(cfg)
	e.Advance()
	return e
}

// moveCardToBattlefield finds a deck card by pointer in seat 0's hidden zones
// and moves it onto the battlefield with a logged MoveZone, returning its id.
func moveCardToBattlefield(t *testing.T, e *Engine, c *cards.Card) state.ObjID {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || (o.Zone != state.ZLibrary && o.Zone != state.ZHand) || o.Card != c {
			continue
		}
		id := o.ID
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
		return id
	}
	t.Fatalf("card %q not found in seat 0's library/hand", c.Faces[0].Name)
	return 0
}

// findSVarOption returns the granted "ability" option anchored on svar for
// obj, or ok=false. Granted options carry SVar (never Ability), so the
// printed-index helper cannot find them.
func findSVarOption(t *testing.T, e *Engine, obj state.ObjID, svar string) (decision.Option, bool) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending while scanning for granted %q on %d", svar, obj)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj && o.SVar == svar {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestPrintedContinuousAddAbilityCrossObjectGrant covers Done-means (a): a
// printed static whose Affected$ is another permanent. The ability must be
// offered on the RECIPIENT, activate, pay its cost and resolve -- and the
// granted body must resolve from the grantor's face, not the recipient's
// (the recipient has no such SVar, which was the silent no-op).
func TestPrintedContinuousAddAbilityCrossObjectGrant(t *testing.T) {
	t.Parallel()
	grantor := card(t, "Name:Grantor\nManaCost:0\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Creature.Other+YouCtrl | AddAbility$ Zap\n"+
		"SVar:Zap:AB$ Draw | Cost$ T | NumCards$ 1 | Defined$ You | SpellDescription$ Draw a card.\nOracle:x\n")
	recipient := card(t, "Name:Recipient\nManaCost:0\nTypes:Creature\nPT:1/1\nOracle:x\n")

	e := printedAddabilityEngine(t, grantor, recipient)
	moveCardToBattlefield(t, e, grantor)
	recipientID := moveCardToBattlefield(t, e, recipient)
	e.G.Obj(recipientID).SummonSick = false
	addMana(t, e, 0, "")

	opt, ok := findSVarOption(t, e, recipientID, "Zap")
	if !ok {
		t.Fatalf("cross-object printed AddAbility$ grant not offered on the recipient: %+v", e.Pending().Options)
	}
	if opt.GrantSource == 0 || opt.GrantSource == recipientID {
		t.Fatalf("granted option GrantSource = %d, want the grantor id", opt.GrantSource)
	}
	if grantorID := opt.GrantSource; e.G.Obj(grantorID).Face().Name != "Grantor" {
		t.Fatalf("GrantSource names %q, want Grantor", e.G.Obj(grantorID).Face().Name)
	}

	hand := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, opt.Index)
	if len(e.G.Stack) != 1 {
		t.Fatalf("activation did not push an ability object: stack=%v", e.G.Stack)
	}
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+1 {
		t.Fatalf("recipient's granted Draw did not resolve: hand %d -> %d", hand, got)
	}
}

// TestPrintedContinuousAddAbilitySelfGrant covers Done-means (b): the
// self-scoped shape (Affected$ Card.Self), the path every existing Animate
// and max-speed grant shares. It must stay on the DelayedPush identity and
// resolve from the object itself.
func TestPrintedContinuousAddAbilitySelfGrant(t *testing.T) {
	t.Parallel()
	self := card(t, "Name:Self Granter\nManaCost:0\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddAbility$ Draw\n"+
		"SVar:Draw:AB$ Draw | Cost$ 0 | NumCards$ 1 | Defined$ You | SpellDescription$ Draw a card.\nOracle:x\n")

	e := printedAddabilityEngine(t, self)
	id := moveCardToBattlefield(t, e, self)
	addMana(t, e, 0, "")

	opt, ok := findSVarOption(t, e, id, "Draw")
	if !ok {
		t.Fatalf("self-scoped printed AddAbility$ grant not offered: %+v", e.Pending().Options)
	}
	if opt.GrantSource != id {
		t.Fatalf("self-grant GrantSource = %d, want the object itself %d", opt.GrantSource, id)
	}
	hand := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, opt.Index)
	// A self-grant mints through DelayedPush (the byte-identical legacy
	// identity), never GrantAbilityPush.
	if n := countKind(e.L.Events, events.DelayedPush, id); n != 1 {
		t.Fatalf("self-grant DelayedPush count = %d, want 1", n)
	}
	if n := countKind(e.L.Events, events.GrantAbilityPush, id); n != 0 {
		t.Fatalf("self-grant minted %d GrantAbilityPush events, want 0", n)
	}
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+1 {
		t.Fatalf("self-granted Draw did not resolve: hand %d -> %d", hand, got)
	}
}

// TestPrintedContinuousAddAbilityMultiValue covers Done-means (c): the
// `AddAbility$ A & B` multi-value form (6 corpus carriers) offers both
// bodies.
func TestPrintedContinuousAddAbilityMultiValue(t *testing.T) {
	t.Parallel()
	multi := card(t, "Name:Multi Granter\nManaCost:0\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddAbility$ Draw & Heal\n"+
		"SVar:Draw:AB$ Draw | Cost$ 0 | NumCards$ 1 | Defined$ You | SpellDescription$ Draw a card.\n"+
		"SVar:Heal:AB$ GainLife | Cost$ 0 | Defined$ You | LifeAmount$ 2 | SpellDescription$ Gain 2 life.\n"+
		"Oracle:x\n")

	e := printedAddabilityEngine(t, multi)
	id := moveCardToBattlefield(t, e, multi)
	addMana(t, e, 0, "")

	if _, ok := findSVarOption(t, e, id, "Draw"); !ok {
		t.Fatalf("first half of AddAbility$ Draw & Heal not offered: %+v", e.Pending().Options)
	}
	heal, ok := findSVarOption(t, e, id, "Heal")
	if !ok {
		t.Fatalf("second half of AddAbility$ Draw & Heal not offered: %+v", e.Pending().Options)
	}
	life := e.G.Players[0].Life
	submitChoices(t, e, heal.Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != life+2 {
		t.Fatalf("second AddAbility$ body did not resolve: life %d -> %d, want +2", life, got)
	}
}

// TestPrintedContinuousAddAbilityManaGrantsExactlyOnce covers Done-means (3):
// the duplicate-offer trap. A printed AddAbility$ whose body is AB$ Mana must
// appear in availableManaAbilities exactly once -- before the fix the old
// direct static scan and the new grantedAbilities walk each contributed a
// member. The static holder is an Artifact granting the ability to ANOTHER
// artifact, so the grant is genuinely cross-object.
func TestPrintedContinuousAddAbilityManaGrantsExactlyOnce(t *testing.T) {
	t.Parallel()
	holder := card(t, "Name:Mana Holder\nManaCost:0\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Artifact.Other+YouCtrl | AddAbility$ Rock\n"+
		"SVar:Rock:AB$ Mana | Cost$ T | Produced$ C\nOracle:x\n")
	target := card(t, "Name:Mana Target\nManaCost:0\nTypes:Artifact\nOracle:x\n")

	e := printedAddabilityEngine(t, holder, target)
	moveCardToBattlefield(t, e, holder)
	id := moveCardToBattlefield(t, e, target)

	ma := e.availableManaAbilities(0, id)
	if len(ma) != 1 {
		t.Fatalf("availableManaAbilities returned %d members for one printed AddAbility grant, want exactly 1", len(ma))
	}
	if ma[0].API != "Mana" {
		t.Fatalf("granted mana member API = %q, want Mana", ma[0].API)
	}
}

// TestPrintedContinuousAddAbilityCorpusBarbedField covers Done-means (2): the
// real corpus Aura Barbed Field grants its enchanted LAND "{T}: This land
// deals 1 damage to any target." The ability was inert before the fix.
func TestPrintedContinuousAddAbilityCorpusBarbedField(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	barbed := mustCorpusCard(t, reg, "Barbed Field")
	mountain := mustCorpusCard(t, reg, "Mountain")

	e := printedAddabilityEngine(t, barbed, mountain)
	auraID := moveCardToBattlefield(t, e, barbed)
	landID := moveCardToBattlefield(t, e, mountain)
	e.emit(events.Event{Kind: events.Attach, Obj: auraID, IDs: []state.ObjID{landID}})
	addMana(t, e, 0, "")

	opt, ok := findSVarOption(t, e, landID, "Damage")
	if !ok {
		t.Fatalf("Barbed Field's granted land ability not offered: %+v", e.Pending().Options)
	}
	if opt.GrantSource != auraID {
		t.Fatalf("Barbed Field grant GrantSource = %d, want aura %d", opt.GrantSource, auraID)
	}
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a KTarget decision after activating the granted damage ability, got %+v", d)
	}
	idx := indexOfPlayerOption(d, 1)
	if idx < 0 {
		t.Fatalf("opponent face not offered as a target by ValidTgts$ Any: %+v", d.Options)
	}
	life := e.G.Players[1].Life
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[1].Life; got != life-1 {
		t.Fatalf("Barbed Field's granted ability dealt no damage: life %d -> %d", life, got)
	}
	if !e.G.Obj(landID).Tapped {
		t.Fatal("the granted {T} cost did not tap the enchanted land")
	}
}

// TestPrintedContinuousAddAbilityCorpusKusariGama covers Done-means (2): the
// real corpus Equipment Kusari-Gama grants its equipped CREATURE "{2}: This
// creature gets +1/+0 until end of turn." The body's `Defined$ Self` must
// resolve to the equipped creature (the ability's Source), never to the
// Equipment -- the exact source-vs-recipient distinction the fix is about.
func TestPrintedContinuousAddAbilityCorpusKusariGama(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	gama := mustCorpusCard(t, reg, "Kusari-Gama")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")

	e := printedAddabilityEngine(t, gama, bear)
	equipID := moveCardToBattlefield(t, e, gama)
	bearID := moveCardToBattlefield(t, e, bear)
	e.emit(events.Event{Kind: events.Attach, Obj: equipID, IDs: []state.ObjID{bearID}})
	addMana(t, e, 0, "CC")

	before := e.Derived(bearID).Power
	opt, ok := findSVarOption(t, e, bearID, "GamaPump")
	if !ok {
		t.Fatalf("Kusari-Gama's granted equipped-creature ability not offered: %+v", e.Pending().Options)
	}
	if opt.GrantSource != equipID {
		t.Fatalf("Kusari-Gama grant GrantSource = %d, want equipment %d", opt.GrantSource, equipID)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)

	if got := e.Derived(bearID).Power; got != before+1 {
		t.Fatalf("Kusari-Gama's Defined$ Self resolved to the wrong object: equipped creature power %d -> %d, want +1", before, got)
	}
	if got := e.Derived(equipID).Power; got != 0 {
		t.Fatalf("Kusari-Gama's pump landed on the Equipment (power %d), not the creature", got)
	}
}

// TestPrintedContinuousAddAbilityCorpusGoldspanDragon is the brief's own
// done-criterion carrier: the real corpus Goldspan Dragon's
// `S:Mode$ Continuous | Affected$ Card.Treasure+YouCtrl | AddAbility$ Mana`
// ("Treasures you control have '{T}, Sacrifice this artifact: Add two mana of
// any one color'") must give ITS controller's Treasure a SECOND mana ability
// (the SVar body's Amount$ 2), while an opponent's Treasure -- outside the
// YouCtrl scope -- keeps only its printed add-one ability. Before the mana
// scan read the static, the deck's Treasures added one mana at a time with no
// error anywhere: the divergence this test pins end to end, activation
// included (the T + sacrifice cost, the "any one color" ask, the two units).
func TestPrintedContinuousAddAbilityCorpusGoldspanDragon(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	dragon := mustCorpusCard(t, reg, "Goldspan Dragon")

	e := printedAddabilityEngine(t, dragon)
	dragonID := moveCardToBattlefield(t, e, dragon)
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "c_a_treasure_sac"})
	e.emit(events.Event{Kind: events.TokenCreate, Player: 1, Text: "c_a_treasure_sac"})
	var myTok, oppTok state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken {
			myTok = id
		}
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 1) {
		if o := e.G.Obj(id); o != nil && o.IsToken {
			oppTok = id
		}
	}
	if myTok == 0 || oppTok == 0 {
		t.Fatalf("the real corpus Treasure tokens were not minted: mine=%d opponent's=%d", myTok, oppTok)
	}
	if o := e.G.Obj(dragonID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("Goldspan Dragon is not on the battlefield -- the precondition the grant reads")
	}

	// Precondition: the two Treasures' mana-ability sets differ exactly the
	// way the static's Affected$ Card.Treasure+YouCtrl scopes them. Mine
	// carries the granted Amount-2 body beside the printed add-one; the
	// opponent's carries ONLY the printed add-one.
	mine := e.availableManaAbilities(0, myTok)
	theirs := e.availableManaAbilities(1, oppTok)
	if len(mine) != 2 || len(theirs) != 1 {
		t.Fatalf("mana-ability membership wrong: mine %d members, opponent's %d members (want 2 and 1)", len(mine), len(theirs))
	}
	granted := -1
	printed := -1
	for i, ma := range mine {
		switch ma.Params["Amount"] {
		case "2":
			granted = i
		case "1":
			printed = i
		}
	}
	if granted < 0 || printed < 0 {
		t.Fatalf("my Treasure's members are not the printed add-one plus the granted add-two: %+v", mine)
	}
	if theirs[0].Params["Amount"] != "1" {
		t.Fatalf("opponent's Treasure must keep only its printed add-one ability, got Amount %q", theirs[0].Params["Amount"])
	}

	addMana(t, e, 0, "")
	d := e.Pending()
	act := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == myTok {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatalf("my Treasure's activate option is not offered: %+v", d.Options)
	}
	submitChoices(t, e, act)

	// Stage 1: two mana abilities share the Treasure, so the engine asks
	// which -- pick the granted member by its ability index.
	cd := e.Pending()
	if cd == nil || cd.Kind != decision.KChoose {
		t.Fatalf("expected the choose-a-mana-ability ask over both members, got %+v", cd)
	}
	pick := -1
	for _, o := range cd.Options {
		if o.Ability == granted {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the granted member is not among the stage-1 options: %+v", cd.Options)
	}
	submitChoices(t, e, pick)

	// Stage 2: "any one color" -- the colour ask; take the first offered.
	if cd2 := e.Pending(); cd2 != nil && cd2.Kind == decision.KChoose {
		submitChoices(t, e, 0)
	}
	if got := e.G.Players[0].Pool[state.MW]; got != 2 {
		t.Fatalf("the granted ability added %d of the chosen colour, want 2 (the SVar body's Amount$ 2)", got)
	}
	if o := e.G.Obj(myTok); o != nil && o.Zone == state.ZBattlefield {
		t.Fatal("the Sac<1/CARDNAME> cost did not sacrifice the Treasure")
	}
}
