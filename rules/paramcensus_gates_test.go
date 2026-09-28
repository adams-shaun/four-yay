// Task ds4 (paramcensus ConditionCheckSVar/CheckSVar + Sacrifice Optional):
// corpus-carried behaviour tests for the gates the task taught the engine to
// read. The cards are pulled from the real compiled corpus (choiceCorpusCard)
// so the fixtures pin the cards' ACTUAL script shapes, not a paraphrase; the
// per-test comment states which measured population each represents.
//
// The three families covered:
//
//   - Vampire Lacerator: a Phase trigger's ConditionCheckSVar$ /
//     ConditionSVarCompare$ gate over Count$PlayerCountOpponents$ (GE11) --
//     the LoseLife/Draw/ChangeZone/DealDamage/Reveal ConditionCheckSVar$
//     cluster's dominant shape (the 54 bare-Kicked lines aside).
//   - Scapeshift: effSacrifice's Optional$ True may-ask -- the sacrifice is
//     declined or taken, and the chained search scales to Remembered$Amount.
//   - Bloodsoaked Champion: an AB's CheckSVar$/SVarCompare$ activation gate
//     read at OFFER time (rules/legal.go sVarGateOK) -- Count$AttackersDeclared
//     zero means the legal-action walk does not offer the return.
//   - Into the Roil: the bare Condition$ Kicked branch of conditionMet -- the
//     draw leg fires only when the spell was actually cast kicked.
//   - Desecration Demon: the Optional$ ask's per-target binding -- a
//     multi-player optional sacrifice asks each targeted player in turn
//     (each answer re-enters scoped to the exact target that asked,
//     `ResumeTarget`), so the table does NOT wedge (the r2 review's
//     measured infinite re-ask is closed by the target binding, not by
//     suppressing the ask).
//
// Every fixture card is a REAL deck card (the corpus protagonist plus authored
// extras) moved with logged MoveZone events, so each test's replayCheck holds
// -- the same discipline newFixtureDeck's doc records. Card scripts are never
// copied from Forge's .cards/cardsfolder (GPL); the corpus cards here are
// loaded from the gitignored corpus at test time.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// gateFixture builds a two-seat game at Main 1 of turn 1 (seat 0 starting)
// whose seat-0 deck is the named CORPUS card first, then the authored extras,
// then Mountains; the protagonist is bridged to seat 0's hand (the
// newFixtureDeck discipline, replay-safe). The extras stay in the library so
// a caller can move them with logged MoveZone events.
func gateFixture(t *testing.T, seed uint64, name string, extras ...string) (*Engine, Config, state.ObjID) {
	t.Helper()
	fixture := choiceCorpusCard(t, name)
	deck := []*cards.Card{fixture}
	for _, extra := range extras {
		deck = append(deck, card(t, extra))
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(append([]*cards.Card{fixture}, deck[1:]...), mountainDeck(t, 40-len(deck))...),
			mountainDeck(t, 40),
		},
		Tokens: map[string]*cards.Card{},
	})
	e := New(cfg)
	e.Advance()
	id := findInZones(t, e, 0, name)
	if inZone(e, state.ZLibrary, 0, id) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	toMain1(t, e)
	return e, cfg, id
}

// gateMoveFromLibrary moves one of the extras (dealt to hand or still in the
// library -- the opening hand takes the deck's top seven) to the given zone
// with a logged event, returning its object id.
func gateMoveFromLibrary(t *testing.T, e *Engine, name string, to state.Zone) state.ObjID {
	t.Helper()
	id := findInZones(t, e, 0, name)
	from := state.ZLibrary
	if inZone(e, state.ZHand, 0, id) {
		from = state.ZHand
	} else if !inZone(e, state.ZLibrary, 0, id) {
		t.Fatalf("%q not in seat 0's hand or library", name)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
	return id
}

// findInZones locates the object id whose face name matches in seat 0's
// hand or library (nil otherwise); t.Fatals only when the caller asks it to.
func findInZones(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, cand := range e.G.Zone(z, p) {
			if e.G.Obj(cand).Face().Name == name {
				return cand
			}
		}
	}
	return 0
}

