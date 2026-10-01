package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins planeswalker loyalty (CR 306.5b, 306.8, 606.3, 704.5i) end
// to end on real corpus cards: Jace, the Mind Sculptor and Gideon, Ally of
// Zendikar enter with their starting loyalty, Lightning Bolt and Firebolt
// remove loyalty counters rather than marking damage, a 3-loyalty Jace dies
// to exactly 3 damage (the reported defect), and the loyalty abilities'
// costs and CR 606.3 gating behave. The engine pieces under test are the
// battlefield-entry grant in events.Apply's Move (CR 306.5b), the
// walker-damage exchange in effects/damage.go (CR 306.8), the zero-loyalty
// state-based action in rules/sba.go (CR 704.5i), and the ParseCost
// AddCounter<LOYALTY> component plus rules/legal.go's ability gating.

// walkerBoard seeds seat 0 with the named corpus planeswalker, puts every
// copy onto the battlefield through LOGGED MoveZone events (so the CR 306.5b
// entry grant runs for real and paid activations replay), moves every card in
// onBoard onto the battlefield the same way, and stops at turn 2 seat 0 Main1.
func walkerBoard(t *testing.T, reg *cards.Registry, name string, onBoard ...*cards.Card) (*Engine, Config, state.ObjID) {
	t.Helper()
	walker := mustCorpusCard(t, reg, name)
	deck := append([]*cards.Card{walker}, onBoard...)
	cfg := Config{Seed: 31, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(deck, mountainDeck(t, 40-len(deck))...),
			mountainDeck(t, 40),
		},
	}
	// Seat 0 is the protagonist (the board parks at seat 0 turns);
	// seatZeroStart advances the seed until the CR 103.1 toss starts seat 0.
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	var id state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 {
			continue
		}
		if o.Card == walker {
			id = o.ID
		}
		if onDeckHas(onBoard, o.Card) {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
		}
	}
	if id == 0 {
		t.Fatalf("no %q copy in seat 0's deck", name)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg, id
}

func onDeckHas(cs []*cards.Card, c *cards.Card) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

// cardToHand moves one owner-0 copy of c to hand through a logged MoveZone,
// so a later cast of it replays. A copy already in hand is left where it is;
// one found on the battlefield (walkerBoard puts every onBoard card there)
// or in the library is moved from wherever it is.
func cardToHand(t *testing.T, e *Engine, c *cards.Card) {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == c && o.Zone == state.ZHand {
			return
		}
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == c {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZHand})
			return
		}
	}
	t.Fatalf("no owner-0 copy of %s", c.Faces[0].Name)
}

// targetObject answers a pending KTarget decision by choosing the option for
// obj (or player, for player targets), fatal when absent.
func targetObject(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("object %d not offered: %+v", obj, d.Options)
}

