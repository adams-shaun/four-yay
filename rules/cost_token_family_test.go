package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The cost-token family (task inbox-paramcensus-cost-token-family): the five
// heads ParseCost still substituted generic mana for on the repo decks --
// announced PayLife<X>, bare Exile<N/Spec> (battlefield), Draw<N/Spec>
// (previously MIS-BUCKETED into SubCounter by the nonManaCost switch), the
// announced SubCounter<X/Kind> removal, and DamageYou<N> (the unless-pay
// shape the Sacrifice arm pays; the head itself was still unmodelled). One
// engine test per cost kind, each proving the cost is actually paid and that
// an unpayable shape is never offered (or never announced beyond its bound).

// corpusDeckEngine builds a replayable two-seat engine whose seat-0 deck
// begins with hand (left in the opening hand) followed by board (moved onto
// the battlefield with logged MoveZones), mountains filling the rest; seat 1
// is all mountains. Cards must be in the returned cfg's Decks so
// replayCheck can rebuild the game from the log alone. Returns the engine,
// the config, and hand[0]'s hand id (0 when hand is empty).
func corpusDeckEngine(t *testing.T, hand, board []*cards.Card) (*Engine, Config, state.ObjID) {
	t.Helper()
	deck := append(append([]*cards.Card{}, hand...), board...)
	cfg := Config{Seed: 31, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(deck, mountainDeck(t, 40-len(deck))...),
			mountainDeck(t, 40),
		}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	var handIDs []state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 {
			continue
		}
		for _, h := range hand {
			if o.Card == h && o.Zone == state.ZHand {
				handIDs = append(handIDs, o.ID)
			}
		}
		for _, b := range board {
			if o.Card == b {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
			}
		}
	}
	if len(hand) > 0 && len(handIDs) != 1 {
		// The genesis deal may not have dealt the protagonist: bridge it from
		// the library with a logged MoveZone (the same shape
		// newFixtureDeck's bridge uses), then re-drive.
		if len(handIDs) == 0 {
			for i := range e.G.Objs {
				o := &e.G.Objs[i]
				if o.Owner == 0 && o.Card == hand[0] && o.Zone == state.ZLibrary {
					e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZHand})
					handIDs = append(handIDs, o.ID)
					break
				}
			}
			e.pending = nil
			e.Advance()
		}
		if len(handIDs) != 1 {
			t.Fatalf("expected exactly one opening-hand copy of the hand card, got %v", handIDs)
		}
	}
	e.Advance()
	if len(hand) == 0 {
		return e, cfg, 0
	}
	return e, cfg, handIDs[0]
}

// drawsForPlayer counts the Draw events naming p in the log.
func drawsForPlayer(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// xAskOptions returns the pending X-announcement decision's options, failing
// when the pending decision is not one.
func xAskOptions(t *testing.T, e *Engine) []decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("no X announcement ask pending: %+v", d)
	}
	return d.Options
}

func maxXOf(options []decision.Option) int {
	m := -1
	for _, o := range options {
		if o.Kind == "x" && o.Amount > m {
			m = o.Amount
		}
	}
	return m
}

