package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The round-2 review's damage findings, executable: DamageSource$ resolution
// (the named object is the damaging one for the lifelink rider, for emit-side
// protection and for DamageDone trigger matching), the DamageAll ValidPlayers$
// arm, walker damage as a real Damage event with its CR 306.8 conversion
// folded into events.Apply, and the destruction batch's pre-batch lifelink
// LKI. Real corpus scripts wherever the corpus reaches the shape; inline
// fixtures only where no corpus card reaches it (the creature-walker, and the
// two synthetic provenance probes), per the licensing rule.

// dsBoardWith builds a 2-seat game whose seat 0 deck holds every named card
// (mountains pad it), parks every named card's owner-0 copy on the
// battlefield through a logged MoveZone, and leaves seat 0 at turn 2, main
// phase, with no summoning sickness and a pending decision to clear. Names
// map to their battlefield ids. seat1 (may be nil) is seeded into seat 1's
// deck before its own mountain padding.
func dsBoardWith(t *testing.T, reg *cards.Registry, seat1 *cards.Card,
	names ...string) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	deck := make([]*cards.Card, 0, len(names))
	for _, n := range names {
		deck = append(deck, mustCorpusCard(t, reg, n))
	}
	cfg := Config{Seed: 47, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(deck, mountainDeck(t, 40-len(deck))...),
			append([]*cards.Card{seat1}, mountainDeck(t, 39)...),
		}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	ids := make(map[string]state.ObjID, len(names))
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 {
			continue
		}
		n := o.Face().Name
		if !want[n] {
			continue
		}
		if o.Zone != state.ZBattlefield {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
		}
		ids[n] = o.ID
	}
	for n := range want {
		if _, ok := ids[n]; !ok {
			t.Fatalf("no %q copy found in seat 0's genesis", n)
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.pending = nil
	// Fixture setup noise: parking a card with an ETB/enters trigger queues
	// it during the genesis moves (Scourge of Valkas fires on its own
	// entrance, Card.Self); the tests here pin a REAL later event, so the
	// stale queue is cleared, never resolved.
	e.pendingTriggers = nil
	return e, cfg, ids
}

// dsBoard is dsBoardWith with no extra seat-1 card.
func dsBoard(t *testing.T, reg *cards.Registry, names ...string) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	return dsBoardWith(t, reg, nil, names...)
}

// drainStack answers the pending decision until the stack is empty: priority
// is passed, a KTriggerOptional offer is answered "yes", anything else is a
// test failure.
func drainStack(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Kind == decision.KPriority && len(e.G.Stack) == 0 {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			passFirst(t, e)
		case decision.KTriggerOptional:
			submitChoices(t, e, 0) // yes
		default:
			t.Fatalf("unexpected decision %q while draining: %+v", d.Kind, d.Options)
		}
	}
	t.Fatal("drainStack never emptied the stack")
}

// moveToHand moves one object to seat 0's hand through a logged MoveZone
// (the id-keyed form of the shared cardToHand, which wants a *cards.Card).
func moveToHand(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Zone == state.ZHand {
		return
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZHand})
}

// TestScourgeOfValkasDealsDamageFromTheEnteringDragon pins DamageSource$
// TriggeredCard on the real card: another Dragon entering fires Scourge's
// trigger, and the damage comes from THAT dragon -- so its lifelink pays the
// rider. Before the fix the rider read the trigger's own source (Scourge,
// which has no lifelink) and nobody gained life.
func TestScourgeOfValkasDealsDamageFromTheEnteringDragon(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := dsBoard(t, reg, "Scourge of Valkas", "Adult Gold Dragon")
	dragon := ids["Adult Gold Dragon"]
	if e.G.Obj(dragon).Zone == state.ZBattlefield {
		// The dragon must still be off the battlefield so ENTERING it is what
		// fires the trigger; move it back to hand through a logged MoveZone.
		e.emit(events.Event{Kind: events.MoveZone, Obj: dragon, From: state.ZBattlefield, To: state.ZHand})
	}
	before := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.MoveZone, Obj: dragon, From: e.G.Obj(dragon).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Scourge trigger did not pose a target decision: %+v", d)
	}
	idx := indexOfPlayerOption(d, 1)
	if idx < 0 {
		t.Fatalf("seat 1 not offered as the trigger's target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// The opponent gets the first response after the cast; pass their
	// priority so the trigger's resolution is fully drained.
	passUntilSeat0Priority(t, e, 8)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("seat 1 life = %d, want %d (2 Dragons you control)", got, before-2)
	}
	if n := countLifeChanges(t, e, 0, 2); n != 1 {
		t.Fatalf("logged %d LifeChange(0, +2) events, want 1: the damage must come from the LIFELINK dragon", n)
	}
	replayCheck(t, e, cfg)
}