// targetPlayer answers a pending KTarget decision by choosing the option for
// the given player seat, fatal when absent.
func targetPlayer(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == p {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("player %d not offered: %+v", p, d.Options)
}

// jaceAbility returns the index of the walker's loyalty ability whose cost
// matches kind ("AddCounter" for [+N], "SubCounter" for [-N]) and value n.
func jaceAbility(t *testing.T, c *cards.Card, kind string, n int32) int {
	t.Helper()
	for i, sa := range c.Faces[0].Abilities {
		cost := ParseCost(sa.Params["Cost"])
		for _, part := range cost.AddCounter {
			if kind == "AddCounter" && part.N == n && part.Spec == "LOYALTY" {
				return i
			}
		}
		for _, part := range cost.SubCounter {
			if kind == "SubCounter" && part.N == n && part.Spec == "LOYALTY" {
				return i
			}
		}
	}
	t.Fatalf("no %s %d loyalty ability on %s", kind, n, c.Faces[0].Name)
	return -1
}

// loyaltyAbilityOffered reports whether the pending-game offer for p includes
// the (obj, ability) loyalty option.
func loyaltyAbilityOffered(e *Engine, p state.PlayerID, obj state.ObjID, ability int) bool {
	for _, o := range e.legalActions(p) {
		if o.Kind == "ability" && o.Obj == obj && o.Ability == ability {
			return true
		}
	}
	return false
}

func TestPlaneswalkerEntersWithStartingLoyalty(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name string
		want int32
	}{
		{"Jace, the Mind Sculptor", 3},
		{"Gideon, Ally of Zendikar", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, id := walkerBoard(t, reg, tc.name)
			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield {
				t.Fatalf("zone %s", o.Zone)
			}
			if got := o.Counter("LOYALTY"); got != tc.want {
				t.Fatalf("entering loyalty = %d, want %d (CR 306.5b)", got, tc.want)
			}
			if o.Damage != 0 {
				t.Fatalf("walker entered with %d damage marked", o.Damage)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestLightningBoltKillsAThreeLoyaltyJace is the reported defect, executable:
// a 3-loyalty Jace dies to Lightning Bolt, and the damage arrives as loyalty
// removal, never as marked damage.
func TestLightningBoltKillsAThreeLoyaltyJace(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bolt := mustCorpusCard(t, reg, "Lightning Bolt")
	e, cfg, jace := walkerBoard(t, reg, "Jace, the Mind Sculptor", bolt)
	if e.G.Obj(jace).Counter("LOYALTY") != 3 {
		t.Fatalf("Jace entered with %d loyalty", e.G.Obj(jace).Counter("LOYALTY"))
	}
	cardToHand(t, e, bolt)
	addMana(t, e, 0, "R")
	castFirst(t, e, "cast")
	targetObject(t, e, jace)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(jace).Zone != state.ZGraveyard {
		t.Fatalf("3-loyalty Jace survived a bolt in %s (the reported defect)", e.G.Obj(jace).Zone)
	}
	if o := e.G.Obj(jace); o.Damage != 0 {
		t.Fatalf("pure walker marked %d damage; spell damage must remove loyalty (CR 306.8/120.3c)", o.Damage)
	}
	// Entry loyalty is a real CounterChange; the damage-to-loyalty
	// conversion is folded into Damage (no CounterChange AFTER the hit).
	entryIndex, damageIndex := -1, -1
	sawSBA := false
	for i, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == jace && ev.Counter == "LOYALTY" && ev.Amount == 3 {
			entryIndex = i
		}
		if ev.Kind == events.MoveZone && ev.Obj == jace && ev.To == state.ZGraveyard && ev.Text == "zero loyalty" {
			sawSBA = true
		}
		if ev.Kind == events.Damage && ev.Obj == jace {
			damageIndex = i
		}
	}
	if entryIndex < 0 || damageIndex <= entryIndex || !sawSBA {
		t.Fatalf("log exchange missing entry, Damage or zero-loyalty SBA: entry %d damage %d SBA %v", entryIndex, damageIndex, sawSBA)
	}
	for _, ev := range e.L.Events[damageIndex+1:] {
		if ev.Kind == events.CounterChange && ev.Obj == jace && ev.Counter == "LOYALTY" {
			t.Fatalf("damage-to-loyalty conversion emitted CounterChange: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}

// TestFireboltRemovesLoyaltyWithoutKilling: 2 damage to a 3-loyalty Jace
// leaves it alive at 1 loyalty, still not damage-marked.
func TestFireboltRemovesLoyaltyWithoutKilling(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	firebolt := mustCorpusCard(t, reg, "Firebolt")
	e, cfg, jace := walkerBoard(t, reg, "Jace, the Mind Sculptor", firebolt)
	cardToHand(t, e, firebolt)
	addMana(t, e, 0, "R")
	castFirst(t, e, "cast")
	targetObject(t, e, jace)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(jace)
	if o.Zone != state.ZBattlefield || o.Counter("LOYALTY") != 1 || o.Damage != 0 {
		t.Fatalf("after Firebolt: zone %s loyalty %d damage %d", o.Zone, o.Counter("LOYALTY"), o.Damage)
	}
	replayCheck(t, e, cfg)
}

// TestJacePlusTwoCostsNoMana: the [+2] ability is offered and activatable
// from an EMPTY pool (AddCounter is a free cost component, never the
// one-generic fallback), pays nothing, and moves loyalty 3 -> 5.
func TestJacePlusTwoCostsNoMana(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jaceCard := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	plusTwo := jaceAbility(t, jaceCard, "AddCounter", 2)
	e, cfg, jace := walkerBoard(t, reg, "Jace, the Mind Sculptor")
	e.Advance()
	if !loyaltyAbilityOffered(e, 0, jace, plusTwo) {
		t.Fatal("[+2] not offered from an empty pool")
	}
	opt := abilityOption(t, e, jace, plusTwo)
	submitChoices(t, e, opt.Index)
	// [+2] targets a player; answer with the opponent, then let it resolve.
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("[+2] charged %d mana", e.G.Players[0].Pool.Total())
	}
	if got := e.G.Obj(jace).Counter("LOYALTY"); got != 5 {
		t.Fatalf("loyalty after [+2] = %d, want 5", got)
	}
	replayCheck(t, e, cfg)
}

// TestJaceLoyaltyAbilityOncePerTurnAndOwnTurnOnly: after one activation NO
// loyalty ability of that walker is re-offered this turn -- the gate is per
// PERMANENT (CR 606.3), not per ability index -- and off the controller's own
// turn no loyalty ability is offered at all; the next own turn re-offers.
func TestJaceLoyaltyAbilityOncePerTurnAndOwnTurnOnly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jaceCard := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	plusTwo := jaceAbility(t, jaceCard, "AddCounter", 2)
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, jace := walkerBoard(t, reg, "Jace, the Mind Sculptor", bear)
	e.Advance()
	if !loyaltyAbilityOffered(e, 0, jace, plusTwo) {
		t.Fatal("[+2] not offered on the controller's own turn")
	}
	opt := abilityOption(t, e, jace, plusTwo)
	submitChoices(t, e, opt.Index)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if loyaltyAbilityOffered(e, 0, jace, plusTwo) {
		t.Fatal("[+2] re-offered in the same turn (CR 606.3 once-per-turn)")
	}
	if loyaltyAbilityOffered(e, 0, jace, 1) {
		t.Fatal("the [0] draw-three index still offered after [+2] (CR 606.3 is per PERMANENT, not per ability index)")
	}
	// Off-turn: hand the turn to seat 1 and re-ask seat 0's options.
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 3})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == jace {
			t.Fatalf("loyalty ability %d offered on an opponent's turn", o.Ability)
		}
	}
	// Next own turn: the [+2] index is available again.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 4})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	if !loyaltyAbilityOffered(e, 0, jace, plusTwo) {
		t.Fatal("[+2] not re-offered on the next turn")
	}
	replayCheck(t, e, cfg)
}

// TestWalkerLeaveAndReturnMayActivateAgain (CR 400.7): a permanent that
// leaves the battlefield and returns is a NEW object, so the old stint's
// activation does not block the returned permanent's loyalty abilities this
// turn. The re-entry also re-grants starting loyalty (CR 306.5b), so the
// cost is payable again.
func TestWalkerLeaveAndReturnMayActivateAgain(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jaceCard := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	plusTwo := jaceAbility(t, jaceCard, "AddCounter", 2)
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, jace := walkerBoard(t, reg, "Jace, the Mind Sculptor", bear)
	e.Advance()
	opt := abilityOption(t, e, jace, plusTwo)
	submitChoices(t, e, opt.Index)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if loyaltyAbilityOffered(e, 0, jace, plusTwo) {
		t.Fatal("[+2] re-offered in the same turn (the once gate should hold)")
	}
	// Exile and return the same turn, through logged MoveZone events.
	e.emit(events.Event{Kind: events.MoveZone, Obj: jace, From: state.ZBattlefield, To: state.ZExile})
	e.emit(events.Event{Kind: events.MoveZone, Obj: jace, From: state.ZExile, To: state.ZBattlefield})
	// The priority decision captured before the moves is stale; re-drive the
	// offer loop the way the live Submit loop would (attach_test.go's
	// e.pending = nil pattern).
	e.pending = nil
	e.Advance()
	if got := e.G.Obj(jace).Counter("LOYALTY"); got != 3 {
		t.Fatalf("returned walker loyalty = %d, want the 3 the re-entry re-grants (CR 306.5b)", got)
	}
	if !loyaltyAbilityOffered(e, 0, jace, plusTwo) {
		t.Fatal("returned walker's [+2] withheld (CR 400.7: the returned permanent is a new object)")
	}
	// And the new stint's own once-per-turn gate holds from zero.
	opt = abilityOption(t, e, jace, plusTwo)
	submitChoices(t, e, opt.Index)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if loyaltyAbilityOffered(e, 0, jace, plusTwo) {
		t.Fatal("[+2] re-offered after the new stint's first activation")
	}
	replayCheck(t, e, cfg)
}

// recordLoyaltyPush records a real AbilityPush without resolving its effect,
// then removes its transient stack object so legal-action generation again has
// an empty-stack sorcery window. It is deliberately used only for log-folding
// probes: normal activation flow is covered by the end-to-end Jace tests.
func recordLoyaltyPush(e *Engine, walker state.ObjID, ability int) {
	e.emit(events.Event{Kind: events.AbilityPush, Obj: walker, Player: 0, Amount: int32(ability)})
	e.emit(events.Event{Kind: events.MoveZone, Obj: e.G.NextID - 1, From: state.ZStack, To: state.ZExile})
}

// TestLoyaltyActivationUsesFaceAtPush proves the CR 606.3 scan classifies an
// AbilityPush by the source face active WHEN it was pushed, not the source's
// current face. The back face deliberately places its loyalty ability at a
// different index, the shape a current-face lookup would lose after FlipFace.
func TestLoyaltyActivationUsesFaceAtPush(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, walker := walkerBoard(t, reg, "Jace, the Mind Sculptor")
	e.G.Obj(walker).Card = &cards.Card{Faces: []*cards.Face{
		{Name: "front", Types: []string{"Planeswalker"}, Loyalty: "3", Abilities: []*cards.SA{
			{Kind: "AB", API: "Draw", Params: map[string]string{"Planeswalker": "True"}},
		}},
		{Name: "back", Types: []string{"Planeswalker"}, Loyalty: "3", Abilities: []*cards.SA{
			{Kind: "AB", API: "Draw", Params: map[string]string{}},
			{Kind: "AB", API: "Draw", Params: map[string]string{"Planeswalker": "True"}},
		}},
	}}
	e.G.Obj(walker).SyncFace()
	recordLoyaltyPush(e, walker, 0)
	e.emit(events.Event{Kind: events.FlipFace, Obj: walker, Amount: 1})
	if got := e.loyaltyActivationsThisTurn(walker); got != 1 {
		t.Fatalf("loyalty activations after front-face push and flip = %d, want 1", got)
	}
	if loyaltyAbilityOffered(e, 0, walker, 1) {
		t.Fatal("back-face loyalty ability offered after a front-face activation")
	}
}

// TestLoyaltyStintUsesFoldedZoneHistory makes the MoveZone From fields lie in
// both directions. events.Move uses the object's actual zone, so the loyalty
// stint fold must too: a same-zone battlefield re-append cannot reset the
// gate, while a real leave/re-entry must reset it even when both From fields
// claim the opposite.
func TestLoyaltyStintUsesFoldedZoneHistory(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jaceCard := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	plusTwo := jaceAbility(t, jaceCard, "AddCounter", 2)

	t.Run("stale entry on battlefield reappend does not reset", func(t *testing.T) {
		e, cfg, walker := walkerBoard(t, reg, "Jace, the Mind Sculptor")
		recordLoyaltyPush(e, walker, plusTwo)
		// Actual battlefield -> battlefield, despite From claiming exile.
		e.emit(events.Event{Kind: events.MoveZone, Obj: walker, From: state.ZExile, To: state.ZBattlefield})
		if got := e.loyaltyActivationsThisTurn(walker); got != 1 {
			t.Fatalf("activations after same-zone reappend = %d, want 1", got)
		}
		if loyaltyAbilityOffered(e, 0, walker, plusTwo) {
			t.Fatal("same-zone reappend reset the loyalty gate")
		}
		replayCheck(t, e, cfg)
	})

	t.Run("stale exit and entry still reset on real reentry", func(t *testing.T) {
		e, cfg, walker := walkerBoard(t, reg, "Jace, the Mind Sculptor")
		recordLoyaltyPush(e, walker, plusTwo)
		// Actual battlefield -> exile, then exile -> battlefield. Both From
		// values are stale, so an Event.From-based scan misses both crossings.
		e.emit(events.Event{Kind: events.MoveZone, Obj: walker, From: state.ZExile, To: state.ZExile})
		e.emit(events.Event{Kind: events.MoveZone, Obj: walker, From: state.ZBattlefield, To: state.ZBattlefield})
		if got := e.loyaltyActivationsThisTurn(walker); got != 0 {
			t.Fatalf("activations after real leave/re-entry = %d, want 0", got)
		}
		if !loyaltyAbilityOffered(e, 0, walker, plusTwo) {
			t.Fatal("real leave/re-entry did not reset the loyalty gate")
		}
		replayCheck(t, e, cfg)
	})
}

// TestLoyaltyGateIgnoresRejectedNegativeAbilityPush exercises option building
// after a hostile logged AbilityPush. Apply rejects a negative ability index;
// the historical gate must also bounds-check it rather than panicking.
func TestLoyaltyGateIgnoresRejectedNegativeAbilityPush(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jaceCard := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	plusTwo := jaceAbility(t, jaceCard, "AddCounter", 2)
	e, cfg, walker := walkerBoard(t, reg, "Jace, the Mind Sculptor")
	e.emit(events.Event{Kind: events.AbilityPush, Obj: walker, Player: 0, Amount: -1})
	if !loyaltyAbilityOffered(e, 0, walker, plusTwo) {
		t.Fatal("rejected negative AbilityPush withheld a legal loyalty ability")
	}
	replayCheck(t, e, cfg)
}

// TestRejectedTurnChangeDoesNotResetLoyaltyGate proves the historical CR
// 606.3 fold rejects the same malformed TurnChange as events.Apply. Logging an
// out-of-range player must not create a new activation window while the real
// turn and active player remain unchanged.
func TestRejectedTurnChangeDoesNotResetLoyaltyGate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jaceCard := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	plusTwo := jaceAbility(t, jaceCard, "AddCounter", 2)
	e, cfg, walker := walkerBoard(t, reg, "Jace, the Mind Sculptor")
	recordLoyaltyPush(e, walker, plusTwo)
	turn, active := e.G.Turn, e.G.Active
	e.emit(events.Event{Kind: events.TurnChange, Player: state.PlayerID(255), Amount: turn + 1})
	if e.G.Turn != turn || e.G.Active != active {
		t.Fatalf("rejected TurnChange mutated turn/active: got %d/%d, want %d/%d", e.G.Turn, e.G.Active, turn, active)
	}
	if got := e.loyaltyActivationsThisTurn(walker); got != 1 {
		t.Fatalf("activations after rejected TurnChange = %d, want 1", got)
	}
	if loyaltyAbilityOffered(e, 0, walker, plusTwo) {
		t.Fatal("rejected TurnChange reset the loyalty activation gate")
	}
	replayCheck(t, e, cfg)
}