func chooseX(t *testing.T, e *Engine, x int) {
	t.Helper()
	for _, o := range xAskOptions(t, e) {
		if o.Kind == "x" && o.Amount == x {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("X = %d not offered", x)
}

// TestToxicDelugePaysAnnouncedLife: the announced PayLife<X> cost part is
// announced as the cast's X, bounded by the payer's life total, and the
// settle pays exactly X life beside the mana; the effect sees the same X.
// The unpayable shapes are bounded: no mana means no cast option, and the X
// ask never offers a value beyond the payer's life.
func TestToxicDelugePaysAnnouncedLife(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deluge := mustCorpusCard(t, reg, "Toxic Deluge")
	bear := card(t, "Name:Grizzly\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg, delugeID := corpusDeckEngine(t, []*cards.Card{deluge}, []*cards.Card{bear})
	// No mana: the cast is not offered at all (the mana part is unpayable).
	e.askPriority(0)
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == delugeID {
			t.Fatal("Toxic Deluge offered from an empty pool")
		}
	}
	addMana(t, e, 0, "BRR")
	// Seat the payer at 7 life through a logged LifeChange (not a bare field
	// write), so the replay rebuilds the same total.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -13})
	e.priorityRound()
	submitChoices(t, e, castOption(t, e, delugeID))
	// The X ask is bounded by the payer's life (7), not by mana (the 2 B mana
	// part is X-independent).
	if m := maxXOf(xAskOptions(t, e)); m != 7 {
		t.Fatalf("PayLife<X> options max = %d, want the payer's life 7", m)
	}
	chooseX(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != 6 {
		t.Fatalf("life after Toxic Deluge X=1 = %d, want 6 (7-1)", got)
	}
	var bearID state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).Face().Name == "Grizzly" {
			bearID = id
		}
	}
	if bearID == 0 || e.G.Obj(bearID).Zone != state.ZBattlefield || e.Power(bearID) != 1 || e.Toughness(bearID) != 1 {
		t.Fatalf("bear after -X/-X not a battlefield 1/1 (id %d, p/t %d/%d)",
			bearID, e.Power(bearID), e.Toughness(bearID))
	}
	// A bigger X than the board can survive is still a legal announcement --
	// the cost is life, not the effect's reach. X=3 sweeps the 2/2 bear to
	// the graveyard while the payer keeps 4 life.
	e2, cfg2, delugeID2 := corpusDeckEngine(t, []*cards.Card{deluge}, []*cards.Card{bear})
	addMana(t, e2, 0, "BRR")
	e2.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -13})
	e2.priorityRound()
	e2.askPriority(0)
	submitChoices(t, e2, castOption(t, e2, delugeID2))
	chooseX(t, e2, 3)
	passUntilStackEmpty(t, e2, 20)
	if got := e2.G.Players[0].Life; got != 4 {
		t.Fatalf("life after Toxic Deluge X=3 = %d, want 4", got)
	}
	for _, id := range e2.G.Zone(state.ZGraveyard, 0) {
		if e2.G.Obj(id).Face().Name == "Grizzly" {
			bearID = 0 // found: the sweep took it
		}
	}
	if bearID != 0 {
		t.Fatalf("the 2/2 bear survived a -3/-3 sweep: %+v", e2.G.Obj(bearID))
	}
	replayCheck(t, e2, cfg2)
	replayCheck(t, e, cfg)
}

// TestBareExileCostExilesTheSource: Relic of Progenitus's "{1}, Exile Relic
// of Progenitus: Exile all graveyards" -- the bare Exile<1/CARDNAME> token
// exiles the source from the battlefield as the payment (the singleton
// self-reference asks nothing), the ability resolves, and the mana plus the
// exile requirement gate the offer.
func TestBareExileCostExilesTheSource(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	relicCard := mustCorpusCard(t, reg, "Relic of Progenitus")
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{relicCard})
	relic := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).Face().Name == "Relic of Progenitus" {
			relic = id
		}
	}
	if relic == 0 {
		t.Fatal("Relic not on the battlefield")
	}
	idx := -1
	for i, sa := range relicCard.Faces[0].Abilities {
		if sa.API == "ChangeZoneAll" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("Relic of Progenitus has no ChangeZoneAll ability")
	}
	// A card from each library into that seat's graveyard for the sweep to
	// take (deck cards, moved with logged events, so the replay rebuilds).
	for p := state.PlayerID(0); p < 2; p++ {
		lib := e.G.Zone(state.ZLibrary, p)
		e.emit(events.Event{Kind: events.MoveZone, Obj: lib[len(lib)-1], From: state.ZLibrary, To: state.ZGraveyard})
	}
	// No mana: the ability is not offered (the exile part alone does not pay
	// the {1}).
	e.askPriority(0)
	if _, ok := findAbilityOption(e, relic, idx); ok {
		t.Fatal("Relic's exile-sweep ability offered from an empty pool")
	}
	addMana(t, e, 0, "R")
	e.priorityRound()
	opt, ok := findAbilityOption(e, relic, idx)
	if !ok {
		t.Fatal("Relic's exile-sweep ability not offered with the {1} in the pool")
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(relic); o.Zone != state.ZExile {
		t.Fatalf("Relic after its own exile cost: zone %s, want exile", o.Zone)
	}
	for p := state.PlayerID(0); p < 2; p++ {
		if n := len(e.G.Zone(state.ZGraveyard, p)); n != 0 {
			t.Fatalf("seat %d graveyard has %d cards after the sweep, want 0", p, n)
		}
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("seat 0's hand = %d, want %d after the sweep's draw (genesis deal aside)", got, handBefore+1)
	}
	replayCheck(t, e, cfg)
}

