package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// trig:DamageAll ("Whenever one or more <sources> deal damage to one or more
// <targets>") is the batch-level damage trigger Forge's
// GameAction.triggerDamageAll fires ONCE per damage batch if at least one
// matching source dealt damage to at least one matching target. Before this
// mode existed the trigger never fired at all (it was unregistered), so
// Contaminant Grafter's proliferate and the rest of the 8-card corpus family
// were dead.
//
// The batch face is pinned on the REAL corpus script: Contaminant Grafter's
// T:Mode$ DamageAll | CombatDamage$ True | ValidSource$ Creature.YouCtrl |
// ValidTarget$ Player | Execute$ TrigProliferate, with two attackers dealing
// combat damage to a player in ONE damage step. The mode must fire the
// proliferate ability exactly ONCE for the whole batch -- not once per Damage
// event -- which a counter carrier makes observable. The non-combat face is
// pinned on the same real card: its CombatDamage$ True requires the
// engine-side combatDamaging flag, so a creature's own ability dealing
// non-combat damage must not fire it.

// TestDamageAllFiresOncePerBatchOnRealCorpusScript is the brief's headline
// case. Two creatures you control (Contaminant Grafter itself and a Hill
// Giant) attack unblocked, so TWO Damage events land in the one combat damage
// batch; the "one or more" reading is ONE proliferate, which adds exactly one
// +1/+1 counter to a counter-bearing carrier. The old per-event reading would
// pose two proliferate asks and add two counters.
func TestDamageAllFiresOncePerBatchOnRealCorpusScript(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg,
		[]string{"Contaminant Grafter", "Hill Giant"},
		[]string{"Name:Counter Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"},
		nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	grafter := findBattlefield(t, e, 0, "Contaminant Grafter", 0)
	giant := findBattlefield(t, e, 0, "Hill Giant", 0)
	carrier := findBattlefield(t, e, 0, "Counter Bear", 0)

	// Precondition: the carrier really carries a counter (a proliferate with
	// no counter-bearing permanent is a no-op, so the assertion below would
	// pass vacuously without this).
	e.emit(events.Event{Kind: events.CounterChange, Obj: carrier, Counter: "P1P1", Amount: 2})
	if got := e.G.Obj(carrier).Counter("P1P1"); got != 2 {
		t.Fatalf("precondition: carrier P1P1 = %d, want 2", got)
	}

	e.askAttackers()
	submitAttackers(t, e, grafter, giant)
	drainCombatDamagePriority(t, e)

	// Both attackers connected (the mode needs at least one matching source
	// AND one matching target in the batch, so a silent attack would make the
	// assertion vacuous).
	if got := e.G.Players[1].Life; got != 12 {
		t.Fatalf("precondition: defender life = %d, want 12 (5 + 3 combat damage landed)", got)
	}

	// Exactly ONE proliferate ask for the whole damage batch.
	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "proliferate" {
		t.Fatalf("pending = %+v, want the single DamageAll proliferate ask", d)
	}
	carrierOpt := -1
	for i, o := range d.Options {
		if o.Obj == carrier {
			carrierOpt = i
		}
	}
	if carrierOpt < 0 {
		t.Fatalf("counter carrier not offered to proliferate: %+v", d.Options)
	}
	answerProliferate(t, e, d, carrierOpt)
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(carrier).Counter("P1P1"); got != 3 {
		t.Fatalf("carrier P1P1 = %d, want 3 (the batch fired the trigger ONCE: two attackers, one proliferate)", got)
	}
	if n := countCounterChanges(e, carrier, "P1P1", 1); n != 1 {
		t.Fatalf("logged %d CounterChange(P1P1, +1) on the carrier, want 1 (one batch instance)", n)
	}
	replayCheck(t, e, cfg)
}