// TestVampireLaceratorUpkeepGateLosesOneLife drives the real corpus trigger:
// with every opponent above 10 life the ConditionSVarCompare$ GE11 gate holds
// and the controller loses 1 life at their upkeep.
func TestVampireLaceratorUpkeepGateLosesOneLife(t *testing.T) {
	t.Parallel()
	e, cfg, lac := gateFixture(t, 901, "Vampire Lacerator")
	e.emit(events.Event{Kind: events.MoveZone, Obj: lac, From: state.ZHand, To: state.ZBattlefield})
	before := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.TriggerPush, Obj: lac, Player: 0, Amount: 0})
	e.resolveTop()
	if got := e.G.Players[0].Life; got != before-1 {
		t.Fatalf("life = %d, want %d (gate holds at opponent life %d)",
			got, before-1, e.G.Players[1].Life)
	}
	if e.G.Obj(lac).Zone != state.ZBattlefield {
		t.Fatalf("Lacerator zone %v, want kept on the battlefield", e.G.Obj(lac).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestVampireLaceratorUpkeepGateSparedUnderTen is the other side of the same
// gate: an opponent at 10 or less makes PlayerCountOpponents$LowestLifeTotal
// read 10, GE11 fails, and the upkeep loss does not happen.
func TestVampireLaceratorUpkeepGateSparedUnderTen(t *testing.T) {
	t.Parallel()
	e, cfg, lac := gateFixture(t, 902, "Vampire Lacerator")
	e.emit(events.Event{Kind: events.MoveZone, Obj: lac, From: state.ZHand, To: state.ZBattlefield})
	// The single opponent at the spared threshold, set through a logged
	// LifeChange so the replayCheck below reconstructs it.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -10})
	before := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.TriggerPush, Obj: lac, Player: 0, Amount: 0})
	e.resolveTop()
	if got := e.G.Players[0].Life; got != before {
		t.Fatalf("life = %d, want unchanged %d (gate fails at opponent life 10)", got, before)
	}
	replayCheck(t, e, cfg)
}

const gateLandSrc = "Name:Gate Land\nTypes:Land\nOracle:x\n"

// TestScapeshiftOptionalDeclineSacrificesNothing answers the may-ask with the
// empty choice: no land is sacrificed, and the chained search sees
// Remembered$Amount 0 -- it finds nothing (ChangeNum 0 completes the
// fail-to-find directly) but still shuffles, and Cleanup clears Remembered.
// The ask is 0..Amount: Amount$ SacX resolves through
// SVar:SacX:Count$Valid Land.YouCtrl (= 2 here), so Max is 2, the card's own
// "any number of lands".
func TestScapeshiftOptionalDeclineSacrificesNothing(t *testing.T) {
	t.Parallel()
	e, cfg, sp := gateFixture(t, 903, "Scapeshift", gateLandSrc, gateLandSrc)
	addMana(t, e, 0, "GGGG")
	l1 := gateMoveFromLibrary(t, e, "Gate Land", state.ZBattlefield)
	l2 := gateMoveFromLibrary(t, e, "Gate Land", state.ZBattlefield)
	castFixture(t, e, sp, -1) // cast + resolve up to the sacrifice ask
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 2 {
		t.Fatalf("optional sacrifice ask missing: %+v (Max 2 = Amount$ SacX, Count$Valid Land.YouCtrl)", d)
	}
	for _, o := range d.Options {
		if o.Obj != l1 && o.Obj != l2 {
			t.Fatalf("ask offered a non-land option: %+v", o)
		}
	}
	submitChoices(t, e) // the empty answer declines
	if e.G.Obj(sp).Zone != state.ZGraveyard {
		t.Fatalf("Scapeshift zone %v, want resolved to the graveyard", e.G.Obj(sp).Zone)
	}
	for i, l := range []state.ObjID{l1, l2} {
		if e.G.Obj(l).Zone != state.ZBattlefield {
			t.Fatalf("land %d zone %v, want kept (declined)", i, e.G.Obj(l).Zone)
		}
	}
	replayCheck(t, e, cfg)
}