// TestDrawCostDrawsThePayer: a Cost$ Draw<1/You> component is a Draw part,
// not the SubCounter removal the old nonManaCost default bucketed it into;
// the activation pays it by drawing the payer one card, then the body
// resolves. A spec naming a trigger-only role has no binding in the cast
// flow and the ability is never offered. Riddlesmith's own Draw<1/You> lives
// on a trigger Execute$ body (whose costs this build's executor does not
// charge -- see the task report), so the payment path is pinned on a fixture
// with the same printed token.
func TestDrawCostDrawsThePayer(t *testing.T) {
	t.Parallel()
	// The parse half: the Draw bucket, on the real Riddlesmith script shape.
	parsed := ParseCost("Draw<1/You>")
	if len(parsed.Unknown) != 0 || len(parsed.Draw) != 1 || parsed.Draw[0].N != 1 || parsed.Draw[0].Spec != "You" {
		t.Fatalf("ParseCost(\"Draw<1/You>\") = %+v, want one Draw part and no Unknown", parsed)
	}
	if len(parsed.SubCounter) != 0 {
		t.Fatalf("Draw<1/You> still mis-bucketed into SubCounter: %+v", parsed.SubCounter)
	}
	src := "Name:LootEngine\nTypes:Artifact Creature\nPT:1/1\n" +
		"A:AB$ GainLife | Cost$ Draw<1/You> | Defined$ You | LifeAmount$ 2\nOracle:x\n"
	engine := card(t, src)
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{engine})
	id := e.G.Zone(state.ZBattlefield, 0)[0]
	e.askPriority(0)
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatal("the Draw-cost ability not offered")
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	lifeBefore := e.G.Players[0].Life
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand after the Draw<1/You> payment = %d, want %d", got, handBefore+1)
	}
	if got := e.G.Players[0].Life; got != lifeBefore+2 {
		t.Fatalf("life after the activation = %d, want %d (the body resolved)", got, lifeBefore+2)
	}
	replayCheck(t, e, cfg)

	// A Draw spec with no cast-flow binding is unpayable: never offered.
	unbindable := card(t, "Name:LootEngine2\nTypes:Artifact Creature\nPT:1/1\n"+
		"A:AB$ GainLife | Cost$ Draw<1/Player.TriggeredTarget> | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e2, _, _ := corpusDeckEngine(t, nil, []*cards.Card{unbindable})
	e2.askPriority(0)
	if _, ok := findAbilityOption(e2, e2.G.Zone(state.ZBattlefield, 0)[0], 0); ok {
		t.Fatal("a trigger-only Draw spec was offered as a cast-flow payment")
	}
}

