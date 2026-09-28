package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07 revision.
// These assertions pin the fixed CR 733.1 illegal-cast reversal in the
// ordinary suite; they are conformance checks, not approved approximations.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// cr733CounterProposal uses actual repo decks and compiled SAs. Setup moves
// and floating mana precede the proposal and go through events.Apply via emit.
// No library manipulation occurs DURING the proposal (CR 733.1's exceptions).
func cr733CounterProposal(t *testing.T, reg *cards.Registry, deckName, name string, pyromancer bool) (*Engine, state.ObjID) {
	t.Helper()
	deck := testutil.RepoDeck(t, reg, deckName)
	e := New(Config{Seed: 42, Names: []string{"caster", "opponent"}, Decks: [][]*cards.Card{deck, deck}, Tokens: reg.Tokens})
	e.Advance()
	find := func(name string, to state.Zone) state.ObjID {
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				if e.G.Obj(id).Face().Name == name {
					if z != to {
						e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
					}
					return id
				}
			}
		}
		t.Fatalf("CR 601.2e/733.1 fixture: repo deck %q lacks %q", deckName, name)
		return 0
	}
	source := find(name, state.ZHand)
	sa := e.G.Obj(source).Face().SpellAbility()
	// Shape checks protect the fixed oracle, not compute it. These named
	// cards require one other spell; none can target a player or permanent.
	if sa == nil || sa.API != "Counter" || sa.Params["TargetType"] != "Spell" || sa.Params["ValidTgts"] != "Card" || sa.Params["TargetMin"] != "" || sa.Params["TargetMax"] != "" || sa.Params["TgtZone"] != "" {
		t.Fatalf("CR 601.2e/733.1 fixture: %q no longer has the audited mandatory spell-target SA", name)
	}
	if pyromancer {
		id := find("Young Pyromancer", state.ZBattlefield)
		checked := 0
		for _, tr := range e.G.Obj(id).Face().Triggers {
			if tr.Mode == "SpellCast" {
				checked++
			}
		}
		if checked == 0 {
			t.Fatal("CR 733.1 vacuity guard: Young Pyromancer has no compiled SpellCast trigger")
		}
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 6})
	e.askPriority(0)
	if len(e.G.Stack) != 0 || len(e.pendingTriggers) != 0 || e.Pending().Kind != decision.KPriority || e.Pending().Player != 0 {
		t.Fatalf("CR 601.2e/733.1 fixture: %q needs an empty stack, no queued triggers and caster priority at seq %d", name, e.Pending().Seq)
	}
	return e, source
}

// cr733Propose exercises both the client path and the proposal entry point.
// The latter deliberately bypasses legalActions: fixing only the offer gate
// must not make the reversal test vacuous. It does not hand-construct an SA or
// mutate Game. beginCast is the same entry called by handlePriority.
// The client arm independently asserts the impossible option is ABSENT from
// legalActions (I-2), even once withholding it means no Submit is possible.
// The direct proposal must still be rejected/reversed without loss.
func cr733Propose(t *testing.T, e *Engine, source state.ObjID, direct bool) {
	t.Helper()
	if direct {
		e.beginCast(0, decision.Option{Kind: "cast", Obj: source})
		return
	}
	d := e.Pending()
	for _, opt := range e.legalActions(0) {
		if opt.Kind == "cast" && opt.Obj == source {
			t.Errorf("CR 601.2c/115.5: %q seq %d: legalActions must omit every cast option on this empty stack; offered %+v", e.G.Obj(source).Face().Name, d.Seq, opt)
		}
	}
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == source && opt.AltCostIndex == 0 && opt.Mode == "" {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
				t.Fatalf("CR 601.2e/733.1: proposal of %q at seq %d: %v", e.G.Obj(source).Face().Name, d.Seq, err)
			}
			return
		}
	}
	t.Logf("CR 601.2c/115.5: %q withheld at seq %d; legalActions absence asserted; direct-proposal sibling still exercises reversal", e.G.Obj(source).Face().Name, d.Seq)
}

