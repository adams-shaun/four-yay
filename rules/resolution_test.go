package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// passUntilNonPriority answers "pass" priority decisions until a decision
// that is not KPriority is pending (or the game ends), then returns it. The
// mid-resolution tests need this rather than passUntilStackEmpty (which
// fatals on the very decision they came to see): casting hands priority
// back to the active player, and the asked decision appears only once every
// seat has passed and resolveTop drains the stack.
func passUntilNonPriority(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision pending (game over: %v)", e.G.Over)
		}
		if d.Kind != decision.KPriority {
			return d
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("never reached a non-priority decision within %d passes", limit)
	return nil
}

// castFixture casts the named fixture card from the pending priority
// decision, answering the follow-on target decision (if any) with the
// option for want (or option 0 when want is -1), and returns whatever
// decision is pending after the cast — for the resolution tests that is the
// mid-resolution KModes ask, once both seats pass.
func castFixture(t *testing.T, e *Engine, id state.ObjID, want int) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d != nil && d.Kind == decision.KTarget {
		tIdx := 0
		if want >= 0 {
			tIdx = -1
			for _, o := range d.Options {
				if o.Kind == "player" && o.Player == state.PlayerID(want) {
					tIdx = o.Index
				}
			}
			if tIdx < 0 {
				t.Fatalf("seat %d not offered as a target: %+v", want, d.Options)
			}
		}
		submitChoices(t, e, tIdx)
	}
	return passUntilNonPriority(t, e, 20)
}

// TestCharmAsksForItsModeAndRunsTheChoice is the KModes end-to-end carrier:
// a Charm-shaped instant (two Choices$ sub-abilities) is cast, the engine
// poses a KModes decision over the modes mid-resolution — suspended with the
// spell still on the stack — and the chosen mode (here the SECOND one, to
// prove the choice is honoured, not the engine's old first-mode default) is
// what runs. The log records the DecisionAsk/DecisionMade pair plus the
// ModeChosen event, and the whole game replays byte-for-byte from the log.
func TestCharmAsksForItsModeAndRunsTheChoice(t *testing.T) {
	t.Parallel()
	charm := "Name:PiC\nManaCost:R\nTypes:Instant\nA:SP$ Charm | Choices$ DoGain,DoLose\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\n" +
		"SVar:DoLose:DB$ LoseLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Lose 5 life\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 91, charm)
	addMana(t, e, 0, "R")
	life := e.G.Players[0].Life
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a KModes decision, got %+v", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "mode" ||
		d.Options[0].Label != "Gain 5 life" || d.Options[1].Label != "Lose 5 life" {
		t.Fatalf("mode options: %+v", d.Options)
	}
	if o := e.G.Obj(id); o.Zone != state.ZStack {
		t.Fatalf("the charm must suspend with the spell on the stack, zone %s", o.Zone)
	}
	submitChoices(t, e, 1) // mode 2: Lose 5 life — the CHOSEN mode runs
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Life != life-5 {
		t.Fatalf("life = %d, want %d (only the chosen mode ran)", e.G.Players[0].Life, life-5)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Errorf("the charm resolved to %s, want Graveyard", z)
	}
	if !hasEventKind(e, events.ModeChosen) {
		t.Fatal("no ModeChosen event recorded the modal pick")
	}
	if !hasEvent(e, events.DecisionAsk, 0) {
		t.Fatal("no DecisionAsk recorded")
	}
	replayCheck(t, e, cfg)
}

// TestCharmCannotBeCastForNoModePins the engine's mode-count validation: the
// KModes decision is Min == Max == CharmNum, so a client that answers with
// too few (or too many) choices is rejected — a real enforcement point that
// keeps a stray or partial modal answer from reaching the engine.
func TestCharmCannotBeCastForNoMode(t *testing.T) {
	t.Parallel()
	charm := "Name:PiC\nManaCost:R\nTypes:Instant\nA:SP$ Charm | Choices$ DoGain,DoLose\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\n" +
		"SVar:DoLose:DB$ LoseLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Lose 5 life\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 94, charm)
	addMana(t, e, 0, "R")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a KModes decision, got %+v", d)
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}} // Min 1, zero modes
	if err := d.Validate(in); err == nil {
		t.Fatal("zero-mode answer validated against the Min == CharmNum shape")
	}
}

