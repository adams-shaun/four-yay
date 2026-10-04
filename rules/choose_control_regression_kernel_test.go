package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPlanetaryAnnihilationEachPlayerChoosesOwnLandKernel: Defined$ Player
// walks every chooser in turn and ControlledByPlayer$ Chooser filters each
// player's options down to their own land.
func TestPlanetaryAnnihilationEachPlayerChoosesOwnLandKernel(t *testing.T) {
	t.Parallel()
	decks := make([][]*cards.Card, 4)
	for i := range decks {
		decks[i] = mountainDeck(t, 40)
	}
	e := New(Config{Seed: 712, Names: []string{"a", "b", "c", "d"}, Decks: decks})
	planet := e.G.AddObject(choiceCorpusCard(t, "Planetary Annihilation"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: planet.ID, From: state.ZLibrary, To: state.ZStack})
	lands := make([]state.ObjID, 4)
	for p := range lands {
		o := e.G.AddObject(card(t, "Name:Land\nTypes:Land\nOracle:x\n"), state.PlayerID(p))
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		lands[p] = o.ID
	}
	face := planet.Face()
	kr4Resolve(e, func() *effects.Ctx {
		ctx := &effects.Ctx{Source: planet.ID, Controller: 0}
		effects.SetSVars(ctx, face.SVars)
		return ctx
	}, face.SpellAbility())
	for p := range lands {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Player != state.PlayerID(p) || len(d.Options) != 1 || d.Options[0].Obj != lands[p] {
			t.Fatalf("chooser %d saw %+v, want only land %d", p, d, lands[p])
		}
		submitChoices(t, e, d.Options[0].Index)
	}
}