// TestCR733IllegalCounterProposalReverses checks an independent oracle:
// the pre-proposal stack is empty and CR 115.5 excludes the spell itself, so
// these mandatory-target casts cannot complete 601.2c. CR 601.2/601.2e and
// 733.1 require rejection/reversal, NOT a CR 608.2b resolution-time fizzle.
// Preexisting floating mana must remain and the card must stay/return in hand.
func TestCR733IllegalCounterProposalReverses(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, tc := range []struct{ name, deck string }{
		{"Mana Leak", "uw-tempo"},
		{"Counterspell", "uw-tempo"},
		{"Force of Will", "ur-delver"},
	} {
		for _, entry := range []string{"priority_intent", "direct_proposal"} {
			t.Run(tc.name+"/"+entry, func(t *testing.T) {
				e, source := cr733CounterProposal(t, reg, tc.deck, tc.name, false)
				seq := e.Pending().Seq
				pool, life := e.G.Players[0].Pool, e.G.Players[0].Life
				hand := len(e.G.Zone(state.ZHand, 0))
				start := len(e.L.Events)
				checked++ // Count examined proposals, never offending offers.
				cr733Propose(t, e, source, entry == "direct_proposal")
				for _, ev := range e.L.Events[start:] {
					if ev.Kind == events.ManaAdd || ev.Kind == events.PutOnStack || ev.Kind == events.MoveZone {
						t.Logf("%q proposal seq %d: event seq %d kind=%v obj=%d amount=%d %s->%s %q", tc.name, seq, ev.Seq, ev.Kind, ev.Obj, ev.Amount, ev.From, ev.To, ev.Text)
					}
					if ev.Kind == events.Resolve && ev.Obj == source {
						t.Errorf("%q proposal seq %d resolved at seq %d — CR 601.2e/733.1: an illegal proposal must be reversed", tc.name, seq, ev.Seq)
					}
				}
				if got := e.G.Obj(source).Zone; got != state.ZHand || len(e.G.Zone(state.ZHand, 0)) != hand {
					t.Errorf("%q proposal seq %d left source %d in %s, hand size %d -> %d — CR 601.2e/733.1: return the spell to its original hand, not its resting zone", tc.name, seq, source, got, hand, len(e.G.Zone(state.ZHand, 0)))
				}
				if got := e.G.Players[0].Pool; got != pool {
					t.Errorf("%q proposal seq %d spent mana %v -> %v without any possible target — CR 733.1: cancel all payments", tc.name, seq, pool, got)
				}
				if e.G.Players[0].Life != life || len(e.G.Stack) != 0 || e.cast != nil {
					t.Errorf("%q proposal seq %d left life=%d (was %d), stack=%v, cast pending=%t — CR 733.1: reverse the entire proposal", tc.name, seq, e.G.Players[0].Life, life, e.G.Stack, e.cast != nil)
				}
				if d := e.Pending(); d == nil || d.Kind != decision.KPriority || d.Player != 0 || e.G.Priority != 0 {
					t.Errorf("%q proposal seq %d left decision %+v, priority=%d — CR 733.2: the original priority holder may act or pass", tc.name, seq, d, e.G.Priority)
				}
			})
		}
	}
	if checked == 0 {
		t.Fatal("CR 601.2e/733.1 vacuity guard: no illegal mandatory-target proposals examined")
	}
}

// TestCR733IllegalCastCannotTriggerPyromancer isolates the part a refund and
// MoveZone-to-hand patch would miss: CR 733.1 says no abilities trigger as a
// result of an undone action. Young Pyromancer's real repo-deck trigger must
// neither survive in the pending queue nor be pushed onto the stack. No
// triggerMatches/spellsCastThisTurn result is used to derive this expectation.
func TestCR733IllegalCastCannotTriggerPyromancer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, entry := range []string{"priority_intent", "direct_proposal"} {
		t.Run(entry, func(t *testing.T) {
			e, source := cr733CounterProposal(t, reg, "ur-delver", "Force of Will", true)
			seq, start := e.Pending().Seq, len(e.L.Events)
			checked++
			cr733Propose(t, e, source, entry == "direct_proposal")
			for _, ev := range e.L.Events[start:] {
				if ev.Kind == events.TriggerPush {
					t.Errorf("Force of Will proposal seq %d left Young Pyromancer's TriggerPush at seq %d (source %d) — CR 733.1: no abilities trigger from an undone action", seq, ev.Seq, ev.Obj)
				}
			}
			if len(e.pendingTriggers) != 0 || len(e.G.Stack) != 0 {
				t.Errorf("Force of Will proposal seq %d left Young Pyromancer consequences: queued=%d stack=%v — CR 733.1: no abilities trigger from an undone action", seq, len(e.pendingTriggers), e.G.Stack)
			}
		})
	}
	if checked == 0 {
		t.Fatal("CR 733.1 vacuity guard: no illegal proposals with Young Pyromancer examined")
	}
}