// TestKikusShadowDamageSourceTargetedGainsLife pins DamageSource$ Targeted
// with the Targeted$CardPower count head on the real card: the TARGETED
// creature (Adult Gold Dragon, 4/3 with lifelink) deals 4 to itself, and its
// own lifelink pays its controller 4. Before the fix both halves were dead:
// X evaluated to 0 (no damage at all) and the rider read the spell.
// TestDepartedTriggeredDamageSourceCreditsLastController proves the LKI is
// keyed by the NAMED DamageSource$, not only the resolving trigger's source.
// The real Scourge trigger names its entering Dragon as TriggeredCard; after
// its target is chosen, that stolen lifelink Dragon leaves before resolution.
func TestDepartedTriggeredDamageSourceCreditsLastController(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := dsBoard(t, reg, "Scourge of Valkas", "Adult Gold Dragon")
	dragon := ids["Adult Gold Dragon"]
	e.emit(events.Event{Kind: events.MoveZone, Obj: dragon, From: state.ZBattlefield, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: dragon, From: state.ZHand, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Scourge target decision = %+v", d)
	}
	submitChoices(t, e, indexOfPlayerOption(d, 1))
	// The entering Dragon is stolen then removed after the trigger's target
	// selection. Its object now reports owner 0, so only the pre-departure
	// named-source controller/Lifelink snapshot can credit seat 1.
	e.emit(events.Event{Kind: events.ControlChange, Obj: dragon, Player: 1})
	e.emit(events.Event{Kind: events.MoveZone, Obj: dragon, From: state.ZBattlefield, To: state.ZGraveyard})
	passUntilStackEmpty(t, e, 20)
	// Stealing the Dragon means Scourge now controls only itself at resolution,
	// so the real script deals one. The +1 must nevertheless go to its LAST
	// controller, not its owner after the graveyard move reset it.
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("seat 1 life = %d, want 20 (1 damage and +1 departed-source lifelink)", got)
	}
	if n := countLifeChanges(t, e, 1, 1); n != 1 {
		t.Fatalf("logged %d LifeChange(1, +1), want 1", n)
	}
	replayCheck(t, e, cfg)
}

func TestKikusShadowDamageSourceTargetedGainsLife(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := dsBoard(t, reg, "Kiku's Shadow", "Adult Gold Dragon")
	dragon := ids["Adult Gold Dragon"]
	moveToHand(t, e, ids["Kiku's Shadow"])
	addMana(t, e, 0, "BB")
	castFirst(t, e, "cast")
	targetObject(t, e, dragon)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(dragon).Zone; z != state.ZGraveyard {
		t.Fatalf("4/3 dragon survived its own 4 damage in %s", z)
	}
	if n := countLifeChanges(t, e, 0, 4); n != 1 {
		t.Fatalf("logged %d LifeChange(0, +4) events, want 1: the dragon dealt the damage, its lifelink pays", n)
	}
	sawDamage := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == dragon && ev.Amount == 4 {
			sawDamage = true
		}
	}
	if !sawDamage {
		t.Fatal("no 4-amount Damage event against the dragon")
	}
	replayCheck(t, e, cfg)
}

// TestPestilenceAndEarthquakeHitPlayers pin the DamageAll ValidPlayers$ arm
// on real cards: Pestilence's activation damages each creature and each
// PLAYER, Earthquake's cast damages each player (and each non-flying
// creature). Before the fix the player half of both sweeps silently did
// nothing.
func TestPestilenceAndEarthquakeHitPlayers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	e, cfg, ids := dsBoard(t, reg, "Pestilence", "Grizzly Bears")
	pest := ids["Pestilence"]
	bear := ids["Grizzly Bears"]
	addMana(t, e, 0, "B")
	opt := abilityOption(t, e, pest, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != 19 {
		t.Fatalf("Pestilence: seat 0 life = %d, want 19", got)
	}
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("Pestilence: seat 1 life = %d, want 19", got)
	}
	if got := e.G.Obj(bear).Damage; got != 1 {
		t.Fatalf("Pestilence: bear marked damage = %d, want 1", got)
	}
	replayCheck(t, e, cfg)

	e2, cfg2, _ := dsBoard(t, reg, "Earthquake")
	cardToHand(t, e2, ids2Card(t, e2, "Earthquake"))
	addMana(t, e2, 0, "RRR")
	castFirst(t, e2, "cast")
	d := e2.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Earthquake X ask: %+v", d)
	}
	x3 := -1
	for _, o := range d.Options {
		if o.Label == "X = 2" {
			x3 = o.Index
		}
	}
	if x3 < 0 {
		t.Fatalf("no X = 2 option: %+v", d.Options)
	}
	submitChoices(t, e2, x3)
	passUntilStackEmpty(t, e2, 20)
	if got := e2.G.Players[0].Life; got != 18 {
		t.Fatalf("Earthquake: seat 0 life = %d, want 18", got)
	}
	if got := e2.G.Players[1].Life; got != 18 {
		t.Fatalf("Earthquake: seat 1 life = %d, want 18", got)
	}
	replayCheck(t, e2, cfg2)
}