// drainToEnd answers every pending decision — "pass" on a priority
// decision, the first Min options on a KModes decision (the bot policy: on
// an UnlessCost$ may-pay that is "Pay", which the engine declines when the
// payer's pool cannot cover it) — until the stack is empty. The paid Chain
// Lightning copy is itself a Chain Lightning (copies copy all text), so its
// own copy clause re-asks; the payer's empty pool declines it and the game
// finishes. Thist is the resolution tests' substitute for passUntilStackEmpty,
// which fatals on the very mid-resolution decision these tests came to see.
func drainToEnd(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision pending while the stack is non-empty")
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
		case decision.KTarget:
			// CR 707.10c: a copy with MayChooseTarget$ asks its controller
			// for a new target. AskCopyTargets places the inherited targets
			// first, so the leading d.Min options are the deterministic
			// keep-current answer every drain takes. Any other KTarget shape
			// is a surprise the drain must not paper over.
			if d.ResumeKind != "copy_targets" {
				t.Fatalf("unexpected non-copy target decision while draining: %+v", d)
			}
			ch := []int{}
			keep := d.Min
			if keep < 1 {
				keep = 1
			}
			for j := 0; j < len(d.Options) && j < keep; j++ {
				ch = append(ch, d.Options[j].Index)
			}
			submitChoices(t, e, ch...)
		case decision.KModes:
			ch := []int{}
			for j := 0; j < len(d.Options) && j < d.Min; j++ {
				ch = append(ch, d.Options[j].Index)
			}
			submitChoices(t, e, ch...)
		default:
			t.Fatalf("unexpected decision %+v while draining", d)
		}
	}
	if len(e.G.Stack) > 0 && !e.G.Over {
		t.Fatalf("stack not empty after %d drain steps", limit)
	}
}

// TestSuspendedResolutionSurvivesAClone pins the clone-safety requirement
// that the whole resume design is built on: the resume state is plain
// value/pointer data (kind/obj plus a shared-immutable *cards.SA), never a
// closure, so an Engine.Clone taken while a mid-resolution KModes decision
// is pending sees the same suspension, and answering both engines
// identically produces the same chain head. A closure captured over the
// original engine would fail exactly here (the clone would resume into
// nothing), which is the whole reason the field is structured this way.
func TestSuspendedResolutionSurvivesAClone(t *testing.T) {
	t.Parallel()
	charm := "Name:PiC\nManaCost:R\nTypes:Instant\nA:SP$ Charm | Choices$ DoDiscard,DoGain\n" +
		"SVar:DoDiscard:DB$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SpellDescription$ Discard a card\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 95, charm)
	addMana(t, e, 0, "R")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the cast-time KModes announcement, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes || e.resume == nil {
		t.Fatalf("expected a suspended Discard KModes decision, got %+v resume=%+v", d, e.resume)
	}
	c := e.Clone()
	if p := c.Pending(); p == nil || p.Kind != decision.KModes {
		t.Fatalf("clone lost the suspended decision: %+v", p)
	}
	// Answer both engines identically (same choice, then the same drain),
	// and require the same chain head — the cloned suspension must resume
	// into the same continuation and re-derive the same tail.
	submitEach := func(x *Engine, choices ...int) {
		d := x.Pending()
		if err := x.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
			t.Fatalf("submit %v: %v", choices, err)
		}
	}
	submitEach(e, d.Options[0].Index)
	drainToEnd(t, e, 30)
	submitEach(c, d.Options[0].Index)
	drainToEnd(t, c, 30)
	if e.L.Head() != c.L.Head() {
		t.Fatalf("clone diverged: chain %s vs %s", e.L.Head(), c.L.Head())
	}
	replayCheck(t, c, cfg)
}

// hasEventKind reports whether the log carries any event of the given kind.
func hasEventKind(e *Engine, kind events.Kind) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}
