package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task dponce1: Selfless Squire's two halves on the real corpus script —
// the ETB "prevent all damage that would be dealt to you this turn" bodyless
// Prevent$ replacement (DB$ Effect | ReplacementEffects$ RPrevent) and the
// Mode$ DamagePreventedOnce trigger that grows the Squire by the prevented
// amount (SVar X:TriggerCount$DamageAmount → DB$ PutCounter | CounterNum$ X).
// Both halves were dead: the bodyless Prevent$ shape fell to effEffect's
// loud-Note arm, and DamagePreventedOnce was an unregistered trigger mode
// with no stored prevention record to key on. The fixture style is
// combat_damage_trigger_test.go's: the real corpus card placed via a real
// MoveZone so the ETB trigger fires, and the damage seeded directly the way
// replacement_damage_counter_test.go's Spider-Punk tests do.

const damagePreventedOnceAggressor = "Name:Aggressor\nTypes:Creature\nPT:2/2\nOracle:x\n"

// squireTurn re-arms the parked combat clock for seat 0's next turn at
// main 1 (no attackers ask to answer): a logged TurnChange is both the
// replay-consistent way combatTriggerBoard's own eventless summoning-sickness
// clear becomes log-visible (the Jitte tests' device) and the turn the
// Squire's this-turn prevention lives on.
func squireTurn(t *testing.T, e *Engine) {
	t.Helper()
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
}

// drainTriggeredAbilities puts the engine's queued triggers on the stack and
// resolves them until the stack is empty, answering a same-controller
// trigger-order ask with the identity order (the queue's own deterministic
// order) and otherwise passing ordinary priority asks.
func drainTriggeredAbilities(t *testing.T, e *Engine) {
	t.Helper()
	e.putTriggersOnStack()
	for i := 0; i < 50 && (len(e.G.Stack) > 0 || len(e.pendingTriggers) > 0); i++ {
		if d := e.Pending(); d != nil {
			if d.Kind == decision.KTriggerOrder {
				ch := make([]int, d.Max)
				for j := range ch {
					ch[j] = j
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
					t.Fatalf("submit trigger order: %v", err)
				}
				continue
			}
			passUntilStackEmpty(t, e, 30)
			continue
		}
		if len(e.G.Stack) > 0 {
			e.resolveTop()
		} else {
			e.putTriggersOnStack()
		}
	}
}

// countP1P1Changes counts P1P1 CounterChange events on obj with the given
// per-emission amount.
func countP1P1Changes(e *Engine, obj state.ObjID, amount int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == obj && ev.Counter == "P1P1" && ev.Amount == amount {
			n++
		}
	}
	return n
}

// countPreventionNotes counts stored prevention Notes carrying the given
// amount (the full-prevention replacement arm's record).
func countPreventionNotes(e *Engine, amount int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Amount == amount &&
			strings.Contains(strings.ToLower(ev.Text), "prevent") {
			n++
		}
	}
	return n
}

