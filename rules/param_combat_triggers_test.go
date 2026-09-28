package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the combat/damage trigger parameters the param census
// retired (rules/paramcensus_test.go's knownUnsupportedParams):
//
//   - trig:BecomesTarget.ValidSource (Reality Smasher's
//     ValidSource$ Spell.OppCtrl, Thunderbreak Regent's
//     ValidSource$ SpellAbility.OppCtrl): the "becomes the target of a
//     spell or ability an opponent controls" family fires only when the
//     TARGETING spell or ability -- the TargetsChosen event's Obj -- matches
//     the given validity expression;
//   - trig:Attacks.Secondary (Grave Titan, Sun Titan, Tome of Legends):
//     a Secondary$ True trigger is the second half of one card text ("enters
//     or attacks") and still fires on its own when its paired primary did
//     not fire for the event;
//   - trig:DamageDoneOnce.ActiveZones (Sower of Discord): the trigger-side
//     ActiveZones$ gate, read by the shared zoneGate beside TriggerZones$.
//
// The corpus card scripts are read from disk (gitignored, GPL); the
// synthetic pairing fixture is inline.

// targetingBoard deals a two-seat engine with the given cards placed on seat
// 0's battlefield and in seat 1's hand, the clock at Main1 of seat 1's turn
// (so an instant cast from either seat is legal), and returns the engine plus
// a name -> object id map over both sets. Placements mutate setup state only
// (the same shape onBoard uses); nothing here is replayed.
func targetingBoard(t *testing.T, reg *cards.Registry, board0, hand1 []string) (*Engine, map[string]state.ObjID) {
	t.Helper()
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	ids := map[string]state.ObjID{}
	var handIDs []state.ObjID
	for _, name := range hand1 {
		o := e.G.AddObject(mustCorpusCard(t, reg, name), 1)
		o.Zone = state.ZHand
		handIDs = append(handIDs, o.ID)
		ids[o.Face().Name] = o.ID
	}
	e.G.SetZone(state.ZHand, 1, handIDs)
	var boardIDs []state.ObjID
	for _, name := range board0 {
		id := onBoardCard(t, e, 0, mustCorpusCard(t, reg, name))
		boardIDs = append(boardIDs, id)
		ids[e.G.Obj(id).Face().Name] = id
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 1, 1
	return e, ids
}

// castStrikeAt casts seat 1's (or seat 0's, via askPriority's seat) Lightning
// Strike from the hand the board dealt, names want as its target, and returns
// after the cast is fully paid. The caller holds priority afterwards.
func castStrikeAt(t *testing.T, e *Engine, strikeID, want state.ObjID, seat state.PlayerID) {
	t.Helper()
	e.G.Active, e.G.Priority = seat, seat
	e.askPriority(seat)
	submitChoices(t, e, passToCast(t, e, strikeID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision after casting the strike, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == want {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("target %d not offered: %+v", want, d.Options)
	}
	submitChoices(t, e, idx)
}

// TestBecomesTargetValidSourceGatesTheFiring pins
// param:trig:BecomesTarget.ValidSource on the real corpus card: Reality
// Smasher's trigger fires when an OPPONENT's spell targets it -- and its
// unless-discard counter then counters that spell when the payer declines --
// but does not fire at all when its own controller's spell is the targeting
// one.
func TestBecomesTargetValidSourceGatesTheFiring(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, ids := targetingBoard(t, reg,
		[]string{"Reality Smasher"},
		[]string{"Lightning Strike"})
	smasher, strike := ids["Reality Smasher"], ids["Lightning Strike"]
	e.G.Players[1].Pool[state.MR] = 3

	castStrikeAt(t, e, strike, smasher, 1)
	// The targeting spell's TargetsChosen fired the Smasher's trigger; a pass
	// puts it on the stack and it resolves into the unless-discard ask.
	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Reality Smasher's targeting counter")
	}
	if pay.Player != 1 {
		t.Fatalf("unless_pay payer = seat %d, want the targeting spell's controller (seat 1)", pay.Player)
	}
	// Decline the discard: the spell is countered.
	submitChoices(t, e, pay.Options[len(pay.Options)-1].Index)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(strike).Zone; z != state.ZGraveyard {
		t.Fatalf("declined discard should counter the targeting spell: zone = %s, want Graveyard", z)
	}

	// The same spell cast by the SMASHER'S OWN CONTROLLER is not a "spell an
	// opponent controls": ValidSource$ must gate the trigger out entirely.
	e2, ids2 := targetingBoard(t, reg,
		[]string{"Reality Smasher"},
		[]string{"Lightning Strike"})
	smasher2, strike2 := ids2["Reality Smasher"], ids2["Lightning Strike"]
	// Hand the strike to seat 0 by moving the object (setup-only mutation).
	o := e2.G.Obj(strike2)
	o.Owner, o.Controller = 0, 0
	e2.G.SetZone(state.ZHand, 1, nil)
	e2.G.SetZone(state.ZHand, 0, append(e2.G.Zone(state.ZHand, 0), strike2))
	e2.G.Players[0].Pool[state.MR] = 3
	castStrikeAt(t, e2, strike2, smasher2, 0)
	passUntilStackEmpty(t, e2, 20)
	// The strike resolved (a resolved instant lands in its owner's graveyard)
	// and dealt its 3 to the Smasher: no counter stood in the way.
	if z := e2.G.Obj(strike2).Zone; z != state.ZGraveyard {
		t.Fatalf("own-controller spell should simply have resolved: zone = %s", z)
	}
	if e2.G.Obj(smasher2).Damage != 3 {
		t.Fatalf("own spell should simply have resolved onto the Smasher: damage = %d, want 3", e2.G.Obj(smasher2).Damage)
	}
}

// TestBecomesTargetValidSourceSpellAbilityOppCtrl pins the SpellAbility base
// of the same gate on Thunderbreak Regent (real corpus):
// ValidSource$ SpellAbility.OppCtrl fires for an opponent's spell and pays
// out 3 damage to that player -- and stays silent when the targeting spell
// belongs to the Regent's own controller.
func TestBecomesTargetValidSourceSpellAbilityOppCtrl(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, ids := targetingBoard(t, reg,
		[]string{"Thunderbreak Regent", "Shivan Dragon"},
		[]string{"Lightning Strike"})
	dragon, strike := ids["Shivan Dragon"], ids["Lightning Strike"]
	e.G.Players[1].Pool[state.MR] = 3

	castStrikeAt(t, e, strike, dragon, 1)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("seat 1 life after the Regent's payout = %d, want 17", got)
	}

	// The own-controller spell fires no trigger (and deals only to the Dragon).
	e2, ids2 := targetingBoard(t, reg,
		[]string{"Thunderbreak Regent", "Shivan Dragon"},
		[]string{"Lightning Strike"})
	dragon2, strike2 := ids2["Shivan Dragon"], ids2["Lightning Strike"]
	o := e2.G.Obj(strike2)
	o.Owner, o.Controller = 0, 0
	e2.G.SetZone(state.ZHand, 1, nil)
	e2.G.SetZone(state.ZHand, 0, append(e2.G.Zone(state.ZHand, 0), strike2))
	e2.G.Players[0].Pool[state.MR] = 3
	castStrikeAt(t, e2, strike2, dragon2, 0)
	passUntilStackEmpty(t, e2, 20)
	if got := e2.G.Players[1].Life; got != 20 {
		t.Fatalf("own-controller targeting fired the Regent: seat 1 life = %d, want 20", got)
	}
}

// TestAttacksSecondaryFiresAlone pins param:trig:Attacks.Secondary on the
// real corpus card: Grave Titan's Attacks trigger carries Secondary$ True
// (its pair is the ETB ChangesZone half sharing Execute$ TrigToken) and must
// still fire on its own when the attack event is the one that matched -- the
// ETB half did not fire for a DeclareAttackers.
func TestAttacksSecondaryFiresAlone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	titan := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grave Titan"))
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{titan}})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want exactly the Titan's attack trigger", e.G.Stack)
	}
	if e.G.Obj(e.G.Stack[0]).Ability == nil {
		t.Fatal("expected the stack object to be an ability")
	}
}