// ids2Card finds a named card anywhere in seat 0 (hand/library) for a
// hand-cast; a card dsBoard already parked on the battlefield is moved back.
func ids2Card(t *testing.T, e *Engine, name string) *cards.Card {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Face() != nil && o.Face().Name == name {
			return o.Card
		}
	}
	t.Fatalf("no %q in seat 0", name)
	return nil
}

// TestFlamebladeAngelTriggersOnBoltToWalker pins the walker-damage trigger
// visibility: Lightning Bolt at seat 0's Jace (seat 1 casting) is a real
// Damage event against the walker object, so Flameblade Angel's DamageDone
// trigger (ValidTarget$ You,Permanent.YouCtrl) fires, its optional ask is
// answered, and the angel deals 1 to the bolt's controller. Before the fix
// walker damage emitted only a CounterChange and no trigger saw it.
func TestFlamebladeAngelTriggersOnBoltToWalker(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bolt := mustCorpusCard(t, reg, "Lightning Bolt")
	e, cfg, ids := dsBoardWith(t, reg, bolt, "Flameblade Angel", "Jace, the Mind Sculptor")
	if e.G.Obj(ids["Jace, the Mind Sculptor"]).Counter("LOYALTY") != 3 {
		t.Fatal("Jace did not enter at 3 loyalty")
	}
	cardToHandSeat(t, e, 1, bolt)
	// addMana re-asks priority after the pool change (the tested idiom);
	// seat 0 then passes and seat 1's fresh decision sees the funded pool.
	addMana(t, e, 1, "R")
	passFirst(t, e)
	d := e.Pending()
	if d == nil {
		t.Fatal("seat 1 got no decision after the pass")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" {
			submitChoices(t, e, o.Index)
			targetObject(t, e, ids["Jace, the Mind Sculptor"])
			break
		}
	}
	drainStack(t, e, 20)
	if z := e.G.Obj(ids["Jace, the Mind Sculptor"]).Zone; z != state.ZGraveyard {
		t.Fatalf("3-loyalty Jace survived the bolt in %s", z)
	}
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("seat 1 life = %d, want 19: the angel's trigger must answer to the bolt's controller", got)
	}
	replayCheck(t, e, cfg)
}

// cardToHandSeat is cardToHand for an arbitrary seat (the shared helper is
// hard-wired to owner 0).
func cardToHandSeat(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == p && o.Card == c && o.Zone == state.ZHand {
			return
		}
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == p && o.Card == c {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZHand})
			return
		}
	}
	t.Fatalf("no seat-%d copy", p)
}

// passFirst submits the pending priority decision's pass option.
func passFirst(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("want a priority decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "pass" {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no pass option: %+v", d.Options)
}

// passUntilSeat0Priority passes every pending priority decision that is not
// seat 0's (the opponent gets the first response after a cast/activation), so
// the protagonist's own follow-up cast reads a fresh seat-0 decision.
func passUntilSeat0Priority(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			return
		}
		if d.Player == 0 {
			return
		}
		passFirst(t, e)
	}
	t.Fatal("priority never came back to seat 0")
}

// TestCreaturePlaneswalkerTakesMarkedDamageAndLoyaltyLoss pins CR 120.3e's
// non-exclusive exchange on an inline fixture (no corpus card reaches the
// shape: a creature that is also a planeswalker enters via its face): two
// points of spell damage are BOTH two marked-damage points (the 704.5g
// lethal bookkeeping sees it) AND two lost loyalty counters, on one Damage
// event, on a 2/4 body that survives the hit.
func TestCreaturePlaneswalkerTakesMarkedDamageAndLoyaltyLoss(t *testing.T) {
	t.Parallel()
	walker := card(t, "Name:Battle Scholar\nManaCost:2 W W\nTypes:Creature Planeswalker\n"+
		"Loyalty:4\nPT:2/4\nOracle:synthetic creature-walker probe\n")
	spark := card(t, "Name:Spark\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n")
	e := handEngine(t, walker, spark)
	e.G.Players[0].Pool[state.MR] = 1
	walkerID := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: walkerID, From: state.ZHand, To: state.ZBattlefield})
	if got := e.G.Obj(walkerID).Counter("LOYALTY"); got != 4 {
		t.Fatalf("creature-walker entered with %d loyalty, want 4", got)
	}
	e.askPriority(0)
	castFirst(t, e, "cast")
	targetObject(t, e, walkerID)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(walkerID)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("walker left the battlefield: %s", o.Zone)
	}
	if o.Damage != 2 {
		t.Fatalf("creature-walker marked damage = %d, want 2 (CR 120.3e)", o.Damage)
	}
	if got := o.Counter("LOYALTY"); got != 2 {
		t.Fatalf("creature-walker loyalty = %d, want 2 (CR 306.8)", got)
	}
	if !o.WasDealtDamageThisTurn {
		t.Fatal("the creature-walker's Damage event did not record WasDealtDamageThisTurn")
	}
	sawDamage := false
	dmgIdx := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == walkerID && ev.Amount == 2 {
			sawDamage = true
			dmgIdx = i
		}
	}
	// The conversion itself must fold into the Damage event's events.Apply
	// (no CounterChange of its own). The walker's ENTRY loyalty is a real
	// CounterChange since task addcounter1/2 -- every entry counter rides one
	// so the CR 614 replacement class sees it -- but that placement precedes
	// the damage entirely, so the scan starts at the Damage event: a
	// CounterChange at or after it could only be the conversion leaking out
	// of the fold.
	sawCounter := false
	for i, ev := range e.L.Events {
		if i > dmgIdx && ev.Kind == events.CounterChange && ev.Obj == walkerID && ev.Counter == "LOYALTY" {
			sawCounter = true
		}
	}
	if !sawDamage || sawCounter {
		t.Fatalf("walker exchange: Damage event %v (want true), CounterChange event %v (want false: the conversion folds into events.Apply)", sawDamage, sawCounter)
	}
}