// TestAnnouncedSubCounterCostRemovesCounters: Chandra, Awakened Inferno's
// ultimate (Cost$ SubCounter<X/LOYALTY>, NumDmg$ X) announces X bounded by
// the walker's loyalty, removes exactly X loyalty counters as the payment,
// and deals X to the target. Before the announced form was modelled the cost
// degraded to one generic, no X was announced, and the ultimate dealt zero.
func TestAnnouncedSubCounterCostRemovesCounters(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	chandraCard := mustCorpusCard(t, reg, "Chandra, Awakened Inferno")
	ult := -1
	for i, sa := range chandraCard.Faces[0].Abilities {
		if sa.Params["Ultimate"] == "True" {
			ult = i
		}
	}
	if ult < 0 {
		t.Fatal("Chandra has no ultimate ability")
	}
	bearCard := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, chandra := walkerBoard(t, reg, "Chandra, Awakened Inferno", bearCard)
	e.Advance()
	loyalty := e.G.Obj(chandra).Counter("LOYALTY")
	if loyalty != 6 {
		t.Fatalf("Chandra entered with %d loyalty, want 6", loyalty)
	}
	bear := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).Face().Name == "Grizzly Bears" {
			bear = id
		}
	}
	if bear == 0 {
		t.Fatal("the bear is not on the battlefield")
	}
	opt := abilityOption(t, e, chandra, ult)
	submitChoices(t, e, opt.Index)
	if m := maxXOf(xAskOptions(t, e)); m != int(loyalty) {
		t.Fatalf("SubCounter<X/LOYALTY> options max = %d, want the walker's loyalty %d", m, loyalty)
	}
	chooseX(t, e, 2)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the ultimate's target ask, got %+v", d)
	}
	targetIdx := -1
	for _, o := range d.Options {
		if o.Obj == bear && o.Kind != "player" {
			targetIdx = o.Index
		}
	}
	if targetIdx < 0 {
		t.Fatalf("the bear not offered as the ultimate's target: %+v", d.Options)
	}
	submitChoices(t, e, targetIdx)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(chandra).Counter("LOYALTY"); got != loyalty-2 {
		t.Fatalf("loyalty after the ultimate = %d, want %d", got, loyalty-2)
	}
	dealt := int32(0)
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == bear {
			dealt += ev.Amount
		}
	}
	if dealt != 2 {
		t.Fatalf("the ultimate dealt %d damage to the bear, want 2", dealt)
	}
	replayCheck(t, e, cfg)
}

// TestDamageYouCostModelledAndPaid: DamageYou<4> parses as a modelled head
// (no Unknown entry, no generic substitution), the strict unless parser still
// declines it for every API the Sacrifice arm does not intercept, and the
// real card's payment is the damage the accepting opponent takes -- pinned
// end to end on the corpus card (the decline arm is the existing
// sacrifice_unless_pay_test.go's).
func TestDamageYouCostModelledAndPaid(t *testing.T) {
	t.Parallel()
	parsed := ParseCost("DamageYou<4>")
	if len(parsed.Unknown) != 0 || len(parsed.DamageYou) != 1 || parsed.DamageYou[0].N != 4 {
		t.Fatalf("ParseCost(\"DamageYou<4>\") = %+v, want one DamageYou part and no Unknown", parsed)
	}
	if _, ok := ParseUnlessCost("DamageYou<4>"); ok {
		t.Fatal("ParseUnlessCost accepted DamageYou<N> -- the strict gate must keep declining it outside the Sacrifice arm")
	}
	reg := testutil.CorpusRegistry(t)
	e, devil := devilToHand(t, reg, "Vexing Devil")
	e.G.Players[0].Pool = state.Mana{state.MR: 1}
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, devil))
	ask := drainUntilKModes(t, e, 60)
	if ask == nil {
		t.Fatal("no DamageYou offer posed for the resolving Vexing Devil")
	}
	lifeBefore := e.G.Players[1].Life
	submitChoices(t, e, ask.Options[0].Index) // accept: take the 4 damage
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[1].Life; got != lifeBefore-4 {
		t.Fatalf("the accepting opponent's life = %d, want %d", got, lifeBefore-4)
	}
	if o := e.G.Obj(devil); o.Zone != state.ZGraveyard {
		t.Fatalf("the devil after the accepted DamageYou payment: zone %s, want graveyard (sacrificed)", o.Zone)
	}
}