// damageAllBoard4 builds a four-seat combat board whose first seat owns the
// named corpus cards on the battlefield (summoning sickness cleared); every
// other seat gets the same treatment for the cards named for it. The fill is
// Mountains, exactly like combatTriggerBoard.
func damageAllBoard4(t *testing.T, reg *cards.Registry, seats ...[]string) (*Engine, Config) {
	t.Helper()
	var decks [][]*cards.Card
	for p := 0; p < 4; p++ {
		var deck []*cards.Card
		if p < len(seats) {
			for _, name := range seats[p] {
				deck = append(deck, mustCorpusCard(t, reg, name))
			}
		}
		deck = append(deck, mountainDeck(t, 40-len(deck))...)
		decks = append(decks, deck)
	}
	cfg := seatZeroStart(Config{Seed: 73, Names: []string{"a", "b", "c", "d"},
		Tokens: reg.Tokens, Decks: decks})
	e := New(cfg)
	for p := 0; p < 4; p++ {
		n := 0
		if p < len(seats) {
			n = len(seats[p])
		}
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner != state.PlayerID(p) {
				continue
			}
			for _, c := range decks[p][:n] {
				if o.Card == c {
					e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
					break
				}
			}
		}
	}
	// Only seat 0 ever attacks in these fixtures, so only its creatures get
	// the pre-combat sickness clear the replay cannot see -- the same test
	// setup mutation combatTriggerBoard makes; the other seats' sickness
	// stays an event-folded fact on both sides of replayCheck.
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Zone == state.ZBattlefield && o.Owner == state.PlayerID(0) &&
			o.Face() != nil && o.Face().IsCreature() {
			o.SummonSick = false
		}
	}
	return e, cfg
}

// splitAttack answers the KAttackers ask with one (attacker name, defender)
// pair per entry, then passes the CR 508.2/509.6 priority windows so the
// declaration commits.
// dmgAllPair is one (attacker name, defender seat) pair of a split attack.
type dmgAllPair struct {
	name string
	def  state.PlayerID
}

func splitAttack(t *testing.T, e *Engine, pairs ...dmgAllPair) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	var choices []int
	for _, pair := range pairs {
		found := false
		for _, o := range d.Options {
			if o.Kind == "attacker" && o.Player == pair.def && o.Obj != 0 &&
				e.G.Obj(o.Obj).Face().Name == pair.name {
				choices = append(choices, o.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no attacker option for %q at seat %d in %+v", pair.name, pair.def, d.Options)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit split attack %v: %v", choices, err)
	}
	drainCombatPriority(t, e)
}

// combatTokenCount counts seat p's battlefield tokens named name.
func combatTokenCount(e *Engine, p state.PlayerID, name string) int {
	n := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Owner == p && o.IsToken &&
			o.Face() != nil && o.Face().Name == name {
			n++
		}
	}
	return n
}

// countTokenCreations counts the TokenCreate events that minted a token for
// owner p. effToken mints ONE event per token (TokenAmount$ X is N events),
// so this is a mint counter, not a resolution counter -- the latch itself is
// pinned on pendingTriggers by the synthetic test below.
func countTokenCreations(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Player == p {
			n++
		}
	}
	return n
}

// countPlayerDraws/are the shared event counters the three batch-set tests
// assert through.
func countPlayerDraws(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

func countPlayerDiscards(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if events.IsDiscard(ev) && ev.Player == p {
			n++
		}
	}
	return n
}

// answerDrain answers the named decision kinds a drain crosses and passes
// everything else, returning the first decision that matches none of the
// handlers (nil when the game settles).
func answerDrain(t *testing.T, e *Engine, limit int, on func(t *testing.T, e *Engine, d *decision.Decision) bool) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		if on != nil && on(t, e, d) {
			continue
		}
		return d
	}
	t.Fatalf("drain did not settle within %d rounds", limit)
	return nil
}

// passPriorityAll passes every KPriority decision, one seat at a time.
func passPriorityAll(t *testing.T, e *Engine) bool {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		return false
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "pass" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("priority decision with no pass option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit pass: %v", err)
	}
	return true
}

