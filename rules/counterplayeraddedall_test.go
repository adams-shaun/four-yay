// trig:CounterPlayerAddedAll — "Whenever you put one or more counters on
// a(n) <spec>" (Generous Patron, Rikku Resourceful Guardian, Kros Defense
// Contractor, All Will Be One). Pinned end to end on the two corpus cards
// the ticket names: the Patron's support-then-draw chain (one draw per
// counter-PLACING EVENT, never per counter) and Rikku's CantBlockBy window.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestRikkuCounterPutMakesTheCreatureUnblockable pins Rikku, Resourceful
// Guardian end to end: a counter you put on a creature registers the
// CantBlockBy effect (RememberObjects$ TriggeredObjectLKICopy resolves the
// gaining creature), and the combat blocker decision for the opponent no
// longer offers that creature as a blocker — the attack deals its damage
// unblocked. A control game without the put proves the assertions can fail.
func TestRikkuCounterPutMakesTheCreatureUnblockable(t *testing.T) {
	t.Parallel()
	counterSpell := "Name:Count Up\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ PutCounter | Cost$ R | CounterType$ P1P1 | ValidTgts$ Creature | CounterNum$ 1\nOracle:x\n"
	reg := testutil.CorpusRegistry(t)
	rikku, ok := reg.Lookup("Rikku, Resourceful Guardian")
	if !ok {
		t.Fatal("corpus has no Rikku, Resourceful Guardian")
	}
	// The whole board is built through logged events (deck genesis + real
	// battlefield entries), so the game replays from the log alone.
	cfg := seatZeroStart(Config{Seed: 6101, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{card(t, counterSpell), rikku}, mountainDeck(t, 38)...),
			append([]*cards.Card{card(t, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")}, mountainDeck(t, 39)...),
		},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	var spellID, rikkuID, bearID state.ObjID
	findRikku := func(zone state.Zone, p state.PlayerID) {
		for _, id := range e.G.Zone(zone, p) {
			if e.G.Obj(id).Face().Name == "Rikku, Resourceful Guardian" {
				rikkuID = id
			}
		}
	}
	findRikku(state.ZHand, 0)
	if rikkuID == 0 {
		findRikku(state.ZLibrary, 0)
	}
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if e.G.Obj(id).Face().Name == "Count Up" {
			spellID = id
		}
	}
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		if e.G.Obj(id).Face().Name == "Runeclaw Bear" {
			bearID = id
		}
	}
	if spellID == 0 || rikkuID == 0 || bearID == 0 {
		t.Fatalf("deck bridge failed: spell %d rikku %d bear %d", spellID, rikkuID, bearID)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: rikkuID, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearID, From: state.ZLibrary, To: state.ZBattlefield})
	if o := e.G.Obj(spellID); o.Zone == state.ZLibrary {
		e.emit(events.Event{Kind: events.MoveZone, Obj: spellID, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}

	// Rikku's window is "until end of turn", so the counter and the attack
	// must happen in the SAME turn: drive first until Rikku is past summoning
	// sickness, cast the counter at Main1, then move to declare-attackers.
	driveToStepAll(t, e, 3, 0, state.StepMain1)

	// Precondition: the target ask's first candidate is Rikku (seat 0's only
	// creature) and the board is where the rule looks.
	addMana(t, e, 0, "R")
	d := e.Pending()
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == spellID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the counter spell: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) == 0 || d.Options[0].Obj != rikkuID {
		t.Fatalf("after casting: %+v, want the target ask with Rikku as option 0", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(rikkuID).Counter("P1P1"); got != 1 {
		t.Fatalf("Rikku +1/+1 counters after the put = %d, want 1", got)
	}
	var ce *ContinuousEffect
	for i := range e.continuous {
		if e.continuous[i].Restriction == "CantBlockBy" && len(e.continuous[i].Remembered) == 1 && e.continuous[i].Remembered[0] == rikkuID {
			ce = &e.continuous[i]
		}
	}
	if ce == nil {
		t.Fatal("no CantBlockBy effect registered remembering Rikku after the counter put")
	}
	if !e.blockRestricted(bearID, rikkuID) {
		t.Fatal("seat 1's bear can still block the countered Rikku")
	}

	// End to end through the combat decisions in the SAME turn: Rikku attacks,
	// the bear is offered no block, and the 3/4 Rikku (2/3 + the counter)
	// deals 3.
	driveToStepAll(t, e, 3, 0, state.StepDeclareAttackers)
	e.askAttackers()
	submitAttackers(t, e, rikkuID)
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("defender life after the unblocked attack = %d, want 17", got)
	}
	replayCheck(t, e, cfg)
}

// TestRikkuWindowUncontrolledWithoutThePut is the can-fail control: the same
// board with NO counter put leaves the bear a legal blocker for Rikku and the
// blocker decision posed.
func TestRikkuWindowUncontrolledWithoutThePut(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	rikku, ok := reg.Lookup("Rikku, Resourceful Guardian")
	if !ok {
		t.Fatal("corpus has no Rikku, Resourceful Guardian")
	}
	e := combatEngine(t)
	rikkuID := onBoardCard(t, e, 0, rikku)
	e.G.Obj(rikkuID).SummonSick = false
	bear := onBoard(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	if e.blockRestricted(bear, rikkuID) {
		t.Fatal("control: the bear was restricted without any counter put")
	}
	e.askAttackers()
	submitAttackers(t, e, rikkuID)
	e.G.Step = state.StepDeclareBlockers
	e.askBlockers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("control blocker decision = %+v, want the bear offered as a blocker", d)
	}
	found := false
	for _, opt := range d.Options {
		if opt.Obj == bear {
			found = true
		}
	}
	if !found {
		t.Fatalf("control: the bear had no block option: %+v", d.Options)
	}
}

// TestRikkuWindowExpiresAtEndOfTurn is the expiry regression for the MAJOR: Rikku's
// `DB$ Effect` body carries no `Duration$`, but its trigger text is explicitly
// "until end of turn". Before the fix `effectUntilEOT` read the absent duration
// as `Permanent` and installed a restriction that only ended when Rikku left the
// battlefield, so the countered creature stayed unblockable for the rest of the
// game. This drives the real turn boundary: the counter goes on, the window holds
// through that turn, and by the next turn's declare-blockers the bear is a legal
// blocker again.
func TestRikkuWindowExpiresAtEndOfTurn(t *testing.T) {
	t.Parallel()
	counterSpell := "Name:Count Up\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ PutCounter | Cost$ R | CounterType$ P1P1 | ValidTgts$ Creature | CounterNum$ 1\nOracle:x\n"
	reg := testutil.CorpusRegistry(t)
	rikku, ok := reg.Lookup("Rikku, Resourceful Guardian")
	if !ok {
		t.Fatal("corpus has no Rikku, Resourceful Guardian")
	}
	cfg := seatZeroStart(Config{Seed: 6101, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{card(t, counterSpell), rikku}, mountainDeck(t, 38)...),
			append([]*cards.Card{card(t, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:5/5\nOracle:x\n")}, mountainDeck(t, 39)...),
		},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	var spellID, rikkuID, bearID state.ObjID
	findRikku := func(zone state.Zone, p state.PlayerID) {
		for _, id := range e.G.Zone(zone, p) {
			if e.G.Obj(id).Face().Name == "Rikku, Resourceful Guardian" {
				rikkuID = id
			}
		}
	}
	findRikku(state.ZHand, 0)
	if rikkuID == 0 {
		findRikku(state.ZLibrary, 0)
	}
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if e.G.Obj(id).Face().Name == "Count Up" {
			spellID = id
		}
	}
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		if e.G.Obj(id).Face().Name == "Runeclaw Bear" {
			bearID = id
		}
	}
	if spellID == 0 || rikkuID == 0 || bearID == 0 {
		t.Fatalf("deck bridge failed: spell %d rikku %d bear %d", spellID, rikkuID, bearID)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: rikkuID, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearID, From: state.ZLibrary, To: state.ZBattlefield})
	if o := e.G.Obj(spellID); o.Zone == state.ZLibrary {
		e.emit(events.Event{Kind: events.MoveZone, Obj: spellID, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}

	addMana(t, e, 0, "R")
	d := e.Pending()
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == spellID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the counter spell: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) == 0 || d.Options[0].Obj != rikkuID {
		t.Fatalf("after casting: %+v, want the target ask with Rikku as option 0", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 30)

	// Precondition: the put landed and the restriction is live THIS turn.
	if got := e.G.Obj(rikkuID).Counter("P1P1"); got != 1 {
		t.Fatalf("precondition: Rikku +1/+1 counters after the put = %d, want 1", got)
	}
	if !e.blockRestricted(bearID, rikkuID) {
		t.Fatal("precondition: the window is not active in the turn the counter was put")
	}

	// Drive the real turn boundary: Rikku's window is "until end of turn",
	// so the restriction must be gone by seat 0's NEXT turn (seat 1's turn
	// intervenes, so that is putTurn+2).
	putTurn := e.G.Turn
	driveToStepAll(t, e, putTurn+2, 0, state.StepDeclareAttackers)
	if e.blockRestricted(bearID, rikkuID) {
		t.Fatalf("after the end of turn %d the bear is still forbidden from blocking "+
			"(the window must expire, not persist until Rikku leaves)", putTurn)
	}
	// End to end: the bear is offered as a blocker for Rikku's attack.
	e.G.Obj(rikkuID).SummonSick = false
	e.askAttackers()
	submitAttackers(t, e, rikkuID)
	e.G.Step = state.StepDeclareBlockers
	e.askBlockers()
	d = e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("blocker decision after the window expired = %+v, want the bear offered", d)
	}
	found := false
	for _, opt := range d.Options {
		if opt.Obj == bearID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the bear had no block option on the turn after Rikku's window: %+v", d.Options)
	}
}

// TestLeadByExampleSpellSupportOffersEveryCreature pins the CR 701.41a SPELL
// half on the real corpus carrier: Lead by Example (an instant, `Support$ 2`,
// oracle "Put a +1/+1 counter on each of up to two target creatures." -- no
// "other", because a spell resolving from the stack is not a creature and
// could never be its own target). The support pick must offer EVERY battlefield
// creature -- the caster's own and the opponent's -- with Min 0 ("up to"), and
// the answered pick puts one counter per chosen creature. The permanent half's
// opposite ("other": a permanent's support cannot target its own source) is
// pinned by the Generous Patron probe above.
func TestLeadByExampleSpellSupportOffersEveryCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lead, ok := reg.Lookup("Lead by Example")
	if !ok {
		t.Fatal("corpus has no Lead by Example")
	}
	// Preconditions: the carrier's only ability is the Support$ 2 cast, so the
	// test exercises the branch it names.
	if len(lead.Faces) != 1 || len(lead.Faces[0].Abilities) != 1 ||
		lead.Faces[0].Abilities[0].Params["Support"] != "2" {
		t.Fatalf("Lead by Example abilities = %+v, want one SP$ PutCounter with Support$ 2", lead.Faces[0].Abilities)
	}
	e := layerEngine(t)
	own := onBoard(t, e, 0, "Name:Goblin Skirmisher\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	bear := onBoard(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	sp := e.G.AddObject(lead, 0)
	sp.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), sp.ID))
	e.Advance() // start the game loop (toss + opening deal; no mulligan ask with the default config)
	addMana(t, e, 0, "CG")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after addMana: %+v, want seat 0's priority", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == sp.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Lead by Example: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// Priority on the stack: pass until someone is asked a real question (the
	// support pick, posed when the spell resolves).
	for i := 0; ; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision after the cast")
		}
		if d.Kind != decision.KPriority {
			break
		}
		if i > 10 {
			t.Fatal("priority never left after the Lead by Example cast")
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}})
	}

	// The support pick IS the targeting for a Support$ cast (no ValidTgts$, no
	// KTarget stage): Min 0 ("up to"), Max 2, and every creature offered — the
	// caster's own first in zone order, then the opponent's.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_pick" {
		t.Fatalf("after casting: %+v, want the support counter_pick ask", d)
	}
	if d.Player != 0 || d.Min != 0 || d.Max != 2 {
		t.Fatalf("support ask player/range = seat %d %d..%d, want seat 0 0..2", d.Player, d.Min, d.Max)
	}
	if len(d.Options) != 2 {
		t.Fatalf("support ask options = %d, want 2 (every creature on the battlefield)", len(d.Options))
	}
	if d.Options[0].Obj != own || d.Options[1].Obj != bear {
		t.Fatalf("support options = [%d %d], want [%d %d] in zone order", d.Options[0].Obj, d.Options[1].Obj, own, bear)
	}
	// Answer with BOTH creatures: one counter each (Support is never a
	// per-creature count, even with count left over).
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	passUntilStackEmpty(t, e, 60)
	if got := e.G.Obj(own).Counter("P1P1"); got != 1 {
		t.Fatalf("own creature +1/+1 counters after Lead by Example = %d, want 1", got)
	}
	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("opponent creature +1/+1 counters after Lead by Example = %d, want 1", got)
	}
}