// The r2-review fixes: the SVar-priced announced PayLife<X> faces (the free
// announcement the r2 build gave every PayLife<X> face was fail-OPEN for the
// four faces whose SVar:X fixes the value), and the plain Cost$ DamageYou<N>
// settle branch, which the r2 round had covered only through Vexing Devil's
// UnlessCost$ path.

// TestSVarFixedPayLifeXPaysItsBody: Murderous Betrayal's
// "SVar:X:Count$YourLifeTotal/HalfUp" FIXES the announced PayLife<X> part's
// value -- the payer announces nothing (no X ask is ever posed), the settle
// pays exactly half their life rounded up, and the body resolves. The
// announcement menu the r2 build posed (an arbitrary 0..life X) is gone.
func TestSVarFixedPayLifeXPaysItsBody(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	betrayalCard := mustCorpusCard(t, reg, "Murderous Betrayal")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{betrayalCard, bear})
	betrayal, bearID := state.ObjID(0), state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		switch e.G.Obj(id).Face().Name {
		case "Murderous Betrayal":
			betrayal = id
		case "Grizzly Bears":
			bearID = id
		}
	}
	if betrayal == 0 || bearID == 0 {
		t.Fatalf("board not set up: betrayal %d bear %d", betrayal, bearID)
	}
	addMana(t, e, 0, "BB")
	// Odd life so HalfUp is distinguishable from HalfDown: 17 -> 9.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -3})
	e.priorityRound()
	opt, ok := findAbilityOption(e, betrayal, 0)
	if !ok {
		t.Fatal("Murderous Betrayal's destroy ability not offered with BB in the pool")
	}
	submitChoices(t, e, opt.Index)
	// NO announcement ask: the SVar fixed the value, so the next decision is
	// the ability's target ask -- a KChoose "x" menu here is the fail-open
	// announcement the fix removed.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the destroy ability's target ask directly (no X ask), got %+v", d)
	}
	targetIdx := -1
	for _, o := range d.Options {
		if o.Obj == bearID && o.Kind != "player" {
			targetIdx = o.Index
		}
	}
	if targetIdx < 0 {
		t.Fatalf("the bear not offered as the destroy target: %+v", d.Options)
	}
	submitChoices(t, e, targetIdx)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bearID); o.Zone != state.ZGraveyard {
		t.Fatalf("the targeted bear after the destroy: zone %s, want graveyard", o.Zone)
	}
	// The fixed life settles at payCast (CR 601.2h, after 601.2c's target
	// choice): 17 - 9 (HalfUp of 17).
	if got := e.G.Players[0].Life; got != 8 {
		t.Fatalf("life after the fixed PayLife<X> settle = %d, want 8 (HalfUp of 17)", got)
	}
	replayCheck(t, e, cfg)
}

