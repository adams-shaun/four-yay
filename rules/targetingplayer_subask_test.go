package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// This file pins TargetingPlayer$ on a chained DB$ sub. For a SPELL the link
// is announced on cast (CR 601.2c) by the cast flow's subTargetAsk, which
// routes the chooser through targetAskChooser (Volcanic Offering's
// DBDestroyLand). For a TRIGGER's deeper link the ask is still posed below
// the rules tier as the trigger resolves -- effects.chosenTargetsFor (the
// mvts1 "tgts" ask) or effects.changeZoneChosenTargets (the ChangeZone
// "choice" ask) -- and Engine.ChooserFor routes it (Mausoleum Turnkey's
// TrigChangeZone shape). Before either router the ask silently went to the
// ability's controller.

// cardHasSubTargetingPlayer reports whether the compiled card carries any
// SVar body (a depth>=2 DB$ sub) whose TargetingPlayer$ equals spec and whose
// ValidTgts$ contains validTgts -- the fixture premise of the tests below. A
// build that lost the parameter would otherwise pass them vacuously with the
// ask defaulted to the controller. (SVar subs are stored as raw body text on
// the face; the parsed SA is built on demand at resolution.)
func cardHasSubTargetingPlayer(card *cards.Card, spec, validTgts string) bool {
	for _, f := range card.Faces {
		for _, body := range f.SVars {
			if strings.Contains(body, "TargetingPlayer$ "+spec) &&
				strings.Contains(body, "ValidTgts$ "+validTgts) {
				return true
			}
		}
	}
	return false
}

// subaskChooserBoard deals seat 0 the named corpus carrier plus a Grizzly
// Bears (for graveyard seeding) and basics; seat 1 gets the extras the test
// names (a nonbasic land for Volcanic Offering) plus a Grizzly Bears and
// basics. Seat 0 is the active seat so the caster's Main 1 is pending.
func subaskChooserBoard(t *testing.T, reg *cards.Registry, carrier *cards.Card, seat1Extras ...*cards.Card) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	d0 := []*cards.Card{carrier, bear}
	for len(d0) < 40 {
		d0 = append(d0, forest, mountain)
	}
	d0 = d0[:40]
	d1 := append([]*cards.Card{}, seat1Extras...)
	d1 = append(d1, bear)
	for len(d1) < 40 {
		d1 = append(d1, mountain)
	}
	d1 = d1[:40]
	cfg := seatZeroStart(Config{Seed: 9120, Names: []string{"caster", "chooser"},
		Decks: [][]*cards.Card{d0, d1}, Tokens: reg.Tokens, NameUniverse: reg.AllCards()})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// TestVolcanicOfferingSubTargetAskGoesToOpponent: Volcanic Offering's root
// SP$ Pump has no TargetingPlayer$, so the CASTER answers the first land ask;
// its DBDestroyLand link carries `TargetingPlayer$ Player.Opponent |
// ValidTgts$ Land.nonBasic+YouDontCtrl` -- "target nonbasic land of an
// opponent's choice you don't control". CR 601.2c: that is still a target of
// the spell, chosen as it is CAST, by the opponent. So the ask is posed to
// seat 1 during the announcement, before any cost is paid, and so are the two
// creature targets (the caster's, then the opponent's). Nothing is asked as
// the spell resolves.
func TestVolcanicOfferingSubTargetAskGoesToOpponent(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	offering := searchCorpusCard(t, reg, "Volcanic Offering")
	if !cardHasSubTargetingPlayer(offering, "Player.Opponent", "Land.nonBasic") {
		t.Fatal("Volcanic Offering's compiled sub no longer carries TargetingPlayer$ Player.Opponent | ValidTgts$ Land.nonBasic -- fixture premise broken")
	}
	furnace := searchCorpusCard(t, reg, "Great Furnace")
	e, cfg := subaskChooserBoard(t, reg, offering, furnace)
	offeringID := searchMoveByName(t, e, "Volcanic Offering", state.ZHand)
	furnaceID := searchMoveByNameSeat(t, e, 1, "Great Furnace", state.ZBattlefield)
	// "7 damage to target creature you don't control" is mandatory: with no
	// opposing creature the spell could not be cast at all (CR 601.2c).
	bearID := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if offeringID == 0 || furnaceID == 0 || bearID == 0 {
		t.Fatalf("fixtures missing: offering=%d furnace=%d bear=%d", offeringID, furnaceID, bearID)
	}
	fo := e.G.Obj(furnaceID)
	if fo == nil || fo.Zone != state.ZBattlefield || fo.Controller != 1 {
		t.Fatalf("furnace precondition: %+v (want a battlefield nonbasic land under seat 1)", fo)
	}
	addMana(t, e, 0, "CCCCR") // {4}{R}
	submitChoices(t, e, castCardOption(t, e, offeringID).Index)

	// Root ask: the caster (seat 0) picks the first land.
	root := e.Pending()
	if root == nil || root.Kind != decision.KTarget || root.Player != 0 || root.ResumeKind == "cast_sub" {
		t.Fatalf("root target ask = %+v, want the caster seat 0 answering the root Pump", root)
	}
	answerTargetAsk(t, e, []state.ObjID{furnaceID})

	// DBDestroyLand: announced on cast, by the named opponent.
	ask := castSubAsk(t, e)
	if ask.Player != 1 {
		t.Fatalf("the opponent's-choice land ask is posed to seat %d, want the opponent seat 1 (TargetingPlayer$ Player.Opponent)", ask.Player)
	}
	// Legality stays controller-relative: the offer is still seat 0's
	// "nonbasic land you don't control", i.e. seat 1's furnace.
	answerCastSubObj(t, e, furnaceID)
	// DBDamage: the caster's own creature target.
	if ask = castSubAsk(t, e); ask.Player != 0 {
		t.Fatalf("the caster's creature ask is posed to seat %d, want seat 0", ask.Player)
	}
	answerCastSubObj(t, e, bearID)
	// DBDamage2: the opponent's-choice creature target.
	if ask = castSubAsk(t, e); ask.Player != 1 {
		t.Fatalf("the opponent's-choice creature ask is posed to seat %d, want seat 1", ask.Player)
	}
	answerCastSubObj(t, e, bearID)

	// Everything was announced before payment; the spell is on the stack with
	// its mana spent, and it resolves without asking anything.
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || len(e.G.Stack) != 1 {
		t.Fatalf("after the announcement: pending %+v, stack %v; want priority with the spell on the stack", d, e.G.Stack)
	}
	for len(e.G.Stack) > 0 {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("the resolution posed %+v: every target was already announced on cast", d)
		}
		submitChoices(t, e, passIndex(t, d))
	}
	if o := e.G.Obj(furnaceID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the targeted nonbasic land was not destroyed: %+v", o)
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the targeted creature did not die to the 7 damage: %+v", o)
	}
	replayCheck(t, e, cfg)
}