// TestScapeshiftOptionalAcceptSacrificesAndSearches takes one land from the
// same ask: it dies to the graveyard, the chained library search asks for up
// to one land (Remembered$Amount 1), and the chosen Mountain enters tapped.
func TestScapeshiftOptionalAcceptSacrificesAndSearches(t *testing.T) {
	t.Parallel()
	e, cfg, sp := gateFixture(t, 904, "Scapeshift", gateLandSrc, gateLandSrc)
	addMana(t, e, 0, "GGGG")
	l1 := gateMoveFromLibrary(t, e, "Gate Land", state.ZBattlefield)
	gateMoveFromLibrary(t, e, "Gate Land", state.ZBattlefield)
	castFixture(t, e, sp, -1)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 2 {
		t.Fatalf("optional sacrifice ask missing: %+v (want 0..2, Amount$ SacX)", d)
	}
	var landIdx int
	for _, o := range d.Options {
		if o.Obj == l1 {
			landIdx = o.Index
		}
	}
	submitChoices(t, e, landIdx)
	if e.G.Obj(l1).Zone != state.ZGraveyard {
		t.Fatalf("chosen land zone %v, want sacrificed to the graveyard", e.G.Obj(l1).Zone)
	}
	// The search: up to one land from the (Mountain) library -- the options
	// list every eligible Mountain, Max caps the answer at one.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 1 || len(d.Options) == 0 {
		t.Fatalf("search ask missing after the sacrifice: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind != "search" {
			t.Fatalf("search option kind %q: %+v", o.Kind, o)
		}
	}
	mountain := d.Options[0].Obj
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(sp).Zone != state.ZGraveyard {
		t.Fatalf("Scapeshift zone %v, want resolved", e.G.Obj(sp).Zone)
	}
	if mo := e.G.Obj(mountain); mo == nil || mo.Zone != state.ZBattlefield || !mo.Tapped {
		t.Fatalf("found mountain = %+v, want on the battlefield tapped", mo)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != libBefore-1 {
		t.Fatalf("library size %d, want %d (one taken, shuffle preserves the count)", got, libBefore-1)
	}
	replayCheck(t, e, cfg)
}

// TestBloodsoakedChampionRaidGateOffersOnlyAfterAttacking is the offer-time
// CheckSVar$ read (rules/legal.go sVarGateOK): with zero attackers declared
// this turn the Raid gate fails and the legal-action walk does not offer the
// return; with one attacker it does.
func TestBloodsoakedChampionRaidGateOffersOnlyAfterAttacking(t *testing.T) {
	t.Parallel()
	e, cfg, champ := gateFixture(t, 905, "Bloodsoaked Champion",
		"Name:Raider\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: champ, From: state.ZHand, To: state.ZGraveyard})
	addMana(t, e, 0, "BB")
	raider := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	for _, o := range e.legalActions(0) {
		if o.Obj == champ {
			t.Fatalf("Raid gate did not withhold the return with zero attackers: %+v", o)
		}
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{raider}})
	found := false
	for _, o := range e.legalActions(0) {
		if o.Obj == champ {
			found = true
		}
	}
	if !found {
		t.Fatalf("Raid gate withheld the return despite one attacker: %+v", e.legalActions(0))
	}
	replayCheck(t, e, cfg)
}