// TestDamageSourceIsTheSourceProtectionAndTriggersRead pins BOTH provenance
// consumers on one inline probe (no corpus DealDamage line reaches a shape
// where the named source's provenance is independently observable):
//
//   - Leg 1 (protection, CR 702.16d): a colourless artifact enabler deals 2
//     damage with DamageSource$ Targeted at a blue creature that has
//     protection from blue. The NAMED source is the blue creature itself, so
//     emit-side prevention blocks the hit entirely (pre-fix the source was
//     the colourless enabler and the damage landed).
//   - Leg 2 (triggers, CR 603.10 DamageDone): the same enabler hits a Dragon;
//     a watcher's "whenever a Dragon deals damage" trigger (ValidSource$
//     Dragon) fires off the NAMED source, never off the enabler (pre-fix the
//     trigger read the stack top, the ability wrapper, and stayed silent).
func TestDamageSourceIsTheSourceProtectionAndTriggersRead(t *testing.T) {
	t.Parallel()
	enabler := "Name:Awkward Robot\nManaCost:3\nTypes:Artifact Creature Golem\nPT:0/4\n" +
		"A:AB$ DealDamage | Cost$ T | ValidTgts$ Creature | NumDmg$ 2 | DamageSource$ Targeted | " +
		"SpellDescription$ The target deals damage to itself.\nOracle:x\n"
	protected := "Name:Blue Knight\nManaCost:1 U\nTypes:Creature Knight\nPT:2/2\nK:Protection from blue\nOracle:x\n"
	dragon := "Name:Probe Dragon\nManaCost:2 R\nTypes:Creature Dragon\nPT:2/2\nOracle:x\n"
	watcher := "Name:Dragon Watcher\nManaCost:1 W\nTypes:Creature Cleric\nPT:1/1\n" +
		"T:Mode$ DamageDone | ValidSource$ Dragon | TriggerZones$ Battlefield | Execute$ TrigGain | " +
		"TriggerDescription$ Whenever a Dragon deals damage, gain 1 life.\n" +
		"SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 83, enabler, protected, dragon, watcher)
	robot := putCreature(t, e, 0, enabler)
	knight := putCreature(t, e, 0, protected)
	drake := putCreature(t, e, 0, dragon)
	putCreature(t, e, 0, watcher)
	// Everything the four puts parked is summoning sick; a fresh turn for
	// seat 0 clears it (the same ordering dsBoard uses: moves first, then
	// the TurnChange that clears the sickness).
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})

	// Leg 1: prevention reads the NAMED source. The knight is blue and has
	// protection from blue; the enabler is a colourless artifact, so without
	// DamageSource$ the hit would land.
	addMana(t, e, 0, "CC")
	opt := abilityOption(t, e, robot, 0)
	submitChoices(t, e, opt.Index)
	targetObject(t, e, knight)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(knight).Damage; got != 0 {
		t.Fatalf("the knight took %d damage from itself: the blue source's hit must be prevented (CR 702.16d)", got)
	}
	sawPrevent := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == knight && strings.Contains(ev.Text, "prevented") {
			sawPrevent = true
		}
	}
	if !sawPrevent {
		t.Fatal("no prevention Note for the knight")
	}

	// Leg 2: a DamageDone trigger's ValidSource$ reads the named source. The
	// drake deals 2 to itself (and dies to its own lethality); the Dragon
	// Watcher must fire off the DAMAGE SOURCE (a Dragon), never off the
	// colourless artifact enabler.
	beforeLife := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.Untap, Obj: robot})
	e.priorityRound() // re-ask: the standing decision predates the untap
	opt = abilityOption(t, e, robot, 0)
	submitChoices(t, e, opt.Index)
	targetObject(t, e, drake)
	drainStack(t, e, 20)
	if got := e.G.Players[0].Life; got != beforeLife+1 {
		t.Fatalf("watcher did not fire off the named source: life %d, want %d", got, beforeLife+1)
	}
	replayCheck(t, e, cfg)
}