// TestAllWillBeOnePlayerCounterBranchDealsBatchDamage pins the PLAYER half of
// the matcher end to end on the real corpus card All Will Be One: its
// `ValidObject$ Permanent.inRealZoneBattlefield,Player` line must fire on a
// PlayerCounterChange placement (a player gaining counters), and the body's
// `NumDmg$ X` with `SVar:X:TriggerCount$Amount` must read the batch size --
// three poison counters deal three damage, not one and not zero.
func TestAllWillBeOnePlayerCounterBranchDealsBatchDamage(t *testing.T) {
	t.Parallel()
	awo, ok := testutil.CorpusRegistry(t).Lookup("All Will Be One")
	if !ok {
		t.Fatal("corpus has no All Will Be One")
	}
	e := combatEngine(t)
	ench := onBoardCard(t, e, 0, awo)
	e.G.Active = 0
	bear := onBoard(t, e, 1, "Name:Big Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:5/5\nOracle:x\n")

	// Precondition: the enchantment is on the battlefield and the trigger line
	// is the CounterPlayerAddedAll mode carrying the ,Player alternative the
	// player branch needs (a creature-only line would pass this test against
	// the object branch by accident).
	if o := e.G.Obj(ench); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: All Will Be One is not on the battlefield")
	}
	trig := e.G.Obj(ench).Face().Triggers[0]
	if trig.Mode != "CounterPlayerAddedAll" {
		t.Fatalf("precondition: trigger mode = %q, want CounterPlayerAddedAll", trig.Mode)
	}
	if trig.Params["ValidObject"] != "Permanent.inRealZoneBattlefield,Player" {
		t.Fatalf("precondition: ValidObject = %q, want the permanent-or-player spec", trig.Params["ValidObject"])
	}

	// A player-counter placement caused by seat 0: publish the adder the way a
	// cost or turn-based placement does (no stack cause exists for one).
	e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "POISON", Amount: 3})
	e.SetCounterAdder(counterAdderUnset) // restore "no publication"

	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers after a 3-counter player batch = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after the trigger was put on the stack: %+v, want the damage target ask", d)
	}
	tidx := -1
	for _, opt := range d.Options {
		if opt.Obj == bear {
			tidx = opt.Index
		}
	}
	if tidx < 0 {
		t.Fatalf("the bear was not offered as a damage target: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{tidx}}); err != nil {
		t.Fatalf("submit damage target: %v", err)
	}
	passUntilStackEmpty(t, e, 30)

	// A 5/5 bear survives the damage, so the marked damage (not the resolved
	// count) is what is read: 3 is the batch, and a hard-coded 1 would fail.
	if got := e.G.Obj(bear).Damage; got != 3 {
		t.Fatalf("bear damage after a 3-counter PLAYER batch = %d, want 3 (TriggerCount$Amount)", got)
	}
}