// TestOathOfTeferiGrantsASecondLoyaltyActivation: the S:Mode$ NumLoyaltyAct
// static (Twice$ True, ValidCard$ Planeswalker.YouCtrl) raises the
// per-permanent limit from 1 to 2 for planeswalkers the enchantment's
// controller controls, so the walker may activate twice this turn -- including
// the same ability twice (Urza, Lord Protector's reminder text is explicit
// that this is allowed) -- and a third activation is withheld.
func TestOathOfTeferiGrantsASecondLoyaltyActivation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jaceCard := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	oathCard := mustCorpusCard(t, reg, "Oath of Teferi")
	plusTwo := jaceAbility(t, jaceCard, "AddCounter", 2)
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	mtn := mountainDeck(t, 1) // a harmless "another permanent" target for Oath's ETB
	e, cfg, jace := walkerBoard(t, reg, "Jace, the Mind Sculptor", oathCard, bear, mtn[0])
	var mtnID state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == mtn[0] && o.Zone == state.ZBattlefield {
			mtnID = o.ID
		}
	}
	e.Advance()
	// Oath's ETB exile trigger is pending before priority; answer it with the
	// mountain and let the trigger resolve off the stack -- a loyalty ability
	// needs the empty-stack sorcery window, so it cannot be offered while the
	// trigger waits.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		targetObject(t, e, mtnID)
		passUntilStackEmpty(t, e, 20)
		e.Advance()
	}
	opt := abilityOption(t, e, jace, plusTwo)
	submitChoices(t, e, opt.Index)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if !loyaltyAbilityOffered(e, 0, jace, plusTwo) {
		t.Fatal("[+2] not offered a second time under Oath of Teferi (limit should be 2)")
	}
	opt = abilityOption(t, e, jace, plusTwo)
	submitChoices(t, e, opt.Index)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if loyaltyAbilityOffered(e, 0, jace, plusTwo) {
		t.Fatal("[+2] offered a third time (Oath grants exactly two activations)")
	}
	replayCheck(t, e, cfg)
}