// TestDamageAllBatchTargetSetCountsMatchingPlayersOnRealCorpusScript pins the
// batch TARGET set on Malcolm, Keen-Eyed Navigator's real script
// (SVar:X:TriggeredPlayersTargets$Amount feeding DB$ Token | TokenAmount$ X)
// through the REAL combat pipeline: a split attack -- one Pirate at each of
// TWO different opponents in ONE combat damage pass -- creates exactly TWO
// Treasures ("for each opponent dealt damage"), and the dedup control leg
// sends BOTH pirates at ONE opponent: exactly ONE treasure, the batch counts
// OPPONENTS, not damage events (a per-event firing would mint two).
func TestDamageAllBatchTargetSetCountsMatchingPlayersOnRealCorpusScript(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, leg := range []struct {
		name     string
		wantTok  int
		pairs    []dmgAllPair
		wantLife map[state.PlayerID]int32
	}{
		{
			name:     "split across two opponents",
			wantTok:  2,
			pairs:    []dmgAllPair{{"Malcolm, Keen-Eyed Navigator", 1}, {"Kitesail Corsair", 2}},
			wantLife: map[state.PlayerID]int32{1: 18, 2: 18},
		},
		{
			name:     "both at one opponent",
			wantTok:  1,
			pairs:    []dmgAllPair{{"Malcolm, Keen-Eyed Navigator", 1}, {"Kitesail Corsair", 1}},
			wantLife: map[state.PlayerID]int32{1: 16},
		},
	} {
		t.Run(leg.name, func(t *testing.T) {
			e, cfg := damageAllBoard4(t, reg,
				[]string{"Malcolm, Keen-Eyed Navigator", "Kitesail Corsair"},
				[]string{"Grizzly Bears"}, []string{"Grizzly Bears"}, []string{"Grizzly Bears"})
			// Precondition: the trigger carrier is on the battlefield with its
			// DamageAll line, so a silent scan cannot make the legs vacuous.
			malcolm := findBattlefield(t, e, 0, "Malcolm, Keen-Eyed Navigator", 0)
			if f := e.G.Obj(malcolm).Face(); f == nil || len(f.Triggers) == 0 || f.Triggers[0].Mode != "DamageAll" {
				t.Fatalf("precondition: Malcolm face has no DamageAll trigger: %+v", f)
			}
			e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
			e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
			e.askAttackers()
			splitAttack(t, e, leg.pairs...)
			for i := 0; i < 40; i++ {
				if !passPriorityAll(t, e) {
					break
				}
				if e.G.Step == state.StepMain2 {
					break
				}
			}
			// Precondition: the declared damage really landed where the pairs
			// sent it -- a silent attack would make the treasure count vacuous.
			for def, want := range leg.wantLife {
				if got := e.G.Players[def].Life; got != want {
					t.Fatalf("precondition: seat %d life = %d, want %d (the declared combat damage)", def, got, want)
				}
			}
			if got := combatTokenCount(e, 0, "Treasure Token"); got != leg.wantTok {
				t.Fatalf("Treasure tokens = %d, want %d (X = the count of opponents dealt damage this way)", got, leg.wantTok)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestDamageAllLatchQueuesOneInstanceAcrossDifferentTargets pins the
// line-only batch latch on the same real script, at the queue point where a
// regression is directly observable: TWO matching Damage events to TWO
// DIFFERENT opponents inside ONE batch bracket (the shape a split attack
// deals) must queue exactly ONE DamageAll instance -- the "one or more"
// reading is one trigger per batch, not one per (line, target) pair. The
// resolved face is then pinned too: the single instance's batch target set
// has both opponents, so TokenAmount$ X mints both Treasures.
func TestDamageAllLatchQueuesOneInstanceAcrossDifferentTargets(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := damageAllBoard4(t, reg,
		[]string{"Malcolm, Keen-Eyed Navigator", "Kitesail Corsair"},
		[]string{"Grizzly Bears"}, []string{"Grizzly Bears"}, []string{"Grizzly Bears"})
	malcolm := findBattlefield(t, e, 0, "Malcolm, Keen-Eyed Navigator", 0)
	if f := e.G.Obj(malcolm).Face(); f == nil || len(f.Triggers) == 0 || f.Triggers[0].Mode != "DamageAll" {
		t.Fatalf("precondition: Malcolm face has no DamageAll trigger: %+v", f)
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	// The synthetic combat-shaped batch: both events carry the engine-side
	// combatDamaging flag and e.damaging the SAME dealer (Malcolm is itself a
	// Pirate), the shape TestDamageAllNonCombatDamageDoesNotFire's positive
	// control uses, bracketed into ONE batch.
	e.damaging = malcolm
	e.combatDamaging = true
	e.BeginDamageBatch()
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.emit(events.Event{Kind: events.Damage, Player: 2, Amount: 2})
	e.EndDamageBatch()
	e.combatDamaging = false
	e.damaging = 0
	// Precondition: both hits landed.
	if e.G.Players[1].Life != 18 || e.G.Players[2].Life != 18 {
		t.Fatalf("precondition: life 1=%d 2=%d, want 18/18 (both Damage events landed)",
			e.G.Players[1].Life, e.G.Players[2].Life)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("two-target batch queued %d DamageAll instances, want 1 (the line-only latch; a per-target key queues 2)", len(e.pendingTriggers))
	}
	// Drive the engine's own continuation (the synthetic fixture's
	// equivalent of the combat pass's priority machinery): the drain pushes
	// the queued trigger and grants priority, and the stack drain resolves it.
	e.resumeTriggerDrain()
	passUntilStackEmpty(t, e, 60)
	if got := combatTokenCount(e, 0, "Treasure Token"); got != 2 {
		t.Fatalf("Treasure tokens = %d, want 2 (the single instance read both opponents off the batch target set)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDamageAllNonCombatDamageDoesNotFire pins the CombatDamage$ half on the
// same real card, on the shape TestCombatDamageFlagDrivesTheCombatDamageGate
// uses: an identical Damage event fires Contaminant Grafter's DamageAll
// trigger in the combat state (the engine-side combatDamaging flag
// dealCombatDamage sets) and does NOT in the non-combat state. The combat
// leg is also a positive control -- it proves the mode is registered and
// queued, so this test cannot pass vacuously against an unregistered mode.
func TestDamageAllNonCombatDamageDoesNotFire(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg,
		[]string{"Contaminant Grafter"},
		[]string{"Name:Pinger\nManaCost:1 R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"},
		nil, nil)
	grafter := findBattlefield(t, e, 0, "Contaminant Grafter", 0)
	// A logged TurnChange/StepChange is the replay-consistent way to clear the
	// fixtures' summoning sickness and park the clock (the flag test's shape).
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	// Precondition: the trigger's carrier is really on the battlefield with
	// its DamageAll line, so a silent scan cannot make the legs vacuous.
	if f := e.G.Obj(grafter).Face(); f == nil || len(f.Triggers) == 0 || f.Triggers[0].Mode != "DamageAll" {
		t.Fatalf("precondition: grafter face = %+v, want a DamageAll trigger", f)
	}
	pinger := findBattlefield(t, e, 0, "Pinger", 0)

	// Combat leg: the flag set and e.damaging the pinger -- the state
	// dealCombatDamage runs every assignment's emit under. The pinger is a
	// creature you control, so ValidSource$ matches and the PLAYER recipient
	// matches ValidTarget$.
	e.damaging = pinger
	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	e.combatDamaging = false
	e.damaging = 0
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("combat-state Damage queued %d DamageAll triggers, want 1 (the positive control)", len(e.pendingTriggers))
	}
	e.pendingTriggers = nil

	// Non-combat leg: the identical event shape with the flag unset (a
	// creature's own ability dealing damage). It must not queue the
	// CombatDamage$ True trigger.
	e.damaging = pinger
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	e.damaging = 0
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("non-combat Damage queued %d DamageAll triggers, want 0 (CombatDamage$ True)", len(e.pendingTriggers))
	}
	replayCheck(t, e, cfg)
}

// faceDamageAllTrigger reports whether o's face carries a Mode$ DamageAll
// trigger line -- the precondition every batch-set test asserts so a silent
// scan cannot make it vacuous.
func faceDamageAllTrigger(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	for i := range f.Triggers {
		if f.Triggers[i].Mode == "DamageAll" {
			return true
		}
	}
	return false
}

// TestDamageAllTargetSetDrivesHordewingSkaabDrawCount pins the count head
// end to end on Hordewing Skaab's real script: SVar:X:TriggeredPlayersTargets$Amount
// feeds BOTH halves of its body (Cost$ Draw<X/You> and NumCards$ X), so the
// number of opponents dealt combat damage in one batch sizes the
// draw-then-discard loot. A split attack at two opponents draws and discards
// TWO; a single-opponent attack draws and discards ONE. The window must
// offer a real "pay" -- an unresolvable X lands decline-only and the card
// does nothing (the pre-fix reading).
func TestDamageAllTargetSetDrivesHordewingSkaabDrawCount(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, leg := range []struct {
		name        string
		pairs       []dmgAllPair
		wantLife    map[state.PlayerID]int32
		wantDraw    int
		wantDiscard int
	}{
		{
			name:        "two opponents dealt damage",
			pairs:       []dmgAllPair{{"Hordewing Skaab", 1}, {"Gravecrawler", 2}},
			wantLife:    map[state.PlayerID]int32{1: 17, 2: 18},
			wantDraw:    2,
			wantDiscard: 2,
		},
		{
			name:        "one opponent dealt damage",
			pairs:       []dmgAllPair{{"Hordewing Skaab", 1}},
			wantLife:    map[state.PlayerID]int32{1: 17},
			wantDraw:    1,
			wantDiscard: 1,
		},
	} {
		t.Run(leg.name, func(t *testing.T) {
			e, cfg := damageAllBoard4(t, reg,
				[]string{"Hordewing Skaab", "Gravecrawler"},
				[]string{"Grizzly Bears"}, []string{"Grizzly Bears"}, []string{"Grizzly Bears"})
			// Precondition: the carrier is out with its DamageAll line.
			horde := findBattlefield(t, e, 0, "Hordewing Skaab", 0)
			if !faceDamageAllTrigger(e.G.Obj(horde)) {
				t.Fatalf("precondition: Hordewing Skaab face has no DamageAll trigger")
			}
			// Baseline before combat: the log carries every seat's
			// opening-hand draws, so the assertions compare deltas.
			drawsBefore := countPlayerDraws(e, 0)
			e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
			e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
			e.askAttackers()
			splitAttack(t, e, leg.pairs...)
			for i := 0; i < 60 && e.G.Step != state.StepMain2; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatalf("no decision mid-combat (step %v)", e.G.Step)
				}
				switch d.Kind {
				case decision.KPriority:
					passPriorityAll(t, e)
				case decision.KTriggerOptional:
					// The OptionalDecider$ You election: accept.
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
						t.Fatalf("submit trigger election: %v", err)
					}
				case decision.KChoose:
					pay := -1
					for _, o := range d.Options {
						if o.Kind == "trigger_cost_pay" {
							pay = o.Index
						}
					}
					if pay < 0 {
						t.Fatalf("trigger-cost window offers no pay (X unresolvable?): %+v", d.Options)
					}
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pay}}); err != nil {
						t.Fatalf("submit draw-cost pay: %v", err)
					}
				case decision.KModes:
					// The Mode$ TgtChoose discard ask: Min == Max == X, the
					// count head's answer made observable on the wire.
					if int(d.Min) != leg.wantDiscard || int(d.Max) != leg.wantDiscard {
						t.Fatalf("discard ask Min=%d Max=%d, want %d (X = opponents dealt damage)", d.Min, d.Max, leg.wantDiscard)
					}
					ch := make([]int, 0, d.Min)
					for k := 0; k < d.Min; k++ {
						ch = append(ch, int(k))
					}
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
						t.Fatalf("submit discard: %v", err)
					}
				default:
					t.Fatalf("unexpected %v decision during the Hordewing drain: %+v", d.Kind, d)
				}
			}
			// Precondition: the declared damage landed.
			for def, want := range leg.wantLife {
				if got := e.G.Players[def].Life; got != want {
					t.Fatalf("precondition: seat %d life = %d, want %d", def, got, want)
				}
			}
			if got := countPlayerDraws(e, 0) - drawsBefore; got != leg.wantDraw {
				t.Fatalf("Draw events for seat 0 = %d, want %d (X = opponents dealt damage)", got, leg.wantDraw)
			}
			if got := countPlayerDiscards(e, 0); got != leg.wantDiscard {
				t.Fatalf("discard events for seat 0 = %d, want %d (NumCards$ X)", got, leg.wantDiscard)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestDamageAllBatchSourceControllersDrawOnRealCorpusScript pins the batch
// SOURCE set on Nelly Borca, Impulsive Accuser's real script: bodies reading
// "Defined$ TriggeredSourcesController & You" ("you and the controller of
// those creatures each draw a card") resolve the controllers of EVERY source
// the batch matched, deduplicated -- two creatures under two DIFFERENT
// opponents' control in one batch draw for three players (Nelly's controller
// plus both controllers); two hits from creatures under ONE controller draw
// for two (the dedup); a single hit draws for two.
func TestDamageAllBatchSourceControllersDrawOnRealCorpusScript(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, leg := range []struct {
		name      string
		sources   []state.PlayerID // controllers of the dealing creatures
		events    int              // matching Damage events in the batch
		wantDraws map[state.PlayerID]int
	}{
		{
			name:      "two controllers",
			sources:   []state.PlayerID{1, 2},
			events:    2,
			wantDraws: map[state.PlayerID]int{0: 1, 1: 1, 2: 1, 3: 0},
		},
		{
			name:      "one controller, two hits",
			sources:   []state.PlayerID{1},
			events:    2,
			wantDraws: map[state.PlayerID]int{0: 1, 1: 1, 2: 0, 3: 0},
		},
		{
			name:      "one source, one hit",
			sources:   []state.PlayerID{1},
			events:    1,
			wantDraws: map[state.PlayerID]int{0: 1, 1: 1, 2: 0, 3: 0},
		},
	} {
		t.Run(leg.name, func(t *testing.T) {
			e, cfg := damageAllBoard4(t, reg,
				[]string{"Nelly Borca, Impulsive Accuser"},
				[]string{"Grizzly Bears"}, []string{"Grizzly Bears"}, []string{"Grizzly Bears"})
			// Precondition: Nelly is out with her DamageAll line.
			nelly := findBattlefield(t, e, 0, "Nelly Borca, Impulsive Accuser", 0)
			if !faceDamageAllTrigger(e.G.Obj(nelly)) {
				t.Fatalf("precondition: Nelly Borca face has no DamageAll trigger")
			}
			e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
			e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
			bear := func(p state.PlayerID) state.ObjID {
				return findBattlefield(t, e, p, "Grizzly Bears", 0)
			}
			// Baseline before the batch: the log carries every seat's
			// opening-hand draws, so the assertions compare deltas.
			before := make(map[state.PlayerID]int, 4)
			for p := state.PlayerID(0); p < 4; p++ {
				before[p] = countPlayerDraws(e, p)
			}
			e.combatDamaging = true
			e.BeginDamageBatch()
			for k := 0; k < leg.events; k++ {
				// The creatures alternate so a two-event leg under one
				// controller still carries two distinct source objects.
				e.damaging = bear(leg.sources[k%len(leg.sources)])
				e.emit(events.Event{Kind: events.Damage, Player: 3, Amount: 1})
			}
			e.damaging = 0
			e.EndDamageBatch()
			e.combatDamaging = false
			// Precondition: the damage landed on the shared opponent.
			if e.G.Players[3].Life != int32(20-leg.events) {
				t.Fatalf("precondition: seat 3 life = %d, want %d", e.G.Players[3].Life, 20-leg.events)
			}
			if len(e.pendingTriggers) != 1 {
				t.Fatalf("%d-event batch queued %d DamageAll instances, want 1", leg.events, len(e.pendingTriggers))
			}
			e.resumeTriggerDrain()
			passUntilStackEmpty(t, e, 60)
			for p, want := range leg.wantDraws {
				if got := countPlayerDraws(e, p) - before[p]; got != want {
					t.Fatalf("Draw events for seat %d = %d, want %d (TriggeredSourcesController & You)", p, got, want)
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestDamageAllTriggeredTargetsDrivesBreechesExileTop pins the third reader
// on Breeches, Brazen Plunderer's real script: its body is
// "DB$ Dig | DigNum$ 1 | ChangeNum$ All | Defined$ TriggeredTargets |
// DestinationZone$ Exile" -- "exile the top card of each of those opponents'
// libraries" -- so the batch's matching TARGET set is the dig's Defined$
// list. A split attack at two opponents exiles one library-top card from
// EACH of them (two library->exile moves) and nothing from the untouched
// opponent; the control leg that sends both pirates at one opponent exiles
// exactly one card, from that opponent only.
func TestDamageAllTriggeredTargetsDrivesBreechesExileTop(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, leg := range []struct {
		name      string
		pairs     []dmgAllPair
		wantMoves map[state.PlayerID]int
	}{
		{
			name:      "split across two opponents",
			pairs:     []dmgAllPair{{"Breeches, Brazen Plunderer", 1}, {"Kitesail Corsair", 2}},
			wantMoves: map[state.PlayerID]int{1: 1, 2: 1, 3: 0},
		},
		{
			name:      "both at one opponent",
			pairs:     []dmgAllPair{{"Breeches, Brazen Plunderer", 1}, {"Kitesail Corsair", 1}},
			wantMoves: map[state.PlayerID]int{1: 1, 2: 0, 3: 0},
		},
	} {
		t.Run(leg.name, func(t *testing.T) {
			e, cfg := damageAllBoard4(t, reg,
				[]string{"Breeches, Brazen Plunderer", "Kitesail Corsair"},
				[]string{"Grizzly Bears"}, []string{"Grizzly Bears"}, []string{"Grizzly Bears"})
			// Precondition: the carrier is out with its DamageAll line.
			if !faceDamageAllTrigger(e.G.Obj(findBattlefield(t, e, 0, "Breeches, Brazen Plunderer", 0))) {
				t.Fatalf("precondition: Breeches face has no DamageAll trigger")
			}
			e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
			e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
			e.askAttackers()
			splitAttack(t, e, leg.pairs...)
			// Breeches has no flying, so the defenders are asked to block;
			// they decline, and the rest of combat drains on passes.
			for i := 0; i < 60 && e.G.Step != state.StepMain2; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatalf("no decision mid-combat (step %v)", e.G.Step)
				}
				switch d.Kind {
				case decision.KPriority:
					passPriorityAll(t, e)
				case decision.KBlockers:
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
						t.Fatalf("submit empty blockers: %v", err)
					}
				default:
					t.Fatalf("unexpected %v decision during the Breeches drain: %+v", d.Kind, d)
				}
			}
			// Precondition: the declared damage landed (the mode needs a real
			// matching batch, and a silent attack would make the exile count
			// vacuous).
			for _, pair := range leg.pairs {
				if got := e.G.Players[pair.def].Life; got >= 20 {
					t.Fatalf("precondition: seat %d life = %d, want the declared combat damage", pair.def, got)
				}
			}
			for p, want := range leg.wantMoves {
				if got := countLibraryExiles(e, p); got != want {
					t.Fatalf("library->exile moves for seat %d = %d, want %d (Defined$ TriggeredTargets: one dig per matched opponent)", p, got, want)
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}

// countLibraryExiles counts the library->exile MoveZone events out of player
// p's library -- the Dig with DestinationZone$ Exile moves.
func countLibraryExiles(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZExile &&
			ev.Player == p {
			n++
		}
	}
	return n
}
