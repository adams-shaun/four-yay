package rules

// CR 702.148: the Ravenous keyword. Real corpus-card tests (the .cards corpus,
// never a committed .txt) for the 12-carrier population measured at task
// kw-ravenous; Jacked Rabbit is the pinned carrier because its script also
// reads Count$CardPower off the counters the keyword places, so the counter
// put and the attack-token half share one observable.
//
// What the keyword expands to (cards/kw_ravenous.go): an ETB trigger that
// puts X +1/+1 counters (X = the {X} the cast paid, Count$xPaid) and then, as
// a SubAbility$, draws a card gated on that same count with the shared
// SVar-condition engine (ConditionCheckSVar$ + ConditionSVarCompare$ GE5).
// The tests pin both halves of the oracle text: X=3 -> 3 counters and NO
// draw; X=5 -> 5 counters and one draw.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ravenousConfig seeds a 2-seat game whose seat 0 carries one real Jacked
// Rabbit plus 39 Mountains, drives to the first Main 1 (whichever seat won
// the toss -- the starting player is seed-dependent, so the fixture must not
// assume seat 0 is active) and moves that seat's Rabbit to hand. The
// engine/config/id triple mirrors etbConfig, but the card is the compiled
// corpus card (mustCorpusCard) rather than an inline fixture, which is what
// makes this a real-carrier test.
func ravenousConfig(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.PlayerID) {
	return corpusCardConfig(t, seed, "Jacked Rabbit")
}

// corpusCardConfig is ravenousConfig generalised to any corpus card by name:
// both seats carry the card plus Mountains so the seed-dependent turn-1
// active seat always has one, and it is bridged into that seat's hand at its
// first Main 1.
func corpusCardConfig(t *testing.T, seed uint64, name string) (*Engine, Config, state.ObjID, state.PlayerID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c := mustCorpusCard(t, reg, name)
	seatDeck := func() []*cards.Card {
		return append([]*cards.Card{c}, mountainDeck(t, 39)...)
	}
	// seatZeroStart pins the turn-1 active seat to 0 so the assertions do
	// not depend on the seed (the toss winner is uniform over the seats).
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{seatDeck(), seatDeck()}})
	e := New(cfg)
	e.Advance()
	// Drive to the first Main 1 of the turn-1 active seat (toMain1 is
	// idempotent and keys on e.G.Turn/e.G.Active), then bridge that seat's
	// card into its hand.
	toMain1(t, e)
	caster := e.G.Active
	id := findByName(e, name, caster)
	if id == 0 {
		t.Fatalf("corpus %q not found for seat %d -- corpus missing?", name, caster)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZHand})
	e.pending = nil
	e.priorityRound()
	return e, cfg, id, caster
}

// castRavenous casts the rabbit at the given X and returns the draw events
// observed while the stack drained.
func castRavenous(t *testing.T, e *Engine, id state.ObjID, x int) int {
	t.Helper()
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("X decision for the Ravenous cast: %+v", d)
	}
	submitChoices(t, e, x)
	before := countDraw(e)
	passUntilStackEmpty(t, e, 30)
	return countDraw(e) - before
}

// TestJackedRabbitRavenousX3CountersNoDraw is the brief's X=3 leg: the
// creature enters with 3 +1/+1 counters and does NOT draw (3 < 5).
func TestJackedRabbitRavenousX3CountersNoDraw(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := ravenousConfig(t, 131)
	addMana(t, e, caster, "WWWWW") // {X}{1}{W} with X=3 -> {4}{W}
	drawn := castRavenous(t, e, id, 3)
	o := e.G.Obj(id)
	// Precondition: the cast actually resolved the Rabbit onto the
	// battlefield. A silently-countered or still-in-hand Rabbit would make
	// the counter/draw assertions meaningless.
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Jacked Rabbit zone=%s, want battlefield", o.Zone)
	}
	if got := o.Counter("P1P1"); got != 3 {
		t.Fatalf("Ravenous X=3 put %d +1/+1 counters, want 3", got)
	}
	if e.Power(id) != 4 {
		t.Fatalf("Ravenous X=3 power = %d, want 4 (printed 1 + 3 counters)", e.Power(id))
	}
	if drawn != 0 {
		t.Fatalf("Ravenous X=3 drew %d cards, want 0 (the X>=5 gate must not fire)", drawn)
	}
	replayCheck(t, e, cfg)
}