// TestFixedLifeXSVarCountsCounters: Tornado's
// "SVar:X:Count$CardCounters.VELOCITY/Times.3" shape on a fixture -- the
// fixed value counts the source's own counters and scales, the life the
// settle pays tracks it exactly, and a fixed value the payer cannot afford
// (life below the fixed price) withholds the ability instead of offering a
// cheaper announcement.
func TestFixedLifeXSVarCountsCounters(t *testing.T) {
	t.Parallel()
	src := "Name:VelocityEngine\nTypes:Enchantment\n" +
		"A:AB$ GainLife | Cost$ 2 G PayLife<X> | Defined$ You | LifeAmount$ 1\n" +
		"SVar:X:Count$CardCounters.CHARGE/Times.3\nOracle:x\n"
	engine := card(t, src)
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{engine})
	id := e.G.Zone(state.ZBattlefield, 0)[0]
	addMana(t, e, 0, "GGG")
	e.priorityRound()
	// Zero counters: the fixed value is 0 -- the ability activates for its
	// mana alone and pays no life, with no announcement ask.
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatal("the fixed-PayLife ability not offered at a zero fixed value")
	}
	life := e.G.Players[0].Life
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "x" {
		t.Fatalf("an X announcement menu was posed for a fixed PayLife<X>: %+v", d.Options)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != life+1 {
		t.Fatalf("life after the zero-fixed activation = %d, want %d (nothing paid, the body's +1)", got, life+1)
	}
	// One CHARGE counter: the fixed value is 3, paid exactly. The first
	// activation spent the pool; refill it for the second offer.
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "CHARGE", Amount: 1})
	addMana(t, e, 0, "GGG")
	e.priorityRound()
	opt, ok = findAbilityOption(e, id, 0)
	if !ok {
		t.Fatal("the fixed-PayLife ability not offered at a fixed value of 3")
	}
	life = e.G.Players[0].Life
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != life-3+1 {
		t.Fatalf("life after the fixed-3 activation = %d, want %d (-3 paid, +1 body)", got, life-3+1)
	}
	replayCheck(t, e, cfg)

	// Unpayable fixed value: life below the price withholds the ability.
	e2, _, _ := corpusDeckEngine(t, nil, []*cards.Card{engine})
	id2 := e2.G.Zone(state.ZBattlefield, 0)[0]
	e2.emit(events.Event{Kind: events.CounterChange, Obj: id2, Counter: "CHARGE", Amount: 3})
	e2.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -19})
	e2.priorityRound()
	addMana(t, e2, 0, "GGG")
	e2.priorityRound()
	if _, ok := findAbilityOption(e2, id2, 0); ok {
		t.Fatal("the fixed-PayLife ability offered with life below the fixed price")
	}
}

// TestFixedLifeXUnresolvableWithheld: War Room's
// "SVar:X:Count$ColorsColorIdentity" used to be a body this evaluator could
// not resolve, so the ability was WITHHELD. The head is now resolvable
// (war_room_commander_test.go pins the Commander-format play), and THIS
// Constructed game still withholds the ability -- but for the
// ActivationGameTypes$ format gate now, not for the count: a Constructed
// game has no commanders, an empty identity counts 0, and without the
// format gate the ability would be a free {3},{T} draw here. The land's
// mana ability is unaffected.
func TestFixedLifeXUnresolvableWithheld(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	warRoomCard := mustCorpusCard(t, reg, "War Room")
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{warRoomCard})
	room := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).Face().Name == "War Room" {
			room = id
		}
	}
	if room == 0 {
		t.Fatal("War Room not on the battlefield")
	}
	addMana(t, e, 0, "CCC")
	e.priorityRound()
	if _, ok := findAbilityOption(e, room, 1); ok {
		t.Fatal("War Room's PayLife<X> draw ability offered in a Constructed game (format gate missing?)")
	}
	replayCheck(t, e, cfg)
}