// TestSecondaryYieldsToItsPairedPrimary pins the pairing gate itself on a
// synthetic pair that shares its Execute$ SVar (the Grave Titan shape: the
// two halves of one card text, the second marked Secondary$ True). One
// Damage event that matches BOTH halves queues only the primary; the same
// secondary fires alone when the primary's spec fails; and the same pair
// WITHOUT the Secondary$ marker queues both.
//
// The pair deliberately shares Mode AND Execute$: with the two halves
// sharing the one Execute$ SVar, the yield's pairing (strictly by shared
// Execute$ since the r2 review) recognises them as one card text. A pair of
// same-Mode triggers with DISTINCT Execute$ SVars is two card texts -- the
// Zoraline shape -- and never yields; that contract is pinned on the real
// corpus card below.
func TestSecondaryYieldsToItsPairedPrimary(t *testing.T) {
	t.Parallel()
	pair := func(secondary bool) string {
		mark := ""
		if secondary {
			mark = " | Secondary$ True"
		}
		return "Name:Paired\nManaCost:2 B\nTypes:Creature Horror\nPT:2/2\n" +
			"T:Mode$ DamageDone | ValidTarget$ Player | Execute$ TrigLife" + mark + " | TriggerZones$ Battlefield | TriggerDescription$ primary\n" +
			"T:Mode$ DamageDone | ValidTarget$ Opponent | Execute$ TrigLife | TriggerZones$ Battlefield | TriggerDescription$ secondary\n" +
			"SVar:TrigLife:DB$ LoseLife | LifeAmount$ 2 | Defined$ You\n" +
			"Oracle:x\n"
	}

	// Both halves match one opponent-damage event: the marked secondary
	// yields, one trigger queues.
	e := layerEngine(t)
	onBoard(t, e, 0, pair(true))
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("secondary did not yield: stack = %v, want exactly the primary", e.G.Stack)
	}

	// The primary's spec fails for own-seat damage; the secondary fires alone.
	e2 := layerEngine(t)
	onBoard(t, e2, 0, pair(true))
	e2.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
	e2.putTriggersOnStack()
	if len(e2.G.Stack) != 1 {
		t.Fatalf("secondary should fire alone when the primary does not match: stack = %v", e2.G.Stack)
	}

	// Without the marker the same event queues both halves -- the yield is
	// the parameter's doing, not the shape's. Two pending triggers from one
	// source drain into an APNAP ordering decision rather than the stack, so
	// the count is read off the queue.
	e3 := layerEngine(t)
	onBoard(t, e3, 0, pair(false))
	e3.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	if len(e3.pendingTriggers) != 2 {
		t.Fatalf("unmarked pair should queue both halves: pending = %d", len(e3.pendingTriggers))
	}
}