// TestLoyaltyAbilityLimitCombinesGrantsIndependentOfOrder pins the structural
// combination rule for NumLoyaltyAct: Twice raises the base to two and
// Additional adds on top, regardless of deterministic battlefield scan order.
func TestLoyaltyAbilityLimitCombinesGrantsIndependentOfOrder(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	grant := func(name string, params map[string]string) *cards.Card {
		return &cards.Card{Faces: []*cards.Face{{
			Name: name, Types: []string{"Enchantment"},
			Statics: []cards.Static{{Mode: "NumLoyaltyAct", Params: params}},
		}}}
	}
	twice := grant("Twice grant", map[string]string{
		"ValidCard": "Planeswalker.YouCtrl",
		"Twice":     "True",
	})
	additional := grant("Additional grant", map[string]string{
		"ValidCard":  "Planeswalker.YouCtrl",
		"Additional": "1",
	})
	for _, tc := range []struct {
		name   string
		grants []*cards.Card
	}{
		{name: "additional before twice", grants: []*cards.Card{additional, twice}},
		{name: "twice before additional", grants: []*cards.Card{twice, additional}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, walker := walkerBoard(t, reg, "Jace, the Mind Sculptor", tc.grants...)
			if got := e.loyaltyAbilityLimit(walker); got != 3 {
				t.Fatalf("combined loyalty activation limit = %d, want 3", got)
			}
		})
	}
}