// TestSelflessSquirePreventionGrowsIt: the ETB half prevents, and the trigger
// half grows the Squire by exactly the prevented amount. Two values of N
// (3 and 5) — a single-value pass would hide the silent-zero class bug where
// CounterNum$ X resolves to 0.
func TestSelflessSquirePreventionGrowsIt(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, n := range []int32{3, 5} {
		t.Run("prevent", func(t *testing.T) {
			e, cfg := combatTriggerBoard(t, reg, []string{"Selfless Squire"}, nil,
				nil, []string{damagePreventedOnceAggressor})
			squireTurn(t, e)
			squire := findBattlefield(t, e, 0, "Selfless Squire", 0)
			drainTriggeredAbilities(t, e)
			src := findBattlefield(t, e, 1, "Aggressor", 0)
			life0 := e.G.Players[0].Life
			e.damaging, e.combatDamaging = src, false
			e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: n})
			e.damaging, e.combatDamaging = 0, false
			drainTriggeredAbilities(t, e)
			if got := e.G.Players[0].Life; got != life0 {
				t.Fatalf("seat 0 life = %d, want %d: the ETB prevention half never fired", got, life0)
			}
			if c := countPreventionNotes(e, n); c != 1 {
				t.Fatalf("logged %d prevention Note(s) with amount %d, want 1", c, n)
			}
			if c := countP1P1Changes(e, squire, n); c != 1 {
				t.Fatalf("logged %d CounterChange(P1P1, +%d) on the Squire, want 1", c, n)
			}
			if got := e.G.Obj(squire).Counter("P1P1"); got != n {
				t.Fatalf("Squire P1P1 counters = %d, want %d", got, n)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestDamagePreventedOnceFiresPerPrevention: two SEPARATE prevented damage
// events in one turn → two firings, counters 3 then 2. DamagePreventedOnce's
// "Once" is per prevention (one Damage event, one occurrence), NOT a
// once-per-turn latch — combat batches damage but prevention does not.
func TestDamagePreventedOnceFiresPerPrevention(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Selfless Squire"}, nil,
		nil, []string{damagePreventedOnceAggressor})
	squireTurn(t, e)
	squire := findBattlefield(t, e, 0, "Selfless Squire", 0)
	drainTriggeredAbilities(t, e)
	src := findBattlefield(t, e, 1, "Aggressor", 0)
	life0 := e.G.Players[0].Life
	e.damaging, e.combatDamaging = src, false
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
	e.damaging, e.combatDamaging = 0, false
	drainTriggeredAbilities(t, e)
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 life = %d, want %d", got, life0)
	}
	if c := countPreventionNotes(e, 3); c != 1 || countPreventionNotes(e, 2) != 1 {
		t.Fatalf("prevention Notes: amount 3 x%d, amount 2 x%d, want one of each",
			countPreventionNotes(e, 3), countPreventionNotes(e, 2))
	}
	if c := countP1P1Changes(e, squire, 3); c != 1 || countP1P1Changes(e, squire, 2) != 1 {
		t.Fatalf("CounterChanges: +3 x%d, +2 x%d, want one of each (two firings)",
			countP1P1Changes(e, squire, 3), countP1P1Changes(e, squire, 2))
	}
	if got := e.G.Obj(squire).Counter("P1P1"); got != 5 {
		t.Fatalf("Squire P1P1 counters = %d, want 5 (3 + 2)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDamagePreventedOnceValidTargetYouOnly: the ValidTarget$ You gate —
// prevention to seat 1 fires seat 1's Squire and never seat 0's (whose
// replacement also cannot prevent seat 1's damage).
func TestDamagePreventedOnceValidTargetYouOnly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Selfless Squire"}, nil,
		[]string{"Selfless Squire"}, []string{damagePreventedOnceAggressor})
	squireTurn(t, e)
	squire0 := findBattlefield(t, e, 0, "Selfless Squire", 0)
	squire1 := findBattlefield(t, e, 1, "Selfless Squire", 0)
	drainTriggeredAbilities(t, e)
	src := findBattlefield(t, e, 1, "Aggressor", 0)
	life1 := e.G.Players[1].Life
	e.damaging, e.combatDamaging = src, false
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
	e.damaging, e.combatDamaging = 0, false
	drainTriggeredAbilities(t, e)
	if got := e.G.Players[1].Life; got != life1 {
		t.Fatalf("seat 1 life = %d, want %d", got, life1)
	}
	if c := countP1P1Changes(e, squire0, 3); c != 0 {
		t.Fatalf("seat 0's Squire logged %d CounterChange(P1P1, +3), want 0: damage to seat 1 is not ValidTarget$ You for it", c)
	}
	if c := countP1P1Changes(e, squire1, 3); c != 1 {
		t.Fatalf("seat 1's Squire logged %d CounterChange(P1P1, +3), want 1", c)
	}
	if got := e.G.Obj(squire0).Counter("P1P1"); got != 0 {
		t.Fatalf("seat 0's Squire has %d P1P1 counters, want 0", got)
	}
	if got := e.G.Obj(squire1).Counter("P1P1"); got != 3 {
		t.Fatalf("seat 1's Squire has %d P1P1 counters, want 3", got)
	}
	replayCheck(t, e, cfg)
}

// pickOption submits the KReplacement answer whose option names obj.
func pickOption(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("pending = %+v, want a CR 616.1 replacement-order choice", d)
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("replacement-order options %+v do not offer object %d", d.Options, obj)
}

// TestSelflessSquireOrderedAfterPartialPrevention (r2 finding 1): Squire +
// Thunderstaff, combat damage 3 to the controller, order answer =
// Thunderstaff first. CR 616.1e recomputes applicability after each
// modification, so the recomputed candidate set must STILL hold the Squire's
// bodyless "prevent all": Thunderstaff prevents 1, the Squire prevents the
// remaining 2, life is untouched and the Squire grows +1 then +2 = +3.
func TestSelflessSquireOrderedAfterPartialPrevention(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Selfless Squire", "Thunderstaff"}, nil,
		nil, []string{damagePreventedOnceAggressor})
	squireTurn(t, e)
	squire := findBattlefield(t, e, 0, "Selfless Squire", 0)
	staff := findBattlefield(t, e, 0, "Thunderstaff", 0)
	drainTriggeredAbilities(t, e)
	src := findBattlefield(t, e, 1, "Aggressor", 0)
	life0 := e.G.Players[0].Life
	// Clear the parked priority ask first (replacement_damage_counter_test.go's
	// device): poseDamageReplacementChoice only asks when e.pending is nil, so
	// a raw emit under a parked priority decision parks the choice for
	// askNextReplacementChoice instead of posing it now.
	e.pending = nil
	e.damaging, e.combatDamaging = src, true
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	e.damaging, e.combatDamaging = 0, false
	pickOption(t, e, staff)
	drainTriggeredAbilities(t, e)
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 life = %d, want %d: the recomputed CR 616.1e candidate set dropped the Squire's still-applicable prevent-all", got, life0)
	}
	if countPreventionNotes(e, 1) != 1 || countPreventionNotes(e, 2) != 1 {
		t.Fatalf("prevention Notes: amount 1 x%d, amount 2 x%d, want one of each",
			countPreventionNotes(e, 1), countPreventionNotes(e, 2))
	}
	if countP1P1Changes(e, squire, 1) != 1 || countP1P1Changes(e, squire, 2) != 1 {
		t.Fatalf("Squire CounterChanges: +1 x%d, +2 x%d, want one of each",
			countP1P1Changes(e, squire, 1), countP1P1Changes(e, squire, 2))
	}
	if got := e.G.Obj(squire).Counter("P1P1"); got != 3 {
		t.Fatalf("Squire P1P1 counters = %d, want 3 (1 + 2)", got)
	}
	replayCheck(t, e, cfg)
}