// TestJackedRabbitRavenousX5CountersAndDraw is the brief's X=5 leg: 5
// +1/+1 counters AND the entry draw.
func TestJackedRabbitRavenousX5CountersAndDraw(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := ravenousConfig(t, 132)
	addMana(t, e, caster, "WWWWWWW") // {X}{1}{W} with X=5 -> {6}{W}
	drawn := castRavenous(t, e, id, 5)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Jacked Rabbit zone=%s, want battlefield", o.Zone)
	}
	if got := o.Counter("P1P1"); got != 5 {
		t.Fatalf("Ravenous X=5 put %d +1/+1 counters, want 5", got)
	}
	if e.Power(id) != 6 {
		t.Fatalf("Ravenous X=5 power = %d, want 6 (printed 1 + 5 counters)", e.Power(id))
	}
	if drawn != 1 {
		t.Fatalf("Ravenous X=5 drew %d cards, want exactly 1 (the X>=5 gate)", drawn)
	}
	replayCheck(t, e, cfg)
}

// TestJackedRabbitRavenousAttackTokensReadTheCounters proves the two halves
// of the card interact: the attack trigger's TokenAmount$ Count$CardPower
// reads the power the Ravenous counters raised, so an X=3 Rabbit attacks and
// makes 4 Rabbits -- not the 1 it would make with no counters.
func TestJackedRabbitRavenousAttackTokensReadTheCounters(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := ravenousConfig(t, 133)
	addMana(t, e, caster, "WWWWW")
	if drawn := castRavenous(t, e, id, 3); drawn != 0 {
		t.Fatalf("precondition failed: X=3 drew %d cards, want 0", drawn)
	}
	if e.G.Obj(id).Counter("P1P1") != 3 {
		t.Fatalf("precondition failed: Ravenous counters = %d, want 3", e.G.Obj(id).Counter("P1P1"))
	}
	// Advance into seat 0's combat and declare it as an attacker, then let
	// the attack trigger resolve.
	driveToStep(t, e, e.G.Turn, caster, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackerAt(t, e, id, 1-caster)
	before := countTokenCreate(e)
	passUntilStackEmpty(t, e, 40)
	made := countTokenCreate(e) - before
	if made != 4 {
		t.Fatalf("attack trigger minted %d tokens, want 4 (power 4, one per point of power)", made)
	}
	replayCheck(t, e, cfg)
}

// countTokenCreate counts the whole-table TokenCreate events, the observable
// the attack trigger's TokenAmount$ Count$CardPower drives.
func countTokenCreate(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate {
			n++
		}
	}
	return n
}

// drainRavenous drains the stack to empty, answering any simultaneous-ETB
// trigger_order decision (CR 603.3b: two triggers controlled by the same
// player are ordered by that player). passUntilStackEmpty fatals on any
// non-priority decision, and a trigger_order is posed BEFORE the triggers
// reach the stack (so the stack can even be empty), which is why the plain
// drain returns with the triggers unresolved for a carrier that has its own
// ETB trigger beside Ravenous. Option 0 is a legal order; the tests assert
// the outcome, which is order-independent for these shapes.
func drainRavenous(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for n := 0; n < limit && !e.G.Over; n++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Kind == decision.KPriority && len(e.G.Stack) == 0 {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		case decision.KTriggerOrder:
			// CR 603.3b: submit every offered trigger, in the offered
			// order (a legal order). Min==Max==len(Options), so the answer
			// must name all of them.
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if len(idx) == 0 {
				t.Fatalf("empty trigger_order decision")
			}
			submitChoices(t, e, idx...)
		case decision.KChoose: // the X ask, or another mid-resolution pick
			if len(d.Options) == 0 {
				t.Fatalf("empty %s decision while draining: %+v", d.ResumeKind, d)
			}
			submitChoices(t, e, d.Options[0].Index)
		case decision.KTarget:
			// A carrier's own ETB trigger asking (Zoanthrope's Warp Blast
			// "deals X damage to any target"): answer option 0. The tests
			// assert the Ravenous outcome, not the damage.
			if len(d.Options) == 0 {
				t.Fatalf("empty target decision while draining")
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unexpected %s decision while draining: %+v", d.Kind, d)
		}
	}
}