// choiceProbeOuterSrc is an inline trigger whose Execute body is an inert
// Pump carrying a depth-2 ChangeZone sub with TargetingPlayer$. A depth-1
// ChangeZone body (Mausoleum Turnkey's own shape) is covered by the
// placement ask (rules.askTarget), so it never reaches
// effects.changeZoneChosenTargets; this chain forces the depth-2
// mid-resolution "choice" ask the shared tail owns.
const choiceProbeOuterSrc = "Name:Choice Probe\nManaCost:B\nTypes:Creature Ogre\nPT:3/2\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigOuter | TriggerDescription$ x\n" +
	"SVar:TrigOuter:DB$ Pump | Defined$ Self | SubAbility$ TrigChangeZone\n" +
	"SVar:TrigChangeZone:DB$ ChangeZone | Origin$ Graveyard | Destination$ Hand | ValidTgts$ Creature.YouOwn | TargetingPlayer$ Opponent\n" +
	"Oracle:x\n"

const choiceProbeDeadSrc = "Name:Dead Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// TestMausoleumTurnkeyChangeZoneAskGoesToOpponent is the ChangeZone
// "choice" half: a depth-2 ChangeZone sub carrying `TargetingPlayer$
// Opponent | ValidTgts$ Creature.YouOwn` must have its resolution-time
// `choice` ask posed to seat 1, the named opponent. Before this fix it was
// posed to seat 0, the ability's controller. The trigger shape is inline
// because no real corpus carrier expresses this depth (Mausoleum Turnkey's
// ChangeZone body is depth 1 and reaches the placement ask instead).
func TestMausoleumTurnkeyChangeZoneAskGoesToOpponent(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 551, choiceProbeOuterSrc, choiceProbeDeadSrc)
	gy := searchMoveByName(t, e, "Dead Bear", state.ZGraveyard)
	if o := e.G.Obj(gy); o == nil || o.Zone != state.ZGraveyard || o.Controller != 0 {
		t.Fatalf("graveyard fixture: %+v (want a creature CARD in seat 0's graveyard)", o)
	}
	turnkeyID := searchMoveByName(t, e, "Choice Probe", state.ZBattlefield)
	if o := e.G.Obj(turnkeyID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("turnkey precondition: %+v (want the Ogre on the battlefield under seat 0)", o)
	}

	ask := passPriorityUntilNonPriority(t, e)
	if ask.Kind != decision.KChoose || ask.ResumeKind != "choice" {
		t.Fatalf("mid-resolution ask = %+v, want the ChangeZone \"choice\" KChoose for TrigChangeZone", ask)
	}
	if ask.Player != 1 {
		t.Fatalf("ChangeZone mid-resolution target ask posed to seat %d, want the opponent seat 1 (TargetingPlayer$ Opponent)", ask.Player)
	}
	// Legality stays controller-relative: the offered card is seat 0's own
	// graveyard creature.
	offered := false
	for _, o := range ask.Options {
		if o.Obj == gy {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("choice ask did not offer seat 0's graveyard card %d: %+v", gy, ask.Options)
	}
	replayCheck(t, e, cfg)
}