// TestCommandeerChangesTargetAfterAnsweredChoiceKernel drives ChangeTargets
// through a real answer: the redirected spell targets the chosen player.
func TestCommandeerChangesTargetAfterAnsweredChoiceKernel(t *testing.T) {
	t.Parallel()
	decks := [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}
	e := New(Config{Seed: 714, Names: []string{"a", "b"}, Decks: decks})
	target := e.G.AddObject(card(t, "Name:Burn\nTypes:Instant\nManaCost:R\nA:SP$ DealDamage | ValidTgts$ Player | NumDmg$ 1\nOracle:x\n"), 1)
	commandeer := e.G.AddObject(choiceCorpusCard(t, "Commandeer"), 0)
	for _, id := range []state.ObjID{target.ID, commandeer.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZStack})
	}
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: target.ID, Player: 1, Amount: 1})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: commandeer.ID, IDs: []state.ObjID{target.ID}})
	sa := cards.ResolveSVar(commandeer.Face().SVars, "DBChooseTargets")
	kr4Resolve(e, func() *effects.Ctx {
		return &effects.Ctx{Source: commandeer.ID, Controller: 0, Targets: commandeer.Targets, SVars: commandeer.Face().SVars}
	}, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) < 2 {
		t.Fatalf("ChangeTargets did not ask: %+v", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 0 {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("player 0 not offered: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	if got := e.G.Obj(target.ID).Targets; len(got) != 1 || !got[0].IsPlayer || got[0].Player != 0 {
		t.Fatalf("Commandeer target = %+v, want player 0", got)
	}
}

// TestWishclawChosenPlayerGainsControlKernel: Wishclaw Talisman's DBChoose
// offers the one opponent, who gains control of it.
func TestWishclawChosenPlayerGainsControlKernel(t *testing.T) {
	t.Parallel()
	e := New(Config{Seed: 716, Names: []string{"a", "b"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	wish := e.G.AddObject(choiceCorpusCard(t, "Wishclaw Talisman"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: wish.ID, From: state.ZLibrary, To: state.ZBattlefield})
	sa := cards.ResolveSVar(wish.Face().SVars, "DBChoose")
	e.emit(events.Event{Kind: events.AbilityPush, Obj: wish.ID, Player: 0, Amount: 0})
	kr4Resolve(e, func() *effects.Ctx {
		return &effects.Ctx{Source: wish.ID, Controller: 0, SVars: wish.Face().SVars}
	}, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Player != 1 {
		t.Fatalf("Wishclaw player choice = %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(wish.ID).Controller != 1 {
		t.Fatalf("Wishclaw controller = %d, want 1", e.G.Obj(wish.ID).Controller)
	}
}

// TestReboundTargetRestrictionOnlyOffersPlayersKernel: Rebound's change-target
// ask offers only players (the redirected spell's legal player targets), never
// the creature on the board.
func TestReboundTargetRestrictionOnlyOffersPlayersKernel(t *testing.T) {
	t.Parallel()
	e := New(Config{Seed: 717, Names: []string{"a", "b"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	spell := e.G.AddObject(card(t, "Name:Flexible\nTypes:Instant\nManaCost:R\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"), 1)
	rebound := e.G.AddObject(choiceCorpusCard(t, "Rebound"), 0)
	creature := e.G.AddObject(card(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{spell.ID, rebound.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZStack})
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: creature.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: spell.ID, Player: 0, Amount: 1})
	kr4Resolve(e, func() *effects.Ctx {
		return &effects.Ctx{Source: rebound.ID, Controller: 0, Targets: []state.Target{{Obj: spell.ID}}, SVars: rebound.Face().SVars}
	}, rebound.Face().SpellAbility())
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Rebound did not ask: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind != "player" {
			t.Fatalf("Rebound offered non-player target %+v (creature %d)", o, creature.ID)
		}
	}
}

// TestValleymakerChoosePlayerOffersEveryPlayerKernel: the no-Choices$
// ChoosePlayer offers every living player, and the chosen player -- not the
// controller -- adds the mana.
func TestValleymakerChoosePlayerOffersEveryPlayerKernel(t *testing.T) {
	t.Parallel()
	e := New(Config{Seed: 718, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}})
	vm := e.G.AddObject(choiceCorpusCard(t, "Valleymaker"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: vm.ID, From: state.ZLibrary, To: state.ZBattlefield})
	idx := abilityIndex(t, vm.Card, "ChoosePlayer")
	e.emit(events.Event{Kind: events.AbilityPush, Obj: vm.ID, Player: 0, Amount: int32(idx)})
	kr4Resolve(e, func() *effects.Ctx {
		return &effects.Ctx{Source: vm.ID, Controller: 0, SVars: vm.Face().SVars}
	}, vm.Face().Abilities[idx])
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.Min != 1 || d.Max != 1 || len(d.Options) != 3 {
		t.Fatalf("Valleymaker choice = %+v, want controller choosing one of three players", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 2 {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("player 2 not offered: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	if got := e.G.Players[2].Pool[state.MG]; got != 3 {
		t.Fatalf("chosen player's green mana = %d, want 3", got)
	}
	if got := e.G.Players[0].Pool[state.MG]; got != 0 {
		t.Fatalf("controller's green mana = %d, want 0", got)
	}
}

// TestMoxDiamondReanimatedFinishesTheReanimatingSpellKernel: Mox Diamond's
// replacement asks while a reanimating sorcery resolves; after the discard
// answers, the rest of the sorcery's chain (gain 3) runs and it leaves the
// stack.
func TestMoxDiamondReanimatedFinishesTheReanimatingSpellKernel(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 735)
	mox := e.G.AddObject(choiceCorpusCard(t, "Mox Diamond"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: mox.ID, From: state.ZLibrary, To: state.ZGraveyard})
	seedLands(t, e, 0, 3)
	spell := e.G.AddObject(card(t, "Name:Return\nTypes:Sorcery\nManaCost:B\n"+
		"A:SP$ ChangeZone | ValidTgts$ Artifact | TgtZone$ Graveyard | Origin$ Graveyard | Destination$ Battlefield | SubAbility$ DBGain\n"+
		"SVar:DBGain:DB$ GainLife | Defined$ You | LifeAmount$ 3\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell.ID, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: spell.ID, IDs: []state.ObjID{mox.ID}})
	life := e.G.Players[0].Life
	e.pending = nil
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.ResumeKind != "discard_may" {
		t.Fatalf("Mox Diamond's replacement did not pose the may-discard election: %+v", d)
	}
	submitChoices(t, e, 0) // yes
	d = e.Pending()
	if d == nil || d.ResumeKind != "discard" {
		t.Fatalf("Mox Diamond's replacement did not ask for the land: %+v", d)
	}
	land := kr4OptionKind(d, "discard")
	if land < 0 {
		t.Fatalf("no land offered for the discard: %+v", d.Options)
	}
	submitChoices(t, e, land)
	if z := e.G.Obj(mox.ID).Zone; z != state.ZBattlefield {
		t.Fatalf("Mox zone = %v, want battlefield", z)
	}
	if got := e.G.Players[0].Life; got != life+3 {
		t.Fatalf("life = %d, want %d: the reanimating spell's chain stopped at the replacement", got, life+3)
	}
	if z := e.G.Obj(spell.ID).Zone; z != state.ZGraveyard || len(e.G.Stack) != 0 {
		t.Fatalf("sorcery zone = %v stack = %v, want graveyard and an empty stack", z, e.G.Stack)
	}
}

// TestDeflectingSwatOffersTheSpellControllersLegalTargetsKernel: CR 115.7 --
// the new target must be legal for the redirected spell from ITS controller's
// side: our other creature is offered, the caster's own is not.
func TestDeflectingSwatOffersTheSpellControllersLegalTargetsKernel(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 736)
	mineA := onBoard(t, e, 0, "Name:A\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	mineB := onBoard(t, e, 0, "Name:B\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	theirs := onBoard(t, e, 1, "Name:C\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	murder := e.G.AddObject(card(t, "Name:Murder\nTypes:Instant\nManaCost:B\nA:SP$ Destroy | ValidTgts$ Creature.OppCtrl\nOracle:x\n"), 1)
	swat := e.G.AddObject(choiceCorpusCard(t, "Deflecting Swat"), 0)
	for _, id := range []state.ObjID{murder.ID, swat.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZStack})
	}
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: murder.ID, IDs: []state.ObjID{mineA}})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: swat.ID, IDs: []state.ObjID{murder.ID}})
	kr4Resolve(e, func() *effects.Ctx {
		return &effects.Ctx{Source: swat.ID, Controller: 0, Targets: []state.Target{{Obj: murder.ID}},
			TargetsOffered: true, SVars: swat.Face().SVars}
	}, swat.Face().SpellAbility())
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Deflecting Swat did not ask: %+v", d)
	}
	if kr4Offers(d, theirs) {
		t.Fatalf("offered the caster's own creature, illegal for its spell: %+v", d.Options)
	}
	submitChoices(t, e, kr4Option(t, d, mineB))
	if got := e.G.Obj(murder.ID).Targets; len(got) != 1 || got[0].Obj != mineB {
		t.Fatalf("Murder targets = %+v, want creature %d", got, mineB)
	}
}

// TestBraidsRepeatEachSeesTheSacrificedCardKernel drives Braids, Arisen
// Nightmare's end-step trigger: the controller's optional sacrifice takes the
// relic, the opponent (no sharing-type permanent) loses 2, and Braids's
// controller draws.
func TestBraidsRepeatEachSeesTheSacrificedCardKernel(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 737)
	relic := onBoard(t, e, 0, "Name:Relic\nTypes:Artifact\nOracle:x\n")
	braids := onBoardCard(t, e, 0, choiceCorpusCard(t, "Braids, Arisen Nightmare"))
	hand, life := len(e.G.Zone(state.ZHand, 0)), e.G.Players[1].Life
	e.emit(events.Event{Kind: events.TriggerPush, Obj: braids, Player: 0, Amount: 0})
	e.pending = nil
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 1 {
		t.Fatalf("Braids's optional sacrifice did not ask: %+v", d)
	}
	submitChoices(t, e, kr4Option(t, d, relic))
	if e.G.Obj(braids).Zone != state.ZBattlefield || e.G.Obj(relic).Zone != state.ZGraveyard {
		t.Fatalf("Braids zone %v relic zone %v, want Braids kept and the relic sacrificed",
			e.G.Obj(braids).Zone, e.G.Obj(relic).Zone)
	}
	if got := e.G.Players[1].Life; got != life-2 {
		t.Fatalf("opponent life = %d, want %d", got, life-2)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+1 {
		t.Fatalf("Braids controller hand = %d, want %d", got, hand+1)
	}
}

// TestFecundityOffersTheStolenCreaturesLastControllerKernel: CR 603.10a --
// Fecundity's "that creature's controller may draw" asks, and draws for, the
// taker who controlled the creature as it died.
func TestFecundityOffersTheStolenCreaturesLastControllerKernel(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 738)
	onBoardCard(t, e, 1, choiceCorpusCard(t, "Fecundity"))
	stealAndKill(t, e)
	if len(e.G.Stack) != 1 {
		t.Fatalf("Fecundity trigger not on the stack: %v", e.G.Stack)
	}
	hands := [2]int{len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZHand, 1))}
	e.pending = nil
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional || d.Player != 0 {
		t.Fatalf("Fecundity's optional draw asked %+v, want the taker (player 0)", d)
	}
	yes := kr4OptionKind(d, "yes")
	if yes < 0 {
		t.Fatalf("no yes option: %+v", d.Options)
	}
	submitChoices(t, e, yes)
	if got := [2]int{len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZHand, 1))}; got != [2]int{hands[0] + 1, hands[1]} {
		t.Fatalf("hands = %v, want the taker to draw: %v", got, [2]int{hands[0] + 1, hands[1]})
	}
}