// TestGateChainWiring pins the shared evaluator's wiring once: the AB offer
// gate, the statics wrapper and conditionMet all resolve their gates through
// effects.CheckSVarHolds's SVar-table lookup (ctx table first, then the
// source face's), and the (holds, evaluated) verdict distinguishes a modelled
// head that counts zero -- enforced -- from an unmodelled body -- not
// evaluated, the fail-open contract the three call sites each document.
func TestGateChainWiring(t *testing.T) {
	e, _, id := gateFixture(t, 906, "Bloodsoaked Champion")
	o := e.G.Obj(id)
	// Face() hands out the registry-singleton card's face shared with every
	// other holder of this card -- restore the original table when the test
	// ends so no later test reads the polluted map (order-dependent rot).
	origSVars := o.Face().SVars
	t.Cleanup(func() { o.Face().SVars = origSVars })
	o.Face().SVars = map[string]string{"X": "Count$xPaid"}
	ctx := &effects.Ctx{Source: id, Controller: 0, SVars: o.Face().SVars, X: 3}
	holds, evaluated := effects.CheckSVarHolds(e, ctx, "X", "GE3")
	if !evaluated || !holds {
		t.Fatalf("CheckSVarHolds(X, GE3) = (%v, %v), want (true, true)", holds, evaluated)
	}
	// An unmodelled body (Count$TotalDamageDoneByThisTurn is not a modelled
	// head: a Damage event does not record its source) is NOT evaluated --
	// the verdict the three call sites fail open on.
	ctx2 := &effects.Ctx{Source: id, Controller: 0, SVars: map[string]string{"Y": "Count$TotalDamageDoneByThisTurn"}}
	if _, evaluated := effects.CheckSVarHolds(e, ctx2, "Y", "EQ4"); evaluated {
		t.Fatal("Count$TotalDamageDoneByThisTurn reported evaluated -- the fail-open contract is rotting")
	}
	// A modelled head that counts zero is still evaluated (the distinction
	// the whole verdict mechanism exists for).
	ctx3 := &effects.Ctx{Source: id, Controller: 0, SVars: map[string]string{"Z": "Count$AttackersDeclared"}}
	holds, evaluated = effects.CheckSVarHolds(e, ctx3, "Z", "EQ0")
	if !evaluated || !holds {
		t.Fatalf("CheckSVarHolds(AttackersDeclared EQ0) = (%v, %v), want (true, true)", holds, evaluated)
	}
}

const gateRaiderSrc = "Name:Raider\nTypes:Creature\nPT:1/1\nOracle:x\n"