// TestExocrineRavenousWithItsOwnEtbTrigger pins the multi-ETB carrier shape:
// Exocrine carries K:Ravenous AND its own "when CARDNAME enters, it deals X
// damage to each player and each other creature" trigger, so an X=5 cast
// queues both ETB triggers. The Ravenous half must still place 5 counters and
// draw (the two simultaneous triggers must resolve without wedging), and the
// card's own trigger must deal 5 to each player.
func TestExocrineRavenousWithItsOwnEtbTrigger(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := corpusCardConfig(t, 134, "Exocrine")
	addMana(t, e, caster, "RRRRRRRR") // {X}{2}{R} with X=5 -> {7}{R}
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("X decision for the Exocrine cast: %+v", d)
	}
	submitChoices(t, e, 5)
	before := countDraw(e)
	lifeBefore := []int32{e.G.Players[0].Life, e.G.Players[1].Life}
	drainRavenous(t, e, 60)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Exocrine zone=%s, want battlefield (two ETB triggers must not wedge)", o.Zone)
	}
	if got := o.Counter("P1P1"); got != 5 {
		t.Fatalf("Exocrine Ravenous X=5 put %d +1/+1 counters, want 5", got)
	}
	if got := countDraw(e) - before; got != 1 {
		t.Fatalf("Exocrine Ravenous X=5 drew %d cards, want 1", got)
	}
	for p := 0; p < 2; p++ {
		if got := e.G.Players[p].Life; got != lifeBefore[p]-5 {
			t.Fatalf("seat %d life = %d, want %d (Exocrine's own trigger dealt 5)", p, got, lifeBefore[p]-5)
		}
	}
	replayCheck(t, e, cfg)
}

// TestZoanthropeRavenousZeroToughnessCarrierSurvives is the load-bearing
// shape test: Zoanthrope is a 0/0 with K:Ravenous (the only such carrier),
// so if Ravenous put its counters with a triggered ability the CR 704.5f
// zero-toughness state-based action would kill it before the trigger could
// resolve. The counters are an enters-with replacement (cards/kw_ravenous.go),
// so the permanent is already a 2/2 when the SBA would look. X=2 -> enters as
// a 2/2 with 2 counters and no draw (2 < 5); X=5 -> a 5/5 with 5 counters and
// one draw.
func TestZoanthropeRavenousZeroToughnessCarrierSurvives(t *testing.T) {
	t.Parallel()
	e, _, id, caster := corpusCardConfig(t, 141, "Zoanthrope")
	addMana(t, e, caster, "UURR") // {X}{U}{R} with X=2 -> {2}{U}{R}
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("X decision for the Zoanthrope cast: %+v", d)
	}
	submitChoices(t, e, 2)
	before := countDraw(e)
	drainRavenous(t, e, 60)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: 0/0 Zoanthrope zone=%s, want battlefield (counters must be an enters-with replacement, not a trigger)", o.Zone)
	}
	if got := o.Counter("P1P1"); got != 2 {
		t.Fatalf("Zoanthrope Ravenous X=2 put %d +1/+1 counters, want 2", got)
	}
	if e.Toughness(id) != 2 || e.Power(id) != 2 {
		t.Fatalf("Zoanthrope X=2 is %d/%d, want 2/2", e.Power(id), e.Toughness(id))
	}
	if got := countDraw(e) - before; got != 0 {
		t.Fatalf("Zoanthrope X=2 drew %d cards, want 0", got)
	}

	// X=5 leg on a fresh game: the same 0/0 must enter as a 5/5 and draw.
	e5, cfg5, id5, caster5 := corpusCardConfig(t, 142, "Zoanthrope")
	addMana(t, e5, caster5, "UUUURRR") // {X}{U}{R} with X=5 -> {6}{U}{R} (8 mana; WUBRGC units)
	d5 := e5.Pending()
	submitChoices(t, e5, castOptionFor(t, e5, id5).Index)
	d5 = e5.Pending()
	if d5 == nil || d5.Kind != decision.KChoose {
		t.Fatalf("X decision for the second Zoanthrope cast: %+v", d5)
	}
	submitChoices(t, e5, 5)
	before5 := countDraw(e5)
	drainRavenous(t, e5, 60)
	o5 := e5.G.Obj(id5)
	if o5.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: 0/0 Zoanthrope X=5 zone=%s, want battlefield", o5.Zone)
	}
	if got := o5.Counter("P1P1"); got != 5 {
		t.Fatalf("Zoanthrope Ravenous X=5 put %d +1/+1 counters, want 5", got)
	}
	if got := countDraw(e5) - before5; got != 1 {
		t.Fatalf("Zoanthrope X=5 drew %d cards, want 1", got)
	}
	replayCheck(t, e5, cfg5)
}