// TestSecondaryDoesNotSuppressAnIndependentCoFiringTrigger pins the r2
// review's MAJOR finding on the real corpus card: Zoraline, Cosmos Caller
// carries two Attacks triggers on ONE face -- her printed "Whenever a Bat
// you control attacks, you gain 1 life" (Execute$ TrigGainLife) and the
// Secondary$-marked "enters or attacks" half (Execute$ TrigImmediateTrig).
// They are two card texts, not two halves of one: they share no Execute$
// SVar, so the pairing gate must never treat the printed trigger as the
// marked half's primary, and when Zoraline herself attacks (a Bat she
// controls) BOTH must fire. A Mode-equality pairing fallback suppressed the
// marked half here because the printed trigger matched the same event.
func TestSecondaryDoesNotSuppressAnIndependentCoFiringTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	zoraline := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Zoraline, Cosmos Caller"))
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{zoraline}})
	// Both halves of the SAME face fire for one DeclareAttackers, so no
	// APNAP order ask is posed (one controller) -- the count is read off the
	// queue exactly as the unmarked-pair case above does.
	if len(e.pendingTriggers) != 2 {
		t.Fatalf("pending = %d, want both of Zoraline's attack triggers", len(e.pendingTriggers))
	}
}

// TestSowerOfDiscordChosenPairLosesLife pins both of Sower of Discord's
// retired census labels together, end to end on the real corpus card: its
// enters-the-battlefield ChoosePlayer pair records the two chosen players
// (the first in Remembered, the second as Chosen), and each DamageDoneOnce
// half reflects damage from one chosen player onto the other -- the primary
// (ValidTarget$ Player.Chosen) reflects onto IsRemembered, the Secondary$
// half onto Chosen, under ActiveZones$ Battlefield.
func TestSowerOfDiscordChosenPairLosesLife(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Sower of Discord"))
	e.G.Players[0].Pool[state.MB] = 6
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, handIDsByFace(e)["Sower of Discord"]))
	// The ETB replacement asks twice (after the priority round that resolves
	// the Sower): first over every living player, then over the non-chosen
	// remainder. Option 0 both times: player 0 is the Remembered player,
	// player 1 the Chosen one.
	for i := 0; i < 2; i++ {
		for n := 0; ; n++ {
			d := e.Pending()
			if n > 10 || d == nil {
				t.Fatalf("no choose-player ask #%d arrived", i+1)
			}
			if d.Kind == decision.KChoose {
				break
			}
			if d.Kind != decision.KPriority {
				t.Fatalf("unexpected decision while seeking choose ask #%d: %+v", i+1, d)
			}
			castFirst(t, e, "pass")
		}
		d := e.Pending()
		submitChoices(t, e, d.Options[0].Index)
	}

	// Damage to the CHOSEN player (seat 1): the primary half fires and the
	// REMEMBERED player (seat 0) loses that much life.
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 4})
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != 16 {
		t.Fatalf("seat 0 life after damage to the chosen player = %d, want 16", got)
	}

	// Damage to the REMEMBERED player (seat 0): the secondary half fires and
	// the CHOSEN player loses that much life.
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 20)
	// Seat 1 also took the synthetic damage itself (20 -> 16) before the
	// reflection reached it; seat 0 has now lost both legs (20 -> 16 -> 13).
	if got := e.G.Players[1].Life; got != 13 {
		t.Fatalf("seat 1 life after damage to the remembered player = %d, want 13", got)
	}
	if got := e.G.Players[0].Life; got != 13 {
		t.Fatalf("seat 0 life after both reflections = %d, want 13", got)
	}
}