// TestDestroyAllBatchLifelinkLKIIrrespectiveOfBattlefieldOrder pins the
// destruction batch's pre-batch LKI on real cards: a Basilisk Collar
// (grants deathtouch and lifelink) equipped to a Prodigal Pyromancer whose
// {T} damage ability sits on the stack; Pernicious Deed ({X}, sacrifice
// itself) destroys collar, bearer and itself in one sweep. When Tim's
// ability then resolves, its source's lifelink LKI must be true REGARDLESS
// of battlefield order (CR 603.10a/702.15c: LKI from immediately before the
// simultaneous event) -- the controller gains 1 with the damage.
func TestDestroyAllBatchLifelinkLKIIrrespectiveOfBattlefieldOrder(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, collarFirst := range []bool{true, false} {
		name := "collar first"
		if !collarFirst {
			name = "collar second"
		}
		t.Run(name, func(t *testing.T) {
			board := []string{"Prodigal Pyromancer", "Basilisk Collar", "Magus of the Disk"}
			if !collarFirst {
				board[0], board[1] = board[1], board[0]
			}
			e, cfg, ids := dsBoard(t, reg, board...)
			collarID := ids["Basilisk Collar"]
			timID := ids["Prodigal Pyromancer"]
			// Equip the collar to the pyromancer through the real Equip ability.
			addMana(t, e, 0, "CC")
			e.Advance()
			opt := abilityOption(t, e, collarID, 0)
			submitChoices(t, e, opt.Index)
			targetObject(t, e, timID)
			passUntilStackEmpty(t, e, 20)
			if !e.HasKeyword(timID, "Lifelink") {
				t.Fatal("bearer did not gain lifelink from the collar")
			}
			// Put Tim's damage ability on the stack, targeting seat 1.
			opt = abilityOption(t, e, timID, 0)
			submitChoices(t, e, opt.Index)
			idx := indexOfPlayerOption(e.Pending(), 1)
			if idx < 0 {
				t.Fatalf("seat 1 not offered: %+v", e.Pending().Options)
			}
			submitChoices(t, e, idx)
			// The opponent gets the first response after an activation; pass
			// their priority back to seat 0.
			passUntilSeat0Priority(t, e, 8)
			// In response, Magus of the Disk ({1}, {T}: destroy all
			// artifacts, creatures and enchantments) destroys collar,
			// bearer and itself -- an instant-speed destroy-all.
			e.emit(events.Event{Kind: events.Untap, Obj: ids["Magus of the Disk"]})
			e.priorityRound() // re-ask: the standing decision predates the untap
			addMana(t, e, 0, "C")
			opt = abilityOption(t, e, ids["Magus of the Disk"], 0)
			submitChoices(t, e, opt.Index)
			passUntilStackEmpty(t, e, 20)
			if z := e.G.Obj(timID).Zone; z != state.ZGraveyard {
				t.Fatalf("bearer survived the destroy-all in %s", z)
			}
			if z := e.G.Obj(collarID).Zone; z != state.ZGraveyard {
				t.Fatalf("collar survived in %s", z)
			}
			if got := e.G.Players[1].Life; got != 19 {
				t.Fatalf("seat 1 life = %d, want 19 (Tim's ability resolved)", got)
			}
			if n := countLifeChanges(t, e, 0, 1); n != 1 {
				t.Fatalf("logged %d LifeChange(0, +1) events, want 1: the bearer's lifelink LKI must not depend on battlefield order", n)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestNamedDamageSourceCreditsItsController proves DamageSource$ does not
// retain the resolving spell's controller when the named lifelink source is
// opponent-owned.
func TestNamedDamageSourceCreditsItsController(t *testing.T) {
	t.Parallel()
	link := "Name:Opponent Link\nTypes:Creature\nPT:2/5\nK:Lifelink\nOracle:x\n"
	e := layerEngine(t)
	id := onBoard(t, e, 1, link)
	ctx := &effects.Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}
	effects.Resolve(e, ctx, &cards.SA{Kind: "DB", API: "DealDamage", Params: map[string]string{
		"Defined": "Targeted", "DamageSource": "Targeted", "NumDmg": "2"}})
	if got := e.G.Players[1].Life; got != 22 {
		t.Fatalf("lifelink recipient seat 1 life = %d, want 22", got)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("resolving controller life = %d, want 20", got)
	}
}

// TestValakutExplorationExileProvenanceUsesItsRealScripts proves the card's
// Dig -> exile and end-step ChangeZoneAll -> DamageAll chain sees cards it
// exiled, rather than treating ExiledWithSource as an unknown predicate.
func TestValakutExplorationExileProvenanceUsesItsRealScripts(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := dsBoard(t, reg, "Valakut Exploration", "Grizzly Bears")
	val := ids["Valakut Exploration"]
	bear := ids["Grizzly Bears"]
	var card state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Face() != nil && o.Face().Name == "Mountain" {
			card = o.ID
			break
		}
	}
	if val == 0 || card == 0 {
		t.Fatalf("fixture ids valakut=%d card=%d", val, card)
	}
	if o := e.G.Obj(card); o.Zone != state.ZLibrary {
		e.emit(events.Event{Kind: events.MoveZone, Obj: card, From: o.Zone, To: state.ZLibrary})
	}
	order := append([]state.ObjID{card}, e.G.Zone(state.ZLibrary, 0)...)
	for i := 1; i < len(order); i++ {
		if order[i] == card {
			order = append(order[:i], order[i+1:]...)
			break
		}
	}
	e.emit(events.Event{Kind: events.Shuffle, Player: 0, IDs: order, Secret: true})
	f := e.G.Obj(val).Face()
	ctx := &effects.Ctx{Source: val, Controller: 0, SVars: f.SVars}
	effects.Resolve(e, ctx, cards.ResolveSVar(f.SVars, "TrigExile"))
	if o := e.G.Obj(card); o.Zone != state.ZExile || o.ExiledWith != val {
		t.Fatalf("Dig exile = zone %s source %d, want exile with %d", o.Zone, o.ExiledWith, val)
	}
	effects.Resolve(e, ctx, cards.ResolveSVar(f.SVars, "TrigGraveyard"))
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("Valakut end-step damage seat 1 life = %d, want 19", got)
	}
	if got := e.G.Obj(bear).Damage; got != 0 {
		t.Fatalf("Valakut player-only DamageAll marked the bear for %d, want 0", got)
	}
	if got := e.G.Obj(card).Zone; got != state.ZGraveyard {
		t.Fatalf("Valakut exiled card zone = %s, want graveyard", got)
	}
	replayCheck(t, e, cfg)
}

// TestSpitefulShadowsUsesTriggeredTargetAsDamageSource pins the one
// DamageSource$ referent the first implementation missed. The real Aura is
// attached to an opponent-owned lifelink creature; when that creature is dealt
// 2 damage, Spiteful's real trigger makes the creature deal 2 to its own
// controller. Its controller therefore loses 2 and gains 2, netting 20.
func TestSpitefulShadowsUsesTriggeredTargetAsDamageSource(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	creature := onBoard(t, e, 1, "Name:Linked Giant\nTypes:Creature Giant\nPT:2/5\nK:Lifelink\nOracle:x\n")
	pinger := onBoard(t, e, 0, "Name:Spark Source\nTypes:Creature Wizard\nPT:1/1\nOracle:x\n")
	auraObj := e.G.AddObject(mustCorpusCard(t, reg, "Spiteful Shadows"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: auraObj.ID, From: auraObj.Zone, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Attach, Obj: auraObj.ID, IDs: []state.ObjID{creature}})
	e.pending, e.pendingTriggers = nil, nil

	effects.Resolve(e, &effects.Ctx{Source: pinger, Controller: 0,
		Targets: []state.Target{{Obj: creature}}}, &cards.SA{Kind: "DB", API: "DealDamage",
		Params: map[string]string{"Defined": "Targeted", "NumDmg": "2"}})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("Spiteful queued %d triggers, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("opponent life = %d, want 20 (2 damage, then +2 lifelink)", got)
	}
	if n := countLifeChanges(t, e, 1, 2); n != 1 {
		t.Fatalf("logged %d LifeChange(1, +2) events, want 1", n)
	}
	sawReflected := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == 0 && ev.Player == 1 && ev.Amount == 2 {
			sawReflected = true
		}
	}
	if !sawReflected {
		t.Fatal("Spiteful's reflected player Damage event is missing")
	}
}

// TestStalkingVengeanceUsesTheDyingCreaturesDerivedPowerLKI proves a real
// TriggeredCard$CardPower script sees the event object's pre-move snapshot
// after trigger placement. The Bear has a layer-derived +2/+2 before dying;
// the live graveyard object has neither that layer nor battlefield counters,
// so only the preserved trigger LKI can deal 4.
func TestStalkingVengeanceUsesTheDyingCreaturesDerivedPowerLKI(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := dsBoard(t, reg, "Stalking Vengeance", "Grizzly Bears")
	bear := ids["Grizzly Bears"]
	e.AddContinuous(state.ContinuousEffect{Source: bear, Controller: 0,
		Affects: "Card.Self", Layer: LPT, Sub: SubModify, AddPower: 2, AddToughness: 2})
	if got := e.Power(bear); got != 4 {
		t.Fatalf("pumped Bear power = %d, want 4", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	if !e.putTriggersOnStack() {
		t.Fatal("Stalking Vengeance did not ask for its trigger target")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Stalking target decision = %+v", d)
	}
	idx := indexOfPlayerOption(d, 1)
	if idx < 0 {
		t.Fatalf("seat 1 not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[1].Life; got != 16 {
		t.Fatalf("Stalking damage left seat 1 at %d, want 16", got)
	}
	replayCheck(t, e, cfg)
}

// TestAnimatedWalkerDamageAndCleanup uses a layer-animated printed walker to
// pin both halves of CR 120.3e and the negative-Damage cleanup boundary. It
// drives the production DealDamage emitter so removing its derived-creature
// marker makes the marked-damage assertion fail.
func TestAnimatedWalkerDamageAndCleanup(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	walkerObj := e.G.AddObject(card(t, "Name:Animated Jace\nTypes:Planeswalker Jace\nLoyalty:4\nOracle:x\n"), 0)
	walker := walkerObj.ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: walker, From: walkerObj.Zone, To: state.ZBattlefield})
	e.AddContinuous(state.ContinuousEffect{Source: walker, Controller: 0,
		Affects: "Card.Self", Layer: LType, AddTypes: []string{"Creature"}})
	effects.Resolve(e, &effects.Ctx{Source: walker, Controller: 0,
		Targets: []state.Target{{Obj: walker}}}, &cards.SA{Kind: "DB", API: "DealDamage",
		Params: map[string]string{"Defined": "Targeted", "NumDmg": "2"}})
	if o := e.G.Obj(walker); o.Damage != 2 || o.Counter("LOYALTY") != 2 {
		t.Fatalf("animated walker after damage: marked=%d loyalty=%d, want 2/2", o.Damage, o.Counter("LOYALTY"))
	}
	e.cleanupBody()
	if o := e.G.Obj(walker); o.Damage != 0 || o.Counter("LOYALTY") != 2 {
		t.Fatalf("animated walker after cleanup: marked=%d loyalty=%d, want 0/2", o.Damage, o.Counter("LOYALTY"))
	}
}

// TestRefPropertyCountsUseDerivedPT proves Targeted$ properties include
// continuous modifications and both +1/+1 and -1/-1 counters.
func TestRefPropertyCountsUseDerivedPT(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Count Target\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.AddContinuous(state.ContinuousEffect{Source: id, Controller: 0,
		Affects: "Card.Self", Layer: LPT, Sub: SubModify, AddPower: 3, AddToughness: 3})
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 2})
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "M1M1", Amount: 1})
	ctx := &effects.Ctx{Controller: 0, Targets: []state.Target{{Obj: id}}}
	if got := effects.EvalCount(e, ctx, "Targeted$CardPower"); got != 6 {
		t.Fatalf("Targeted CardPower = %d, want 6 (2 + 3 + 2 - 1)", got)
	}
	if got := effects.EvalCount(e, ctx, "Targeted$CardToughness"); got != 6 {
		t.Fatalf("Targeted CardToughness = %d, want 6 (2 + 3 + 2 - 1)", got)
	}
}

