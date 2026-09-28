package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The CR 616.1 order-choice competitions this file covers are the three the
// engine used to settle in deterministic scan order with no posed choice:
// the AddCounter (CounterChange/PlayerCounterChange) rewrites, the all-Updated
// entry composition, and the CreateToken plan. Every test here drives real
// corpus cards whose bodies the scan order composes DIFFERENTLY by order --
// the two answers land measurably different final states, so the choice is
// real and a reverted fix fails the "no ask was posed" assertion first.

// optionFor returns the index of the order option naming source, failing the
// test when the ask does not offer it (the precondition that the competition
// is posed over the expected candidates).
func optionFor(t *testing.T, d *decision.Decision, source state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == source {
			return o.Index
		}
	}
	t.Fatalf("ask %+v offers no option for source %d", d, source)
	return -1
}

// TestHardenedScalesAndBranchingEvolutionPoseOrderChoice drives the corpus's
// own Plus.1 (Hardened Scales) and Twice (Branching Evolution) AddCounter
// bodies against one counter placement: one +1/+1 counter becomes 4 counters
// in scan order (plus, then double) but 3 with Branching Evolution applied
// first (double, then plus). The affected player (the creature's controller)
// is asked which applies first.
func TestHardenedScalesAndBranchingEvolutionPoseOrderChoice(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)

	t.Run("scan order answer: plus then double", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		scales := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Hardened Scales"))
		branching := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Branching Evolution"))
		bear := onBoard(t, e, 0, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
		if e.G.Obj(scales) == nil || e.G.Obj(branching) == nil || e.G.Obj(bear) == nil {
			t.Fatal("precondition: the two enchantments and the creature must be on the battlefield")
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
		d := e.Pending()
		if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
			t.Fatalf("pending = %+v, want seat 0's KReplacement order ask over 2 candidates", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{optionFor(t, d, scales)}}); err != nil {
			t.Fatal(err)
		}
		if got := e.G.Obj(bear).Counter("P1P1"); got != 4 {
			t.Fatalf("P1P1 counters = %d, want 4 (plus 1 then double: 1 -> 2 -> 4)", got)
		}
	})

	t.Run("reordered answer: double then plus", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		scales := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Hardened Scales"))
		branching := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Branching Evolution"))
		bear := onBoard(t, e, 0, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
		if e.G.Obj(scales) == nil || e.G.Obj(branching) == nil || e.G.Obj(bear) == nil {
			t.Fatal("precondition: the two enchantments and the creature must be on the battlefield")
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
		d := e.Pending()
		if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
			t.Fatalf("pending = %+v, want seat 0's KReplacement order ask over 2 candidates", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{optionFor(t, d, branching)}}); err != nil {
			t.Fatal(err)
		}
		if got := e.G.Obj(bear).Counter("P1P1"); got != 3 {
			t.Fatalf("P1P1 counters = %d, want 3 (double then plus 1: 1 -> 2 -> 3, which differs from the scan order's 4)", got)
		}
	})
}

// TestKismetAndSpelunkingPoseEntryOrderChoice drives the corpus's own
// tap/untap Updated pair: Kismet makes seat 0's lands enter tapped while
// Spelunking makes seat 0's lands enter untapped. The two fight over the
// same tapped bit -- the last body applied wins -- so the entering land's
// controller is asked which applies first, and the two answers land opposite
// tapped states.
func TestKismetAndSpelunkingPoseEntryOrderChoice(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)

	entry := func(t *testing.T, e *Engine) (state.ObjID, *decision.Decision) {
		t.Helper()
		kismet := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Kismet"))
		spelunking := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Spelunking"))
		if e.G.Obj(kismet) == nil || e.G.Obj(spelunking) == nil {
			t.Fatal("precondition: Kismet and Spelunking must be on the battlefield")
		}
		ent := e.G.AddObject(card(t, "Name:Vale\nTypes:Land\nOracle:x\n"), 0)
		ent.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), ent.ID))
		e.emit(events.Event{Kind: events.MoveZone, Obj: ent.ID, From: state.ZHand, To: state.ZBattlefield})
		d := e.Pending()
		if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
			t.Fatalf("pending = %+v, want seat 0's KReplacement order ask over 2 candidates", d)
		}
		return ent.ID, d
	}

	t.Run("untap first: the creature ends tapped", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		id, d := entry(t, e)
		spelunking := e.G.Zone(state.ZBattlefield, 0)[0]
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{optionFor(t, d, spelunking)}}); err != nil {
			t.Fatal(err)
		}
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("entered creature zone = %+v, want it on the battlefield", o)
		}
		if !o.Tapped {
			t.Fatal("untap applied first, Kismet's tap last: the land must be tapped")
		}
	})

	t.Run("tap first: the creature ends untapped", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		id, d := entry(t, e)
		kismet := e.G.Zone(state.ZBattlefield, 1)[0]
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{optionFor(t, d, kismet)}}); err != nil {
			t.Fatal(err)
		}
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("entered creature zone = %+v, want it on the battlefield", o)
		}
		if o.Tapped {
			t.Fatal("tap applied first, Spelunking's untap last: the land must be untapped (the opposite of the other order)")
		}
	})
}

