package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task vow1: Promise of Loyalty end to end on the REAL corpus card.
//
// The card is a total chain: SP$ RepeatEach (RepeatPlayers$ Player) asks each
// player -- via DBPutCounter's bare Choices$ pick (Chooser$
// Player.IsRemembered, the RepeatEach loop subject) -- to vow one of their own
// creatures, RememberCards$ True remembers the chosen one, the chained
// SacAllOthers sacrifices each player's OTHER creatures through the
// `!IsRemembered+ControlledBy Player.IsRemembered` filter bridge, and the
// loop-tail DBEffect registers a Mode$ CantAttack continuous effect whose
// ValidCard$ Card.IsRemembered scopes it to exactly the vowed creatures, with
// ForgetOnMoved$ Battlefield and ForgetCounter$ VOW lifetimes. Before this
// task the whole card was a silent no-op (one loud Note, zero counters, zero
// sacrifices, zero asks).
//
// The fixture deck is compiled corpus cards only (no Forge script text is
// committed); every fixture mutation goes through e.emit, so every test ends
// replay-verified.

func vowRestrictionGame(t *testing.T, seed uint64, perSeat ...int) (*Engine, Config, []*state.Object) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	promise, ok := reg.Lookup("Promise of Loyalty")
	if !ok {
		t.Fatal("corpus missing Promise of Loyalty")
	}
	bear := card(t, staticBearFixture)
	seats := len(perSeat)
	hs := make([][]*cards.Card, seats)
	bs := make([][]*cards.Card, seats)
	hs[0] = []*cards.Card{promise}
	for i := 0; i < seats; i++ {
		for k := 0; k < perSeat[i]; k++ {
			bs[i] = append(bs[i], bear)
		}
	}
	e, cfg := restrictionGame(t, seed, hs, bs)
	var out []*state.Object
	for seat := 0; seat < seats; seat++ {
		found := 0
		for _, id := range e.G.Zone(state.ZBattlefield, state.PlayerID(seat)) {
			o := e.G.Obj(id)
			if o != nil && o.Card == bear {
				out = append(out, o)
				found++
			}
		}
		if found != perSeat[seat] {
			t.Fatalf("seat %d: %d of %d fixture bears on the battlefield", seat, found, perSeat[seat])
		}
	}
	return e, cfg, out
}

// castVow funds {4}{W} from the pool, casts Promise of Loyalty off the
// pending priority decision, and returns the first mid-resolution decision
// (seat 0's vow pick, once both other... once every seat has passed).
func castVow(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	promiseID := findAndMoveToHand(t, e, 0, "Promise of Loyalty")
	addMana(t, e, 0, "WWWWW")
	castFromPriority(t, e, promiseID)
	return passUntilNonPriority(t, e, 60)
}

// answerVowPick submits one option of the pending counter_pick ask and drives
// to the next non-priority decision (the next player's pick, or priority when
// the resolution completed).
func answerVowPick(t *testing.T, e *Engine, d *decision.Decision, option int) *decision.Decision {
	t.Helper()
	submitChoices(t, e, d.Options[option].Index)
	return passUntilNonPriority(t, e, 60)
}

// vowEffect finds the registered CantAttack continuous effect (the vow).
func vowEffect(t *testing.T, e *Engine) *ContinuousEffect {
	t.Helper()
	for i := range e.continuous {
		ce := &e.continuous[i]
		if ce.Restriction == "CantAttack" && ce.ForgetCounter == "VOW" {
			return ce
		}
	}
	t.Fatal("no registered VOW CantAttack continuous effect")
	return nil
}

