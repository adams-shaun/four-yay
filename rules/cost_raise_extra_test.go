package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// These tests drive the REAL offer/cast/activation/payment path for RaiseCost
// statics whose Cost$ is not plain mana or life. Before the additional-cost
// bridge (rules/raise_cost_extra.go) every such Cost$ fell through to the
// Amount$ fallback -- absent, so zero -- and the additional cost was never
// charged: Brutal Suppression's "Sacrifice a land", Drought's "Sacrifice a
// Swamp" per black symbol, Tectonic Split's "sacrifice half your lands",
// Carth the Lion's extra [+1]. Every card is a compiled corpus card looked up
// by name; no Forge script text is committed.

// raiseEngine deals seat 0 a 40-card corpus deck whose first cards are the
// named fixtures over four of each basic land and Grizzly Bears, and seat 1
// the named fixtures over Mountains, then drives to seat 0's Main1.
func raiseEngine(t *testing.T, seat0, seat1 []string) (*Engine, Config) {
	t.Helper()
	reg := searchTestRegistry(t)
	build := func(fixtures []string, lands []string) []*cards.Card {
		deck := make([]*cards.Card, 0, 40)
		for _, name := range fixtures {
			deck = append(deck, searchCorpusCard(t, reg, name))
		}
		for i := 0; i < 4; i++ {
			for _, l := range lands {
				deck = append(deck, searchCorpusCard(t, reg, l))
			}
		}
		for len(deck) < 40 {
			deck = append(deck, searchCorpusCard(t, reg, "Grizzly Bears"))
		}
		return deck
	}
	cfg := seatZeroStart(Config{Seed: 4417, Names: []string{"raiser", "opponent"},
		Decks: [][]*cards.Card{
			build(seat0, []string{"Plains", "Island", "Swamp", "Mountain", "Forest"}),
			build(seat1, []string{"Mountain"}),
		}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// raiseDrive answers every non-priority decision until the pending decision
// is priority again, using pick for each (nil pick = the first Min options).
func raiseDrive(t *testing.T, e *Engine, pick func(d *decision.Decision) []int) {
	t.Helper()
	for i := 0; i < 24; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("cast/activation stalled with no decision")
		}
		if d.Kind == decision.KPriority {
			return
		}
		var choice []int
		if pick != nil {
			choice = pick(d)
		}
		if choice == nil {
			for j := 0; j < d.Min && j < len(d.Options); j++ {
				choice = append(choice, d.Options[j].Index)
			}
		}
		submitChoices(t, e, choice...)
	}
	t.Fatalf("cast/activation did not return to priority: %+v", e.Pending())
}

// raiseAllToZone moves every named card of seat's hand and library into
// zone to (a real MoveZone for each one not already there) and returns them.
func raiseAllToZone(t *testing.T, e *Engine, seat state.PlayerID, name string, to state.Zone) []state.ObjID {
	t.Helper()
	var out []state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range append([]state.ObjID(nil), e.G.Zone(z, seat)...) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil || o.Face().Name != name {
				continue
			}
			if z != to {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
			}
			out = append(out, id)
		}
	}
	e.pending = nil
	e.priorityRound()
	return out
}

// raiseMoveAnother moves the first named card of seat's hand/library that is
// NOT already in zone to (a real MoveZone), so a further copy can be fetched.
func raiseMoveAnother(t *testing.T, e *Engine, seat state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		if z == to {
			continue
		}
		for _, id := range e.G.Zone(z, seat) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("no further %q outside %s for seat %d", name, to, seat)
	return 0
}

func raiseCastOffered(e *Engine, id state.ObjID) bool {
	d := e.Pending()
	return d != nil && hasCastOption(d.Options, id)
}