// TestNamedDamageSourceRealCastGainsOpponentController is the round-2 review's
// opponent-owned lifelink probe, driven through the REAL cast-and-resolution
// path (not a direct effects.Resolve): a spell with DamageSource$ Targeted at
// an OPPONENT-controlled lifelink creature makes that creature deal 2 to
// itself, and the lifelink rider must pay the CREATURE's controller (seat 1),
// never the caster. Pre-fix (controller := c.Controller) the +2 went to seat 0.
func TestNamedDamageSourceRealCastGainsOpponentController(t *testing.T) {
	t.Parallel()
	spell := "Name:Redirector\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2 | DamageSource$ Targeted | " +
		"SpellDescription$ The target creature deals damage to itself.\nOracle:x\n"
	oppLink := "Name:Opp Link\nManaCost:G\nTypes:Creature\nPT:2/5\nK:Lifelink\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 42, spell)
	opp := onBoard(t, e, 1, oppLink)
	moveToHand(t, e, id)
	addMana(t, e, 0, "R")
	castFirst(t, e, "cast")
	targetObject(t, e, opp)
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Players[1].Life; got != 22 {
		t.Fatalf("opponent life = %d, want 22 (opponent controls the lifelink source)", got)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("caster life = %d, want 20 (caster must not gain)", got)
	}
	recip := -1
	for _, ev := range e.L.Events {
		if ev.Kind == events.LifeChange && ev.Amount == 2 {
			recip = int(ev.Player)
		}
	}
	if recip != 1 {
		t.Fatalf("lifelink LifeChange recipient = seat %d, want seat 1", recip)
	}
}