// TestPromiseOfLoyaltyAsksEachPlayerAndVowsACreature is the reported
// scenario: every seat is asked (KChoose Min==Max==1, ResumeKind
// "counter_pick", one option per that seat's OWN creature), the answered
// creature — NOT the deterministic first — takes the VOW counter, the rest of
// that seat's creatures are sacrificed (the SacAllOthers filter honoured the
// RememberCards$ remembered set: the vowed creature survives), and the
// registered effect remembers exactly the chosen creatures.
func TestPromiseOfLoyaltyAsksEachPlayerAndVowsACreature(t *testing.T) {
	t.Parallel()
	e, cfg, bears := vowRestrictionGame(t, 6121, 3, 2)
	d := castVow(t, e)

	// Seat 0's pick: three options (its own three creatures), Min==Max==1.
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_pick" {
		t.Fatalf("after casting Promise of Loyalty: %+v, want seat 0's vow pick", d)
	}
	if d.Min != 1 || d.Max != 1 || d.Player != 0 {
		t.Fatalf("vow ask range/player = %d..%d seat %d, want 1..1 seat 0", d.Min, d.Max, d.Player)
	}
	if len(d.Options) != 3 {
		t.Fatalf("vow ask has %d options, want seat 0's three creatures", len(d.Options))
	}
	for i, o := range d.Options {
		if o.Obj != bears[i].ID {
			t.Fatalf("option %d = obj %d, want seat 0's zone-order bear %d", i, o.Obj, bears[i].ID)
		}
	}
	// Answer with the SECOND creature (the deterministic first pick would
	// vow bears[0]).
	d = answerVowPick(t, e, d, 1)

	// Seat 1's pick: two options.
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_pick" || d.Player != 1 {
		t.Fatalf("after seat 0's answer: %+v, want seat 1's vow pick", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("seat 1's vow ask has %d options, want 2", len(d.Options))
	}
	vow0, vow1 := bears[1].ID, bears[4].ID
	submitChoices(t, e, d.Options[1].Index)
	passUntilStackEmpty(t, e, 60)

	// The two chosen creatures are vowed (one VOW counter each) and alive;
	// the other four were sacrificed.
	if got := e.G.Obj(vow0).Counter("VOW"); got != 1 {
		t.Fatalf("seat 0's vowed bear VOW counters = %d, want 1", got)
	}
	if got := e.G.Obj(vow1).Counter("VOW"); got != 1 {
		t.Fatalf("seat 1's vowed bear VOW counters = %d, want 1", got)
	}
	if o := e.G.Obj(vow0); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("seat 0's vowed bear did not survive")
	}
	if o := e.G.Obj(vow1); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("seat 1's vowed bear did not survive")
	}
	sacs := sacrificeEvents(t, e)
	if len(sacs) != 3 {
		t.Fatalf("got %d sacrifice moves, want 3 (two other seat-0 bears, one other seat-1 bear)", len(sacs))
	}
	for _, ev := range sacs {
		if ev.Obj == vow0 || ev.Obj == vow1 {
			t.Fatalf("the vowed bear %d was sacrificed", ev.Obj)
		}
	}
	for _, id := range []state.ObjID{bears[0].ID, bears[2].ID, bears[3].ID} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("unvowed bear %d not in the graveyard: %+v", id, o)
		}
	}

	// The registered effect remembers EXACTLY the chosen creatures.
	ce := vowEffect(t, e)
	if len(ce.Remembered) != 2 || ce.Remembered[0] != vow0 || ce.Remembered[1] != vow1 {
		t.Fatalf("vow effect Remembered = %v, want [%d %d]", ce.Remembered, vow0, vow1)
	}
	// The CantAttack scoping: "You" is the EFFECT's controller (the caster,
	// seat 0) — seat 1's vowed creature cannot attack seat 0; nothing here
	// blocks any other pair.
	if !e.attackBlocked(vow1, 0, 0) {
		t.Fatal("seat 1's vowed bear may still attack the vow's caster")
	}
	if e.attackBlocked(vow1, 1, 0) {
		t.Fatal("seat 1's vowed bear is blocked from a pair it was never restricted from")
	}
	if e.attackBlocked(vow0, 1, 0) {
		t.Fatal("the caster's own vowed bear is blocked from attacking another player — Target$ You only blocks pairs against the caster")
	}
	replayCheck(t, e, cfg)
}

// TestPromiseOfLoyaltySingleCreatureTakesTheVowWithoutAsking pins the
// strict-supersets gate on the real chain: a player controlling exactly one
// eligible creature has no choice to make, so no counter_pick ask is ever
// posed for them — the vow and the sacrifice (of nothing) resolve
// deterministically.
func TestPromiseOfLoyaltySingleCreatureTakesTheVowWithoutAsking(t *testing.T) {
	t.Parallel()
	e, cfg, bears := vowRestrictionGame(t, 6122, 1, 1)
	castVow(t, e)
	passUntilStackEmpty(t, e, 60)
	for i, b := range bears {
		if got := b.Counter("VOW"); e.G.Obj(b.ID).Counter("VOW") != 1 {
			t.Fatalf("seat %d's lone bear VOW counters = %d, want 1", i, got)
		}
	}
	if len(sacrificeEvents(t, e)) != 0 {
		t.Fatal("a sacrifice ran although every creature was vowed")
	}
	if ce := vowEffect(t, e); len(ce.Remembered) != 2 {
		t.Fatalf("vow effect Remembered = %v, want both lone bears", ce.Remembered)
	}
	replayCheck(t, e, cfg)
}