// TestTriggerActiveZonesGate pins param:trig:DamageDoneOnce.ActiveZones on
// the shared zone gate: an explicit ActiveZones$ is authoritative exactly
// like TriggerZones$, so a DamageDone trigger whose source sits in the
// graveyard fires under ActiveZones$ Graveyard and stays silent under the
// battlefield default.
func TestTriggerActiveZonesGate(t *testing.T) {
	t.Parallel()
	cardFor := func(zones string) string {
		return "Name:Warden\nManaCost:1 W\nTypes:Creature Soldier\nPT:1/1\n" +
			"T:Mode$ DamageDone | ActiveZones$ " + zones + " | ValidTarget$ Player | Execute$ TrigGain | TriggerDescription$ x\n" +
			"SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You\n" +
			"Oracle:x\n"
	}
	inGraveyard := func(t *testing.T, e *Engine, src string) state.ObjID {
		t.Helper()
		o := e.G.AddObject(card(t, src), 0)
		o.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), o.ID))
		return o.ID
	}

	// Graveyard source, graveyard gate: fires.
	e := layerEngine(t)
	inGraveyard(t, e, cardFor("Graveyard"))
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("ActiveZones$ Graveyard should admit a graveyard source: stack = %v", e.G.Stack)
	}

	// Graveyard source, battlefield gate (Forge's default zone): silent.
	e2 := layerEngine(t)
	inGraveyard(t, e2, cardFor("Battlefield"))
	e2.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e2.putTriggersOnStack()
	if len(e2.G.Stack) != 0 {
		t.Fatalf("ActiveZones$ Battlefield must gate a graveyard source out: stack = %v", e2.G.Stack)
	}
}