// TestJaceMinusOneAtOneLoyaltyIsOfferedAndKills: the [-1] ability is gated by
// castable's counter check (offered at exactly 1 loyalty, where spending it
// reaches 0 -- the offer gate is Counter >= N, so the gate fires only below
// the cost), and spending the last loyalty feeds the CR 704.5i sweep.
func TestJaceMinusOneAtOneLoyaltyIsOfferedAndKills(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jaceCard := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	minusOne := jaceAbility(t, jaceCard, "SubCounter", 1)
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, jace := walkerBoard(t, reg, "Jace, the Mind Sculptor", bear)
	// Spend the walker down to exactly 1 loyalty with logged CounterChange
	// events before the turn's priority ask is built.
	e.emit(events.Event{Kind: events.CounterChange, Obj: jace, Counter: "LOYALTY", Amount: -2})
	e.Advance()
	if !loyaltyAbilityOffered(e, 0, jace, minusOne) {
		t.Fatal("[-1] not offered at exactly 1 loyalty (castable's counter gate is Counter >= N)")
	}
	opt := abilityOption(t, e, jace, minusOne)
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bearObjID(t, e, bear))
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(jace).Zone != state.ZGraveyard {
		t.Fatalf("Jace at 0 loyalty stayed in %s (CR 704.5i)", e.G.Obj(jace).Zone)
	}
	replayCheck(t, e, cfg)
}