// TestPromiseOfLoyaltyVowForgets pins both ForgetCounter$ halves of the vow
// effect's lifetime. Three seats so "another defender" exists for the
// scoping: seat 1's vowed creature cannot attack seat 0 (the caster) but may
// attack seat 2.
//
//   - a count that DROPS without reaching zero keeps the restriction (a
//     second VOW counter added and removed leaves the card remembered);
//   - the count REACHING zero (the last VOW counter removed) drops the card
//     from the effect's Remembered set and the restriction stops applying;
//   - ForgetOnMoved$ Battlefield (the creature dying) drops it the same way.
func TestPromiseOfLoyaltyVowForgets(t *testing.T) {
	t.Parallel()
	e, cfg, bears := vowRestrictionGame(t, 6123, 2, 2, 2)
	d := castVow(t, e)
	// Three picks, one per seat in turn order; each answers its second bear.
	for i := 0; i < 3; i++ {
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_pick" || d.Player != state.PlayerID(i) {
			t.Fatalf("vow pick %d: %+v, want seat %d's counter_pick ask", i, d, i)
		}
		if len(d.Options) != 2 {
			t.Fatalf("seat %d's vow ask has %d options, want 2", i, len(d.Options))
		}
		if i == 2 {
			submitChoices(t, e, d.Options[1].Index)
			passUntilStackEmpty(t, e, 60)
			break
		}
		d = answerVowPick(t, e, d, 1)
	}
	v0, v1, v2 := bears[1].ID, bears[3].ID, bears[5].ID
	ce := vowEffect(t, e)
	if len(ce.Remembered) != 3 {
		t.Fatalf("vow effect Remembered = %v, want all three vowed bears", ce.Remembered)
	}
	if !e.attackBlocked(v1, 0, 0) || e.attackBlocked(v1, 2, 0) {
		t.Fatalf("scoping wrong: blocked(1→0)=%v blocked(1→2)=%v", e.attackBlocked(v1, 0, 0), e.attackBlocked(v1, 2, 0))
	}

	// Drop WITHOUT reaching zero keeps the card: a second VOW counter on and
	// off again leaves the count at one.
	e.emit(events.Event{Kind: events.CounterChange, Obj: v1, Counter: "VOW", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: v1, Counter: "VOW", Amount: -1})
	ce = vowEffect(t, e)
	if !e.attackBlocked(v1, 0, 0) {
		t.Fatal("a count dropping 2→1 forgot the card; only reaching zero must forget")
	}
	if len(ce.Remembered) != 3 {
		t.Fatalf("vow effect Remembered = %v, want all three still remembered", ce.Remembered)
	}

	// The count reaching zero forgets the card.
	e.emit(events.Event{Kind: events.CounterChange, Obj: v1, Counter: "VOW", Amount: -1})
	ce = vowEffect(t, e)
	if e.attackBlocked(v1, 0, 0) {
		t.Fatal("the un-vowed bear is still blocked after its last VOW counter left")
	}
	if len(ce.Remembered) != 2 {
		t.Fatalf("vow effect Remembered = %v, want [%d %d] after the forget", ce.Remembered, v0, v2)
	}

	// ForgetOnMoved$ Battlefield: the creature dying forgets it too.
	e.emit(events.Sacrifice(v2))
	if e.attackBlocked(v2, 0, 0) {
		t.Fatal("a dead vowed bear still blocks attacks")
	}
	ce = vowEffect(t, e)
	if len(ce.Remembered) != 1 || ce.Remembered[0] != v0 {
		t.Fatalf("vow effect Remembered = %v, want [%d] after the death forget", ce.Remembered, v0)
	}
	replayCheck(t, e, cfg)
}