// TestManufactorAndXornPoseTokenOrderChoice drives the corpus's own script
// rewriter (Academy Manufactor: a Treasure becomes Clue + Food + Treasure)
// against the corpus's own adder (Xorn: Treasures come plus an additional
// Treasure). The two do not commute: one Treasure becomes four tokens with
// the rewriter first and six with the adder first, so the token's creator is
// asked which applies first.
func TestManufactorAndXornPoseTokenOrderChoice(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)

	count := func(e *Engine, name string) int {
		n := 0
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			// Minted token faces name themselves "Treasure Token" etc.
			if o := e.G.Obj(id); o != nil && o.Face() != nil && strings.HasPrefix(o.Face().Name, name) {
				n++
			}
		}
		return n
	}

	t.Run("rewriter first: four tokens", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		e.G.Tokens = reg.Tokens
		manufactor := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Academy Manufactor"))
		xorn := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Xorn"))
		if e.G.Obj(manufactor) == nil || e.G.Obj(xorn) == nil {
			t.Fatal("precondition: Academy Manufactor and Xorn must be on the battlefield")
		}
		e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "c_a_treasure_sac"})
		d := e.Pending()
		if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
			t.Fatalf("pending = %+v, want seat 0's KReplacement order ask over 2 candidates", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{optionFor(t, d, manufactor)}}); err != nil {
			t.Fatal(err)
		}
		if got := count(e, "Treasure"); got != 2 {
			t.Fatalf("Treasure tokens = %d, want 2 (the original plus Xorn's extra; the plan had 4 mints total)", got)
		}
		if got := count(e, "Clue") + count(e, "Food"); got != 2 {
			t.Fatalf("Clue+Food tokens = %d, want 2 (Manufactor's rewrite of the one original mint)", got)
		}
	})

	t.Run("adder first: six tokens", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		e.G.Tokens = reg.Tokens
		manufactor := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Academy Manufactor"))
		xorn := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Xorn"))
		if e.G.Obj(manufactor) == nil || e.G.Obj(xorn) == nil {
			t.Fatal("precondition: Academy Manufactor and Xorn must be on the battlefield")
		}
		e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "c_a_treasure_sac"})
		d := e.Pending()
		if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
			t.Fatalf("pending = %+v, want seat 0's KReplacement order ask over 2 candidates", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{optionFor(t, d, xorn)}}); err != nil {
			t.Fatal(err)
		}
		if got := count(e, "Treasure"); got != 2 {
			t.Fatalf("Treasure tokens = %d, want 2 (each duplicated mint rewritten into Clue+Food+Treasure)", got)
		}
		if got := count(e, "Clue"); got != 2 {
			t.Fatalf("Clue tokens = %d, want 2 (6 mints total, two more than the rewriter-first order's 4)", got)
		}
	})
}

// TestLifeCompetitionParkedBehindOutstandingDecision covers the queue edge
// poseLifeReplacementChoice used to refuse: a non-commuting life competition
// (Angel of Vitality's Plus.1 against Boon Reflection's Twice) that arises
// while another decision is outstanding. The event parks on the queue behind
// the outstanding decision -- never overwriting it, never applying in scan
// order in its shadow -- and is asked when the queue drains.
func TestLifeCompetitionParkedBehindOutstandingDecision(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	angelCard := mustCorpusCard(t, reg, "Angel of Vitality")
	boonCard := mustCorpusCard(t, reg, "Boon Reflection")

	lifeOrdered := func(t *testing.T, e *Engine, first state.ObjID, wantLife int32, label string) {
		t.Helper()
		// The setup priority ask stays outstanding on purpose: the life event
		// below is emitted in its shadow.
		outstanding := e.Pending()
		if outstanding == nil {
			t.Fatal("precondition: the harness must leave an outstanding decision")
		}
		e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 2})
		// The park: the outstanding decision survives untouched, the event is
		// parked, and nothing was applied silently.
		if e.Pending() != outstanding {
			t.Fatalf("outstanding decision overwritten: pending now %+v", e.Pending())
		}
		if len(e.replChoices) != 1 {
			t.Fatalf("queued competitions = %d, want 1 (the parked life event)", len(e.replChoices))
		}
		if got := e.G.Players[0].Life; got != 20 {
			t.Fatalf("life = %d while parked, want 20 (the event has not been applied yet)", got)
		}
		// Answer passes until the drained queue asks the life competition.
		d := e.Pending()
		for n := 0; n < 12 && (d == nil || d.Kind != decision.KReplacement); n++ {
			if d == nil {
				t.Fatalf("no decision while waiting for the drained life competition (%s)", label)
			}
			if d.Kind != decision.KPriority {
				t.Fatalf("pending = %+v, want priority or the drained life competition (%s)", d, label)
			}
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
					break
				}
			}
			if pass < 0 {
				t.Fatalf("priority ask offers no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
				t.Fatal(err)
			}
			d = e.Pending()
		}
		if d == nil || d.Kind != decision.KReplacement {
			t.Fatalf("pending = %+v, want the drained life competition's KReplacement ask (%s)", d, label)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{optionFor(t, d, first)}}); err != nil {
			t.Fatal(err)
		}
		if got := e.G.Players[0].Life; got != wantLife {
			t.Fatalf("life = %d, want %d (%s)", got, wantLife, label)
		}
	}

	// Scan order (Angel of Vitality on the battlefield first) doubles AFTER
	// the plus: 2 -> 3 -> 6 = 26. The reordered competition applies Boon
	// Reflection first: 2 -> 4 -> 5 = 25. The named card is the answer the
	// subtest submits; the ask locates its option by the replacement's
	// source object.
	t.Run("angel first gains 6", func(t *testing.T) {
		e := newSeats(t, 2)
		a := onBoardCard(t, e, 0, angelCard)
		onBoardCard(t, e, 0, boonCard)
		lifeOrdered(t, e, a, 26, "plus 1 then double: 2 -> 3 -> 6")
	})
	t.Run("boon first gains 5", func(t *testing.T) {
		e := newSeats(t, 2)
		onBoardCard(t, e, 0, angelCard)
		b := onBoardCard(t, e, 0, boonCard)
		lifeOrdered(t, e, b, 25, "double then plus 1: 2 -> 4 -> 5")
	})
}