// TestFreePayLifeXSharedAnnouncementStands: the fix must not break the
// Count$xPaid faces -- Krumar Initiate's "Cost$ X B T PayLife<X>" announces
// ONE X that folds both the printed {X} into generic and the life part, so
// the X ask is still posed and the settle pays exactly the chosen value of
// both.
func TestFreePayLifeXSharedAnnouncementStands(t *testing.T) {
	t.Parallel()
	src := "Name:LifeLedger\nTypes:Artifact Creature\nPT:1/1\n" +
		"A:AB$ GainLife | Cost$ X B PayLife<X> | Defined$ You | LifeAmount$ 2\n" +
		"SVar:X:Count$xPaid\nOracle:x\n"
	engine := card(t, src)
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{engine})
	id := e.G.Zone(state.ZBattlefield, 0)[0]
	addMana(t, e, 0, "BBB")
	e.priorityRound()
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatal("the shared-announcement ability not offered")
	}
	submitChoices(t, e, opt.Index)
	// The X ask IS posed for a Count$xPaid body: the payer chooses. The mana
	// bound: pool BBB folds at most X=2 (2 generic + 1 B = 3 mana).
	if m := maxXOf(xAskOptions(t, e)); m != 2 {
		t.Fatalf("shared PayLife<X> options max = %d, want the mana bound 2 (pool BBB)", m)
	}
	chooseX(t, e, 2)
	passUntilStackEmpty(t, e, 20)
	// Settle: 2 generic + 1 B from BBB, plus 2 life, then the body's +2.
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("life after the shared announcement = %d, want 20 (-2 paid, +2 body)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDamageYouCostPaidFromAbilityCost: the plain Cost$ DamageYou<N> settle
// branch (the r2 round's engine half reached it only through Vexing Devil's
// UnlessCost$ arm). A lifelink source's DamageYou<4> cost deals the payer 4
// damage FROM the source and the lifelink gain reaches the source's
// controller; with the source already exiled by an earlier Exile<CARDNAME>
// part of the same cost, the controller fallback keeps the payment honest
// instead of panicking on the gone source.
func TestDamageYouCostPaidFromAbilityCost(t *testing.T) {
	t.Parallel()
	src := "Name:PainEngine\nTypes:Artifact Creature\nPT:1/1\nK:Lifelink\n" +
		"A:AB$ GainLife | Cost$ DamageYou<4> | Defined$ You | LifeAmount$ 2\nOracle:x\n"
	engine := card(t, src)
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{engine})
	id := e.G.Zone(state.ZBattlefield, 0)[0]
	e.askPriority(0)
	// No mana component: the ability is offered from an empty pool.
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatal("the DamageYou-cost ability not offered from an empty pool")
	}
	dmgBefore, lifeBefore := int32(0), e.G.Players[0].Life
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Player == 0 {
			dmgBefore += ev.Amount
		}
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	dmg := int32(0)
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Player == 0 {
			dmg += ev.Amount
		}
	}
	if dmg != dmgBefore+4 {
		t.Fatalf("Damage events on the payer = %d, want %d (the DamageYou<4> payment)", dmg, dmgBefore+4)
	}
	// Payer -4 (damage) +4 (the lifelink gain, controller == payer) +2 (body).
	if got := e.G.Players[0].Life; got != lifeBefore+2 {
		t.Fatalf("life after the DamageYou activation = %d, want %d (-4 damage, +4 lifelink, +2 body)", got, lifeBefore+2)
	}
	replayCheck(t, e, cfg)

	// The source-left-battlefield fallback: an Exile<1/CARDNAME> part settles
	// BEFORE the DamageYou part, so the source is in exile when the damage
	// lands -- the controller fallback routes the lifelink gain to the payer
	// and the payment completes.
	src2 := "Name:PainPortal\nTypes:Artifact\nK:Lifelink\n" +
		"A:AB$ GainLife | Cost$ Exile<1/CARDNAME> DamageYou<4> | Defined$ You | LifeAmount$ 2\nOracle:x\n"
	portal := card(t, src2)
	e2, cfg2, _ := corpusDeckEngine(t, nil, []*cards.Card{portal})
	id2 := e2.G.Zone(state.ZBattlefield, 0)[0]
	e2.askPriority(0)
	opt2, ok := findAbilityOption(e2, id2, 0)
	if !ok {
		t.Fatal("the Exile+DamageYou ability not offered")
	}
	lifeBefore = e2.G.Players[0].Life
	submitChoices(t, e2, opt2.Index)
	passUntilStackEmpty(t, e2, 20)
	if o := e2.G.Obj(id2); o.Zone != state.ZExile {
		t.Fatalf("PainPortal after its own exile cost: zone %s, want exile", o.Zone)
	}
	dmg = int32(0)
	for _, ev := range e2.L.Events {
		if ev.Kind == events.Damage && ev.Player == 0 {
			dmg += ev.Amount
		}
	}
	if dmg != 4 {
		t.Fatalf("Damage events on the payer after the source left = %d, want 4", dmg)
	}
	if got := e2.G.Players[0].Life; got != lifeBefore+2 {
		t.Fatalf("life after the Exile+DamageYou activation = %d, want %d (-4 damage, +4 fallback lifelink, +2 body)", got, lifeBefore+2)
	}
	replayCheck(t, e2, cfg2)
}