// TestPrintedCreatureWalkerCleanupKeepsLoyalty is the round-2 review's
// printed-creature-walker clean-up probe: a face that is BOTH Creature and
// Planeswalker takes marked damage AND loses loyalty, and the end-of-step
// clean-up's negative Damage event must clear ONLY the marked damage -- it
// must never re-apply the CR 306.8 loyalty conversion (which would "restore"
// loyalty 2 -> 4). Pre-fix the clean-up restored loyalty because the
// conversion ignored the sign of the Damage amount.
func TestPrintedCreatureWalkerCleanupKeepsLoyalty(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	walker := card(t, "Name:Printed Walker\nTypes:Creature Planeswalker\nLoyalty:4\nPT:2/4\nOracle:x\n")
	w := e.G.AddObject(walker, 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: w.ID, From: w.Zone, To: state.ZBattlefield})
	effects.Resolve(e, &effects.Ctx{Source: w.ID, Controller: 0,
		Targets: []state.Target{{Obj: w.ID}}}, &cards.SA{Kind: "DB", API: "DealDamage",
		Params: map[string]string{"Defined": "Targeted", "NumDmg": "2"}})
	if o := e.G.Obj(w.ID); o.Damage != 2 || o.Counter("LOYALTY") != 2 {
		t.Fatalf("printed creature-walker after damage: marked=%d loyalty=%d, want 2/2", o.Damage, o.Counter("LOYALTY"))
	}
	e.cleanupBody()
	if o := e.G.Obj(w.ID); o.Damage != 0 || o.Counter("LOYALTY") != 2 {
		t.Fatalf("printed creature-walker after cleanup: marked=%d loyalty=%d, want 0/2 (cleanup must NOT restore loyalty)", o.Damage, o.Counter("LOYALTY"))
	}
}