// TestSelflessSquireOrderedPathStoresPrevention (r2 finding 2): two
// same-controller Squires, damage 3, order answer = candidate 0 — the
// effect-created match a bot's clamp fallback picks. The chosen full
// prevention is terminal (the held event never lands), so its re-entrant
// prevention Note is the occurrence's only log record: BOTH Squires grow +3
// off it and no damage reaches life.
func TestSelflessSquireOrderedPathStoresPrevention(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Selfless Squire", "Selfless Squire"}, nil,
		nil, []string{damagePreventedOnceAggressor})
	squireTurn(t, e)
	squire0 := findBattlefield(t, e, 0, "Selfless Squire", 0)
	squire1 := findBattlefield(t, e, 0, "Selfless Squire", 1)
	drainTriggeredAbilities(t, e)
	src := findBattlefield(t, e, 1, "Aggressor", 0)
	life0 := e.G.Players[0].Life
	// Clear the parked priority ask first, so the order choice is posed now
	// (poseDamageReplacementChoice asks only when e.pending is nil).
	e.pending = nil
	e.damaging, e.combatDamaging = src, false
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	e.damaging, e.combatDamaging = 0, false
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
		t.Fatalf("pending = %+v, want a two-candidate CR 6.1 order choice", d)
	}
	submitChoices(t, e, 0)
	drainTriggeredAbilities(t, e)
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 life = %d, want %d", got, life0)
	}
	if c := countPreventionNotes(e, 3); c != 1 {
		t.Fatalf("logged %d prevention Note(s) with amount 3, want 1: the ordered path's chosen prevention applied silently", c)
	}
	for _, s := range []state.ObjID{squire0, squire1} {
		if c := countP1P1Changes(e, s, 3); c != 1 {
			t.Fatalf("Squire %d logged %d CounterChange(P1P1, +3), want 1", s, c)
		}
		if got := e.G.Obj(s).Counter("P1P1"); got != 3 {
			t.Fatalf("Squire %d has %d P1P1 counters, want 3", s, got)
		}
	}
	replayCheck(t, e, cfg)
}

// TestDamageReplacementPreventsCaseInsensitive (r2 finding 3): the
// registration (effEffect) and collection (applyReplacementsDispatch)
// classifiers read the Prevent$ param case-insensitively, so the application
// classifier (damageReplacementPrevents) and both full-prevention arms must
// too — a non-canonical `Prevent$ true` bodyless registration must classify
// as full prevention, never fall through to the silent With==nil drop arm.
func TestDamageReplacementPreventsCaseInsensitive(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"True", "true", "TRUE"} {
		r := cards.Repl{Event: "DamageDone", Params: map[string]string{"Prevent": v}}
		if !damageReplacementPrevents(r) {
			t.Fatalf("damageReplacementPrevents(Prevent$ %q) = false, want true: the classifiers disagree on case", v)
		}
	}
	neg := cards.Repl{Event: "DamageDone", Params: map[string]string{"Prevent": "False"}}
	if damageReplacementPrevents(neg) {
		t.Fatal("damageReplacementPrevents(Prevent$ False) = true, want false")
	}
}

// TestDamagePreventedOnceIgnoresNonPreventionNotes: a stored Note that is not
// a damage prevention never fires the mode — neither a Note with an amount
// and unrelated text, nor the Fog whole-pass statement (amount-less by
// decision: a whole-turn statement is not "damage that would be dealt to you
// is prevented").
func TestDamagePreventedOnceIgnoresNonPreventionNotes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := combatTriggerBoard(t, reg, []string{"Selfless Squire"}, nil,
		nil, []string{damagePreventedOnceAggressor})
	squireTurn(t, e)
	squire := findBattlefield(t, e, 0, "Selfless Squire", 0)
	drainTriggeredAbilities(t, e)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("%d triggers pending after the ETB resolved, want 0", len(e.pendingTriggers))
	}
	e.emit(events.Event{Kind: events.Note, Player: 0, Amount: 7, Text: "unrelated marker"})
	e.emit(events.Event{Kind: events.Note, Obj: 0, Text: "all combat damage this turn is prevented"})
	if len(e.pendingTriggers) != 0 {
		for _, p := range e.pendingTriggers {
			t.Logf("queued: %s %+v", p.SA.Kind, p.SA.Params)
		}
		t.Fatalf("a non-prevention Note queued a DamagePreventedOnce trigger")
	}
	if got := e.G.Obj(squire).Counter("P1P1"); got != 0 {
		t.Fatalf("Squire grew to %d P1P1 counters on non-prevention Notes, want 0", got)
	}
}