// TestIntoTheRoilKickedConditionDrawsOnlyWhenKicked drives the real corpus
// card's bare `Condition$ Kicked` gate (effects/conditions.go): the chained
// DB$ Draw leg runs only when the spell was cast with its Kicker paid (the
// source's FlagKicked cast bit), not on the plain cast. Both casts return the
// targeted permanent to its owner's hand either way; only the kicked cast
// draws.
func TestIntoTheRoilKickedConditionDrawsOnlyWhenKicked(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		kicked bool
	}{
		{"kicked", true},
		{"unkicked", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, roil := gateFixture(t, 907, "Into the Roil", gateRaiderSrc)
			raider := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
			addMana(t, e, 0, "UUUU") // {1}{U} either way; the kicker wants two more
			d := e.Pending()
			if d == nil || d.Kind != decision.KPriority {
				t.Fatalf("not at priority: %+v", d)
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "cast" && o.Obj == roil && o.Mode == map[bool]string{true: "kicked", false: ""}[tc.kicked] {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("no %s cast option: %+v", tc.name, d.Options)
			}
			handBefore := len(e.G.Zone(state.ZHand, 0))
			libBefore := len(e.G.Zone(state.ZLibrary, 0))
			submitChoices(t, e, idx)
			d = e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("target ask missing: %+v", d)
			}
			tIdx := -1
			for _, o := range d.Options {
				if o.Obj == raider {
					tIdx = o.Index
				}
			}
			if tIdx < 0 {
				t.Fatalf("raider not offered as the target: %+v", d.Options)
			}
			submitChoices(t, e, tIdx)
			passUntilStackEmpty(t, e, 20)
			if z := e.G.Obj(roil).Zone; z != state.ZGraveyard {
				t.Fatalf("Into the Roil zone %v, want the graveyard", z)
			}
			if z := e.G.Obj(raider).Zone; z != state.ZHand {
				t.Fatalf("raider zone %v, want returned to hand", z)
			}
			// The leg under test: exactly one extra card drawn (hand +1, library
			// -1) when kicked, none otherwise. The raider's return accounts for
			// the -1/+1 of the spell leaving the hand and the raider entering it.
			wantHand, wantLib := handBefore, libBefore
			if tc.kicked {
				wantHand, wantLib = handBefore+1, libBefore-1
			}
			if got := len(e.G.Zone(state.ZHand, 0)); got != wantHand {
				t.Fatalf("hand = %d, want %d", got, wantHand)
			}
			if got := len(e.G.Zone(state.ZLibrary, 0)); got != wantLib {
				t.Fatalf("library = %d, want %d", got, wantLib)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestDesecrationDemonMultiTargetOptionalDoesNotWedge pins the Optional$
// ask's per-target binding on the real corpus card the r2 review wedged with:
// with TWO opponents, Defined$ Opponent + Optional$ True + a real host made
// the shared "sacrifice" resume arm re-run effSacrifice's target walk, target
// 1 consumed target 2's answer, and seat 2 was re-asked forever (11
// consecutive asks before the probe capped). The merged engine binds each
// ask to its exact target (ResumeTarget), so the ask RUNS -- each targeted
// opponent is asked in turn -- and the table does not wedge: a decline moves
// on to the next target's own ask, an accepted sacrifice is remembered only
// for the target that took it, and the RememberSacrificed$ chain (tap +
// P1P1 counter, gated on Remembered$Amount) fires for the sacrifice that
// happened.
func TestDesecrationDemonMultiTargetOptionalDoesNotWedge(t *testing.T) {
	t.Parallel()
	victim := "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n"
	fixture := choiceCorpusCard(t, "Desecration Demon")
	cfg := seatZeroStart(Config{Seed: 908, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{fixture}, mountainDeck(t, 39)...),
			append([]*cards.Card{card(t, victim)}, mountainDeck(t, 39)...),
			append([]*cards.Card{card(t, victim)}, mountainDeck(t, 39)...),
		},
		Tokens: map[string]*cards.Card{},
	})
	e := New(cfg)
	e.Advance()
	demon := findInZones(t, e, 0, "Desecration Demon")
	if demon == 0 {
		t.Fatal("Desecration Demon not in seat 0's hand or library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: demon, From: state.ZHand, To: state.ZBattlefield})
	var victims []state.ObjID
	for _, p := range []state.PlayerID{1, 2} {
		id := findInZones(t, e, p, "Victim")
		if id == 0 {
			t.Fatalf("Victim not in seat %d's hand or library", p)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
		victims = append(victims, id)
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: demon, Player: 0, Amount: 0})
	e.resolveTop()
	// Seat 1 is asked first: a 0..1 may-ask over its own Victim.
	d1 := e.Pending()
	if d1 == nil || d1.Kind != decision.KChoose || d1.Player != 1 || d1.Min != 0 || d1.Max != 1 || len(d1.Options) != 1 || d1.Options[0].Obj != victims[0] {
		t.Fatalf("seat 1's optional sacrifice ask missing or wrong: %+v", d1)
	}
	submitChoices(t, e) // the empty answer declines
	// Seat 2 then gets its OWN ask (the per-target binding): the decline did
	// not consume it and it is not a re-ask of seat 1.
	d2 := e.Pending()
	if d2 == nil || d2.Kind != decision.KChoose || d2.Player != 2 || d2.Min != 0 || d2.Max != 1 || len(d2.Options) != 1 || d2.Options[0].Obj != victims[1] {
		t.Fatalf("seat 2's optional sacrifice ask missing or wrong: %+v", d2)
	}
	submitChoices(t, e, 0) // seat 2 accepts: sacrifice its Victim
	// Resolution completed -- no third ask, no wedge. A priority decision is
	// the ordinary post-resolution game flow, not an ask.
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("an ask decision is still pending after both targets answered: %+v", d)
	}
	if z := e.G.Obj(victims[0]).Zone; z != state.ZBattlefield {
		t.Fatalf("victim 1 zone %v, want kept (seat 1 declined)", z)
	}
	if z := e.G.Obj(victims[1]).Zone; z != state.ZGraveyard {
		t.Fatalf("victim 2 zone %v, want sacrificed (seat 2 accepted)", z)
	}
	dm := e.G.Obj(demon)
	if !dm.Tapped || dm.Counter("P1P1") != 1 {
		t.Fatalf("demon tapped=%v P1P1=%d, want tapped +1 (a sacrifice happened)", dm.Tapped, dm.Counter("P1P1"))
	}
	replayCheck(t, e, cfg)
}