func raiseCast(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == id {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no cast option for %d: %+v", id, e.Pending().Options)
}

func raiseZone(e *Engine, id state.ObjID) state.Zone {
	if o := e.G.Obj(id); o != nil {
		return o.Zone
	}
	return state.ZLibrary
}

// Brutal Suppression: "Activated abilities of nontoken Rebels cost an
// additional 'Sacrifice a land' to activate." A mandatory non-mana raise on
// an ABILITY: with no land the Rebel's ability is withheld; with one land it
// is offered, and activating it sacrifices that land.
func TestRaiseCostBrutalSuppressionSacrificesALand(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Brutal Suppression", "Rappelling Scouts"}, nil)
	blightMove(t, e, 0, "Brutal Suppression", state.ZBattlefield)
	scouts := blightMove(t, e, 0, "Rappelling Scouts", state.ZBattlefield)
	addMana(t, e, 0, "WWW")
	if _, ok := findAbilityOption(e, scouts, 0); ok {
		t.Fatal("Rappelling Scouts' ability offered with no land to sacrifice under Brutal Suppression")
	}
	plains := blightMove(t, e, 0, "Plains", state.ZBattlefield)
	opt, ok := findAbilityOption(e, scouts, 0)
	if !ok {
		t.Fatalf("Rappelling Scouts' ability not offered with {2}{W} and a land: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	raiseDrive(t, e, nil)
	if z := raiseZone(e, plains); z != state.ZGraveyard {
		t.Fatalf("Plains in %s after the activation, want graveyard (sacrificed as the additional cost)", z)
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after activation = %d, want 0 ({2}{W} still charged)", left)
	}
	replayCheck(t, e, cfg)
}

// Drought: "Spells cost an additional 'Sacrifice a Swamp' to cast for each
// black mana symbol in their mana costs." ForEachShard$ Black scales the
// raise: Phyrexian Arena ({1}{B}{B}) needs two Swamps, Lightning Bolt (no
// black symbol) needs none.
func TestRaiseCostDroughtSacrificesASwampPerBlackSymbol(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Drought", "Phyrexian Arena", "Lightning Bolt"}, nil)
	blightMove(t, e, 0, "Drought", state.ZBattlefield)
	arena := blightMove(t, e, 0, "Phyrexian Arena", state.ZHand)
	bolt := blightMove(t, e, 0, "Lightning Bolt", state.ZHand)
	swamp1 := blightMove(t, e, 0, "Swamp", state.ZBattlefield)
	addMana(t, e, 0, "BBBR")
	if !raiseCastOffered(e, bolt) {
		t.Fatal("Lightning Bolt (no black symbol) withheld under Drought")
	}
	if raiseCastOffered(e, arena) {
		t.Fatal("Phyrexian Arena offered with one Swamp under Drought; its two black symbols need two")
	}
	swamp2 := blightMove(t, e, 0, "Swamp", state.ZBattlefield)
	if !raiseCastOffered(e, arena) {
		t.Fatalf("Phyrexian Arena not offered with two Swamps and {1}{B}{B}: %+v", e.Pending().Options)
	}
	raiseCast(t, e, arena)
	raiseDrive(t, e, nil)
	for _, s := range []state.ObjID{swamp1, swamp2} {
		if z := raiseZone(e, s); z != state.ZGraveyard {
			t.Fatalf("Swamp %d in %s after casting Phyrexian Arena, want graveyard", s, z)
		}
	}
	if z := raiseZone(e, arena); z != state.ZStack {
		t.Fatalf("Phyrexian Arena in %s after the cast, want stack", z)
	}
	replayCheck(t, e, cfg)
}

// Tectonic Split: "As an additional cost to cast this spell, sacrifice half
// the lands you control, rounded up." Sac<X/Land> with SVar X the fixed
// Count$Valid Land.YouCtrl/HalfUp: three lands cost two.
func TestRaiseCostTectonicSplitSacrificesHalfTheLands(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Tectonic Split"}, nil)
	split := blightMove(t, e, 0, "Tectonic Split", state.ZHand)
	var forests []state.ObjID
	for i := 0; i < 3; i++ {
		forests = append(forests, blightMove(t, e, 0, "Forest", state.ZBattlefield))
	}
	addMana(t, e, 0, "GGGGGG")
	if !raiseCastOffered(e, split) {
		t.Fatalf("Tectonic Split not offered with {4}{G}{G} and three lands: %+v", e.Pending().Options)
	}
	raiseCast(t, e, split)
	sacAsked := false
	raiseDrive(t, e, func(d *decision.Decision) []int {
		if d.Min == 2 && d.Max == 2 {
			sacAsked = true
		}
		return nil
	})
	if !sacAsked {
		t.Fatal("no choose-two sacrifice ask for Tectonic Split's half-the-lands cost")
	}
	gone := 0
	for _, f := range forests {
		if raiseZone(e, f) == state.ZGraveyard {
			gone++
		}
	}
	if gone != 2 {
		t.Fatalf("%d of 3 lands sacrificed, want 2 (half, rounded up)", gone)
	}
	replayCheck(t, e, cfg)
}

// Carth the Lion: "Planeswalkers' loyalty abilities you activate cost an
// additional [+1] to activate." Jace Beleren's [+2] costs [+3]; his [-1]
// costs [+1][-1], a net [0] (the card's ruling: a [-N] costs [-(N-1)]).
func TestRaiseCostCarthAddsALoyaltyCounter(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Carth the Lion", "Jace Beleren"}, nil)
	blightMove(t, e, 0, "Carth the Lion", state.ZBattlefield)
	passUntilStackEmpty(t, e, 30) // Carth's enters trigger (the dig)
	jace := blightMove(t, e, 0, "Jace Beleren", state.ZBattlefield)
	if got := e.G.Obj(jace).Counter("LOYALTY"); got != 3 {
		t.Fatalf("precondition: Jace loyalty %d, want 3", got)
	}
	opt, ok := findAbilityOption(e, jace, 0)
	if !ok {
		t.Fatalf("Jace's [+2] not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	raiseDrive(t, e, nil)
	if got := e.G.Obj(jace).Counter("LOYALTY"); got != 6 {
		t.Fatalf("Jace loyalty %d after [+2] under Carth, want 6 ([+2] plus the additional [+1])", got)
	}
	replayCheck(t, e, cfg)
}

// Close Encounter's ChooseCard<...> additional cost has no payment stage in
// this build: it must fail CLOSED -- the spell is withheld -- never be cast
// with the additional cost silently dropped.
func TestRaiseCostUnpayableCostWithholdsTheSpell(t *testing.T) {
	t.Parallel()
	e, _ := raiseEngine(t, []string{"Close Encounter"}, nil)
	spell := blightMove(t, e, 0, "Close Encounter", state.ZHand)
	blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	blightMove(t, e, 1, "Mountain", state.ZBattlefield)
	addMana(t, e, 0, "GGGG")
	if raiseCastOffered(e, spell) {
		t.Fatal("Close Encounter offered although its ChooseCard additional cost cannot be paid")
	}
}

// Carth under a [-N] ability: Jace Beleren's [-1] costs [+1][-1], which nets
// to [0] -- Jace keeps all three loyalty counters.
func TestRaiseCostCarthNetsAMinusLoyaltyAbility(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Carth the Lion", "Jace Beleren"}, nil)
	blightMove(t, e, 0, "Carth the Lion", state.ZBattlefield)
	passUntilStackEmpty(t, e, 30)
	jace := blightMove(t, e, 0, "Jace Beleren", state.ZBattlefield)
	opt, ok := findAbilityOption(e, jace, 1)
	if !ok {
		t.Fatalf("Jace's [-1] not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	raiseDrive(t, e, nil)
	if got := e.G.Obj(jace).Counter("LOYALTY"); got != 3 {
		t.Fatalf("Jace loyalty %d after [-1] under Carth, want 3 (a net [0])", got)
	}
	replayCheck(t, e, cfg)
}

// Champion of the Clachan: "As an additional cost to cast this spell,
// behold a Kithkin and exile it. ... When this creature leaves the
// battlefield, return the exiled card to its owner's hand." BeholdExile<1/
// Kithkin>: with no other Kithkin the spell is withheld; with a Kithkin card
// in hand it is cast, that card is exiled linked to the Champion, and the
// leaves-the-battlefield trigger returns it.
func TestRaiseCostBeholdExileChampionOfTheClachan(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Champion of the Clachan", "Goldmeadow Harrier"}, nil)
	champ := blightMove(t, e, 0, "Champion of the Clachan", state.ZHand)
	harrier := blightMove(t, e, 0, "Goldmeadow Harrier", state.ZLibrary)
	addMana(t, e, 0, "WWWW")
	if raiseCastOffered(e, champ) {
		t.Fatal("Champion of the Clachan offered with no other Kithkin to behold")
	}
	blightMove(t, e, 0, "Goldmeadow Harrier", state.ZHand)
	if !raiseCastOffered(e, champ) {
		t.Fatalf("Champion of the Clachan not offered with a Kithkin card in hand: %+v", e.Pending().Options)
	}
	raiseCast(t, e, champ)
	raiseDrive(t, e, nil)
	o := e.G.Obj(harrier)
	if o.Zone != state.ZExile || o.ExiledWith != champ {
		t.Fatalf("beheld Harrier zone=%s exiledWith=%d, want exile linked to the Champion %d", o.Zone, o.ExiledWith, champ)
	}
	passUntilStackEmpty(t, e, 30)
	if z := raiseZone(e, champ); z != state.ZBattlefield {
		t.Fatalf("Champion in %s after resolving, want battlefield", z)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: champ, From: state.ZBattlefield, To: state.ZGraveyard})
	e.priorityRound()
	passUntilStackEmpty(t, e, 30)
	if z := raiseZone(e, harrier); z != state.ZHand {
		t.Fatalf("exiled Harrier in %s after the Champion left, want hand", z)
	}
	replayCheck(t, e, cfg)
}

// Water Whip: "As an additional cost to cast this spell, waterbend {5}.
// (While paying a waterbend cost, you can tap your artifacts and creatures
// to help. Each one pays for {1}.)" With only {U}{U} floating, four
// creatures cannot cover the {5} and the spell is withheld; five can, and
// casting it taps all five as the waterbend payment.
func TestRaiseCostWaterbendWaterWhipTapsCreatures(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Water Whip"}, nil)
	whip := blightMove(t, e, 0, "Water Whip", state.ZHand)
	var bears []state.ObjID
	for i := 0; i < 4; i++ {
		bears = append(bears, blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield))
	}
	addMana(t, e, 0, "UU")
	if raiseCastOffered(e, whip) {
		t.Fatal("Water Whip offered with {U}{U} and four creatures; waterbend {5} needs five")
	}
	bears = append(bears, blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield))
	if !raiseCastOffered(e, whip) {
		t.Fatalf("Water Whip not offered with {U}{U} and five creatures to waterbend: %+v", e.Pending().Options)
	}
	raiseCast(t, e, whip)
	waterbendAsked := false
	raiseDrive(t, e, func(d *decision.Decision) []int {
		var pick []int
		for _, o := range d.Options {
			if o.Kind == "waterbend_generic" {
				pick = append(pick, o.Index)
			}
		}
		if len(pick) > 0 {
			waterbendAsked = true
			return pick
		}
		return nil
	})
	if !waterbendAsked {
		t.Fatal("no waterbend tap announcement posed for Water Whip")
	}
	if z := raiseZone(e, whip); z != state.ZStack {
		t.Fatalf("Water Whip in %s after the cast, want stack", z)
	}
	for _, b := range bears {
		if !e.G.Obj(b).Tapped {
			t.Fatalf("creature %d untapped after waterbending {5} with five creatures", b)
		}
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after cast = %d, want 0 ({U}{U} charged)", left)
	}
	replayCheck(t, e, cfg)
}

// Crashing Wave: "As an additional cost to cast this spell, waterbend {X}."
// The Waterbend<X> raise adds an announced {X}; two tapped creatures pay an
// X of 2 and the spell records X = 2.
func TestRaiseCostWaterbendXCrashingWave(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Crashing Wave"}, nil)
	wave := blightMove(t, e, 0, "Crashing Wave", state.ZHand)
	var bears []state.ObjID
	for i := 0; i < 2; i++ {
		bears = append(bears, blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield))
	}
	addMana(t, e, 0, "UU")
	if !raiseCastOffered(e, wave) {
		t.Fatalf("Crashing Wave not offered with {U}{U}: %+v", e.Pending().Options)
	}
	raiseCast(t, e, wave)
	xSeen := false
	raiseDrive(t, e, func(d *decision.Decision) []int {
		var pick []int
		for _, o := range d.Options {
			if o.Kind == "waterbend_generic" {
				pick = append(pick, o.Index)
			}
			if o.Kind == "x" && o.Amount == 2 {
				xSeen = true
				return []int{o.Index}
			}
		}
		return pick
	})
	if !xSeen {
		t.Fatal("X = 2 never offered for Crashing Wave's waterbend {X}")
	}
	o := e.G.Obj(wave)
	if o.Zone != state.ZStack || o.X != 2 {
		t.Fatalf("Crashing Wave zone=%s X=%d, want stack with X=2", o.Zone, o.X)
	}
	for _, b := range bears {
		if !e.G.Obj(b).Tapped {
			t.Fatalf("creature %d untapped after waterbending X=2", b)
		}
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after cast = %d, want 0", left)
	}
	replayCheck(t, e, cfg)
}

// March of Reckless Joy ({X}{R}): "As an additional cost to cast this spell,
// you may exile any number of red cards from your hand. This spell costs {2}
// less to cast for each card exiled this way." The RaiseCost's
// ExileFromHand<Y> counts the Announce$ Exiled named announcement and the
// Relative$ ReduceCost reads it: with only {R} floating, announcing two
// exiled red cards pays an X of 3 ({3} - {4}, floored at zero), and both
// cards are exiled.
func TestRaiseCostMarchExilesRedCardsForTheDiscount(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"March of Reckless Joy", "Lightning Bolt", "Lightning Bolt"}, nil)
	march := blightMove(t, e, 0, "March of Reckless Joy", state.ZHand)
	bolts := raiseAllToZone(t, e, 0, "Lightning Bolt", state.ZHand)
	if len(bolts) != 2 {
		t.Fatalf("precondition: %d Lightning Bolts in hand, want 2", len(bolts))
	}
	bolt1, bolt2 := bolts[0], bolts[1]
	addMana(t, e, 0, "R")
	if !raiseCastOffered(e, march) {
		t.Fatalf("March of Reckless Joy not offered with {R}: %+v", e.Pending().Options)
	}
	raiseCast(t, e, march)
	announced, x3 := false, false
	raiseDrive(t, e, func(d *decision.Decision) []int {
		for _, o := range d.Options {
			if o.Kind == "named_announce" && o.Amount == 2 {
				announced = true
				return []int{o.Index}
			}
			if o.Kind == "x" && o.Amount == 3 {
				x3 = true
				return []int{o.Index}
			}
		}
		return nil
	})
	if !announced {
		t.Fatal("no announcement of two exiled red cards offered")
	}
	if !x3 {
		t.Fatal("X = 3 not offered with {R} floating after announcing two exiled cards ({4} less)")
	}
	for _, b := range []state.ObjID{bolt1, bolt2} {
		if z := raiseZone(e, b); z != state.ZExile {
			t.Fatalf("Lightning Bolt %d in %s after the cast, want exile (the additional cost)", b, z)
		}
	}
	o := e.G.Obj(march)
	if o.Zone != state.ZStack || o.X != 3 {
		t.Fatalf("March zone=%s X=%d, want stack with X=3", o.Zone, o.X)
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after cast = %d, want 0 (charged {R})", left)
	}
	replayCheck(t, e, cfg)
}

// Explosive Singularity ({8}{R}{R}): "As an additional cost to cast this
// spell, you may tap any number of untapped creatures you control. This
// spell costs {1} less to cast for each creature tapped this way." Seven mana
// and three creatures: the offer sweeps the announcement (three taps make it
// {5}{R}{R}), and the cast taps the three.
func TestRaiseCostExplosiveSingularityTapsForTheDiscount(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Explosive Singularity"}, nil)
	spell := blightMove(t, e, 0, "Explosive Singularity", state.ZHand)
	var bears []state.ObjID
	for i := 0; i < 2; i++ {
		bears = append(bears, blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield))
	}
	addMana(t, e, 0, "RRRRRRR")
	if raiseCastOffered(e, spell) {
		t.Fatal("Explosive Singularity offered with seven mana and two creatures; it needs eight")
	}
	bears = append(bears, blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield))
	if !raiseCastOffered(e, spell) {
		t.Fatalf("Explosive Singularity not offered with seven mana and three creatures to tap: %+v", e.Pending().Options)
	}
	raiseCast(t, e, spell)
	announced := false
	raiseDrive(t, e, func(d *decision.Decision) []int {
		for _, o := range d.Options {
			if o.Kind == "named_announce" && o.Amount == 3 {
				announced = true
				return []int{o.Index}
			}
		}
		for _, o := range d.Options {
			if o.Player == 1 && d.Kind == decision.KTarget {
				return []int{o.Index}
			}
		}
		return nil
	})
	if !announced {
		t.Fatal("no announcement of three tapped creatures offered")
	}
	for _, b := range bears {
		if !e.G.Obj(b).Tapped {
			t.Fatalf("creature %d untapped after announcing three taps", b)
		}
	}
	if z := raiseZone(e, spell); z != state.ZStack {
		t.Fatalf("Explosive Singularity in %s after the cast, want stack", z)
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after cast = %d, want 0 (charged {5}{R}{R})", left)
	}
	replayCheck(t, e, cfg)
}