// TestBatchLifelinkClearedAfterRegeneratedBatchMember is the round-2 review's
// stale-snapshot probe: a destroy-all batch in which a lifelink-bearing member
// is REGENERATED (stays) never consumes its pre-batch lifelink snapshot. The
// TestDepartedStolenSourceCreditsLastController exercises the independently
// resolving path after the source has left the battlefield. Move resets an
// object's Controller to Owner, so this catches the bug where lifelink paid
// owner 0 rather than last controller 1. The source is also the named
// DamageSource$ (Self), ensuring the rider's named-source path uses the same
// LKI controller rather than merely the resolving ability's controller.
func TestDepartedStolenSourceCreditsLastController(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoard(t, e, 0, "Name:Stolen Spark\nTypes:Creature\nPT:2/2\nK:Lifelink\n"+
		"A:AB$ DealDamage | Defined$ You | NumDmg$ 2 | DamageSource$ Self\nOracle:x\n")
	e.emit(events.Event{Kind: events.ControlChange, Obj: source, Player: 1})
	e.emit(events.Event{Kind: events.AbilityPush, Obj: source, Player: 1})
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(source).Controller; got != 0 {
		t.Fatalf("fixture departure controller = %d, want owner 0", got)
	}
	stack := e.G.Stack[len(e.G.Stack)-1]
	if got, ok := e.sourceControllerLKI[stack]; !ok || got != 1 {
		t.Fatalf("source controller LKI = %d, valid=%v, want 1/true", got, ok)
	}
	e.resolveTop()
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("owner life = %d, want 20", got)
	}
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("last controller life = %d, want 20 (damage and lifelink net)", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.LifeChange && ev.Amount == 2 && ev.Player != 1 {
			t.Fatalf("lifelink recipient = seat %d, want last controller seat 1", ev.Player)
		}
	}
}

// TestBatchLifelinkClearedAfterRegeneratedBatchMember checks that the
// end-of-batch clear resets batchDamageKeywords so a later departure reads live
// state rather than a stale pre-batch TRUE entry.
func TestBatchLifelinkClearedAfterRegeneratedBatchMember(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoard(t, reg, "Prodigal Pyromancer", "Basilisk Collar", "Magus of the Disk")
	collarID := ids["Basilisk Collar"]
	timID := ids["Prodigal Pyromancer"]
	addMana(t, e, 0, "CC")
	e.Advance()
	opt := abilityOption(t, e, collarID, 0)
	submitChoices(t, e, opt.Index)
	targetObject(t, e, timID)
	passUntilStackEmpty(t, e, 20)
	if !e.HasKeyword(timID, "Lifelink") {
		t.Fatal("bearer did not gain lifelink from the collar")
	}
	regenEffect(e, timID, "Regenerate", nil)
	e.emit(events.Event{Kind: events.Untap, Obj: ids["Magus of the Disk"]})
	e.priorityRound()
	addMana(t, e, 0, "C")
	opt = abilityOption(t, e, ids["Magus of the Disk"], 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(timID).Zone; z != state.ZBattlefield {
		t.Fatalf("bearer zone = %s, want battlefield (regenerated)", z)
	}
	if z := e.G.Obj(collarID).Zone; z != state.ZGraveyard {
		t.Fatalf("collar zone = %s, want graveyard", z)
	}
	if e.batchDamageKeywords != nil {
		t.Fatalf("batchDamageKeywords=%v leaked after a batch with a regenerated member", e.batchDamageKeywords)
	}
}