func bearObjID(t *testing.T, e *Engine, bear *cards.Card) state.ObjID {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == bear && o.Zone == state.ZBattlefield {
			return o.ID
		}
	}
	t.Fatal("no bear on the battlefield")
	return 0
}

// TestBrotherhoodsEndDamageAllRemovesWalkerLoyalty covers the OTHER shared
// caller of the walker-damage exchange, effDamageAll (a sweep, not a targeted
// deal): Brotherhood's End's DamageAll mode marks 3 on the bear (which the
// lethal-damage SBA then removes) and removes 3 loyalty from Jace (which the
// zero-loyalty SBA then removes), through one resolution.
func TestBrotherhoodsEndDamageAllRemovesWalkerLoyalty(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	endCard := mustCorpusCard(t, reg, "Brotherhood's End")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, jace := walkerBoard(t, reg, "Jace, the Mind Sculptor", bear, endCard)
	bearID := bearObjID(t, e, bear)
	cardToHand(t, e, endCard)
	addMana(t, e, 0, "RRR")
	castFirst(t, e, "cast")
	// The Charm-mode ask: choose the DamageAll mode (Choices$ DBDamage first).
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("want modes decision, got %+v", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(jace).Zone; z != state.ZGraveyard {
		t.Fatalf("3-loyalty Jace survived a 3-wide DamageAll in %s", z)
	}
	if z := e.G.Obj(bearID).Zone; z != state.ZGraveyard {
		t.Fatalf("bear survived in %s", z)
	}
	entryIndex, damageIndex := -1, -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == jace && ev.Counter == "LOYALTY" && ev.Amount == 3 {
			entryIndex = i
		}
		if ev.Kind == events.Damage && ev.Obj == jace {
			damageIndex = i
		}
	}
	if entryIndex < 0 || damageIndex <= entryIndex {
		t.Fatalf("sweep exchange missing entry loyalty or Damage: entry %d damage %d", entryIndex, damageIndex)
	}
	for _, ev := range e.L.Events[damageIndex+1:] {
		if ev.Kind == events.CounterChange && ev.Obj == jace && ev.Counter == "LOYALTY" {
			t.Fatalf("sweep damage-to-loyalty conversion emitted CounterChange: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}

// TestParseCostLoyaltyTokens pins the cost grammar: an AddCounter<N/LOYALTY>
// token is a free AddCounter part (no generic), the 0 form costs nothing, the
// SubCounter form keeps its existing part, and a NON-loyalty AddCounter token
// keeps the pre-loyalty one-generic fallback.
func TestParseCostLoyaltyTokens(t *testing.T) {
	t.Parallel()
	plus := ParseCost("AddCounter<2/LOYALTY>")
	if len(plus.AddCounter) != 1 || plus.AddCounter[0].N != 2 || plus.AddCounter[0].Spec != "LOYALTY" {
		t.Fatalf("AddCounter<2/LOYALTY> = %+v", plus)
	}
	if plus.Generic != 0 || plus.CMC() != 0 {
		t.Fatalf("[+2] costed mana: generic %d cmc %d", plus.Generic, plus.CMC())
	}
	zero := ParseCost("AddCounter<0/LOYALTY>")
	if len(zero.AddCounter) != 1 || zero.AddCounter[0].N != 0 || zero.CMC() != 0 {
		t.Fatalf("AddCounter<0/LOYALTY> = %+v", zero)
	}
	minus := ParseCost("SubCounter<1/LOYALTY>")
	if len(minus.SubCounter) != 1 || minus.SubCounter[0].N != 1 || minus.SubCounter[0].Spec != "LOYALTY" {
		t.Fatalf("SubCounter<1/LOYALTY> = %+v", minus)
	}
	// A source-anchored non-loyalty AddCounter is a real free part (Wall of
	// Roots, Devoted Druid); a chooser-anchored one keeps the fallback.
	self := ParseCost("AddCounter<1/M1M1>")
	if len(self.AddCounter) != 1 || self.AddCounter[0].Spec != "M1M1" || self.Generic != 0 {
		t.Fatalf("AddCounter<1/M1M1> must be a source counter part, got %+v", self)
	}
	other := ParseCost("AddCounter<1/M1M1/Creature.YouCtrl/a creature you control>")
	if len(other.AddCounter) != 0 || other.Generic != 1 {
		t.Fatalf("chooser-anchored AddCounter must keep the fallback, got %+v", other)
	}
}