// Aether Tide ({X}{U}): "As an additional cost to cast this spell, discard X
// creature cards." Discard<X/Creature> with SVar X Count$xPaid is the cast's
// own announced X: X = 2 discards exactly two creature cards, and X is capped
// by the creature cards in hand.
func TestRaiseCostAetherTideDiscardsXCreatureCards(t *testing.T) {
	t.Parallel()
	e, cfg := raiseEngine(t, []string{"Aether Tide"}, []string{"Grizzly Bears", "Grizzly Bears"})
	tide := blightMove(t, e, 0, "Aether Tide", state.ZHand)
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...) {
		if o := e.G.Obj(id); id != tide && o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
	}
	creatures := []state.ObjID{
		raiseMoveAnother(t, e, 0, "Grizzly Bears", state.ZHand),
		raiseMoveAnother(t, e, 0, "Grizzly Bears", state.ZHand),
	}
	raiseAllToZone(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	addMana(t, e, 0, "UUUU")
	if !raiseCastOffered(e, tide) {
		t.Fatalf("Aether Tide not offered: %+v", e.Pending().Options)
	}
	raiseCast(t, e, tide)
	maxX := -1
	raiseDrive(t, e, func(d *decision.Decision) []int {
		var pick []int
		for _, o := range d.Options {
			if o.Kind == "x" {
				if o.Amount > maxX {
					maxX = o.Amount
				}
				if o.Amount == 2 {
					pick = []int{o.Index}
				}
			}
		}
		return pick
	})
	if maxX != 2 {
		t.Fatalf("largest X offered = %d, want 2 (two creature cards in hand cap the discard)", maxX)
	}
	for _, c := range creatures {
		if z := raiseZone(e, c); z != state.ZGraveyard {
			t.Fatalf("creature card %d in %s after casting X=2, want graveyard (discarded as the cost)", c, z)
		}
	}
	if o := e.G.Obj(tide); o.Zone != state.ZStack || o.X != 2 {
		t.Fatalf("Aether Tide zone=%s X=%d, want stack with X=2", o.Zone, o.X)
	}
	replayCheck(t, e, cfg)
}
