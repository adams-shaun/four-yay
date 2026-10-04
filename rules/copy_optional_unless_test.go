package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The COMBINED Optional$ + UnlessCost$ CopySpellAbility shape, pinned
// end-to-end through the engine (findings-r2 found the round-1 read silently
// excluding these SAs). The corpus carriers are Wandering Archaic
// ("they may pay {2}. If they don't, you may copy that spell" -- unswitched)
// and Chain of Silence ("may sacrifice a land. If the player does, they may
// copy this spell" -- UnlessSwitched$ True). The two asks compose
// SEQUENTIALLY: the shared unless gate (effects/unless.go's unlessProceed)
// resolves its pay/decline first and decides only whether the copy body runs;
// when the body runs, its may-copy election is the SECOND ask, posed to the
// copy's CONTROLLER (the Controller$ binding), not to the resolving caster.
//
// The fixture is the engine-level Mimic carrier from copy_unswitched_test.go
// with Optional$ True added, so the engine-level unless_pay settlement and
// the copy_optional ask interact exactly as they do for the real corpus
// cards.

const optionalUnlessCopySrc = `Name:Mimic
ManaCost:1 R
Types:Sorcery
A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3 | SubAbility$ DBCopy
SVar:DBCopy:DB$ CopySpellAbility | Defined$ Parent | Controller$ TargetedController | UnlessPayer$ TargetedController | UnlessCost$ 2 | Optional$ True
Oracle:x
`

// chainSilenceCopySrc is the SWITCHED twin of the fixture above (Chain of
// Silence's own shape: UnlessSwitched$ True, so PAYING runs the body): "may
// sacrifice a land. If the player does, they may copy this spell."
const chainSilenceCopySrc = `Name:Mimic
ManaCost:1 R
Types:Sorcery
A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3 | SubAbility$ DBCopy
SVar:DBCopy:DB$ CopySpellAbility | Defined$ Parent | Controller$ TargetedController | UnlessPayer$ TargetedController | UnlessCost$ 2 | UnlessSwitched$ True | Optional$ True
Oracle:x
`

// castOptionalUnlessMimic casts the fixture at seat 1 (player target) and
// drives to the unless_pay decision, returning it.
func castOptionalUnlessMimic(t *testing.T, e *Engine, m state.ObjID) *decision.Decision {
	t.Helper()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == m {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", m, d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat 1 not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
		t.Fatalf("expected the unless_pay decision, got %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("unless_pay payer = seat %d, want the target's controller (seat 1)", d.Player)
	}
	return d
}

// drainOptionalUnlessCopy drains a Mimic resolution to the end. A KModes
// unless_pay gate is answered with answerPay (true = "Pay 2 — no copy",
// false = decline); a copy_optional election is a FAILURE unless
// allowElection, in which case it is answered with electionYes. Any other
// decision fails the test.
func drainOptionalUnlessCopy(t *testing.T, e *Engine, limit int, answerPay, allowElection, electionYes bool) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (depth %d)", len(e.G.Stack))
		}
		switch {
		case d.Kind == decision.KPriority:
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
		case d.Kind == decision.KModes && d.ResumeKind == "unless_pay":
			idx := 1 // decline
			if answerPay {
				idx = 0 // "Pay 2 — no copy"
			}
			submitChoices(t, e, d.Options[idx].Index)
		case d.Kind == decision.KChoose && d.ResumeKind == "copy_optional":
			if !allowElection {
				t.Fatalf("unexpected may-copy election posed: %+v", d)
			}
			if electionYes {
				submitChoices(t, e, d.Options[0].Index)
			} else {
				submitChoices(t, e, d.Options[1].Index)
			}
		default:
			t.Fatalf("unexpected decision while draining: %+v", d)
		}
	}
	if len(e.G.Stack) > 0 && !e.G.Over {
		t.Fatalf("stack not empty after %d drain steps", limit)
	}
}

// TestOptionalUnlessCopyPayingStopsCopyAndAsksNoElection pins the gate's
// half of the composition: on the unswitched shape, paying the UnlessCost$
// stops the copy AND the body never runs, so no may-copy election is ever
// posed on top of the pay.
func TestOptionalUnlessCopyPayingStopsCopyAndAsksNoElection(t *testing.T) {
	t.Parallel()
	e, cfg, m := newFixtureDeck(t, 311, optionalUnlessCopySrc)
	addMana(t, e, 0, "R1")
	addMana(t, e, 1, "22")
	life := e.G.Players[1].Life
	castOptionalUnlessMimic(t, e, m)
	submitChoices(t, e, 0) // "Pay 2 — no copy"
	drainOptionalUnlessCopy(t, e, 20, true, false, false)
	if e.G.Players[1].Life != life-3 {
		t.Fatalf("life = %d, want %d (the pay bought the copy off)", e.G.Players[1].Life, life-3)
	}
	for _, o := range e.G.Objs {
		if o.IsCopy {
			t.Fatal("a copy was made despite the payer covering the cost")
		}
	}
	if !hasEventKind(e, events.ModeChosen) {
		t.Fatal("no ModeChosen event recorded the unless-pay answer")
	}
	replayCheck(t, e, cfg)
}

// TestOptionalUnlessCopyDeclinePosesElectionToCopyController pins the
// sequencing AND the election's addressee: a declined gate runs the body,
// whose may-copy election is the SECOND ask, posed to the copy's controller
// (Controller$ TargetedController -- seat 1, the addressed player), NOT to
// the caster (seat 0). A declined election makes no copy; the original still
// resolves.
func TestOptionalUnlessCopyDeclinePosesElectionToCopyController(t *testing.T) {
	t.Parallel()
	e, cfg, m := newFixtureDeck(t, 312, optionalUnlessCopySrc)
	addMana(t, e, 0, "R1")
	addMana(t, e, 1, "22")
	life := e.G.Players[1].Life
	castOptionalUnlessMimic(t, e, m)
	submitChoices(t, e, 1) // "Don't pay — make a copy" (the gate; the body now runs)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "copy_optional" {
		t.Fatalf("expected the may-copy election (KChoose copy_optional) after the declined gate, got %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("the may-copy election was asked of seat %d, want seat 1 (the Controller$ TargetedController copy controller, not the seat-0 caster)", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("election options = %+v, want [yes, no]", d.Options)
	}
	submitChoices(t, e, d.Options[1].Index) // decline the copy
	drainOptionalUnlessCopy(t, e, 20, true, false, false)
	if e.G.Players[1].Life != life-3 {
		t.Fatalf("life = %d, want %d (declined election: only the original resolved)", e.G.Players[1].Life, life-3)
	}
	for _, o := range e.G.Objs {
		if o.IsCopy {
			t.Fatal("a copy was made despite the declined may-copy election")
		}
	}
	replayCheck(t, e, cfg)
}

// TestOptionalUnlessSwitchedCopyPaidGatePosesElection pins Chain of
// Silence's orientation (UnlessSwitched$ True): the payer PAYING runs the
// copy body, whose may-copy election is then the second ask -- still posed
// to the copy controller. A declined election makes no copy; the original
// still resolves. Paying makes no election at all (the body never runs).
func TestOptionalUnlessSwitchedCopyPaidGatePosesElection(t *testing.T) {
	t.Parallel()
	e, cfg, m := newFixtureDeck(t, 314, chainSilenceCopySrc)
	addMana(t, e, 0, "R1")
	addMana(t, e, 1, "22")
	life := e.G.Players[1].Life
	castOptionalUnlessMimic(t, e, m)
	submitChoices(t, e, 0) // pay the {2}: on the switched shape the body now RUNS
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "copy_optional" {
		t.Fatalf("expected the may-copy election after the PAID switched gate, got %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("the may-copy election was asked of seat %d, want seat 1 (the copy controller)", d.Player)
	}
	submitChoices(t, e, d.Options[1].Index) // decline the copy
	drainOptionalUnlessCopy(t, e, 20, true, false, false)
	if e.G.Players[1].Life != life-3 {
		t.Fatalf("life = %d, want %d (declined election: only the original resolved)", e.G.Players[1].Life, life-3)
	}
	for _, o := range e.G.Objs {
		if o.IsCopy {
			t.Fatal("a copy was made despite the declined may-copy election")
		}
	}
	replayCheck(t, e, cfg)
}

// TestOptionalUnlessCopyAcceptedElectionMakesOneCopy pins the accept arm
// through the full engine machinery: election "yes" places a real copy owned
// by the copy controller; the copy's own clause then re-asks (gate, election)
// and the drain pays it off, so exactly one copy resolves and seat 1 takes
// the damage of both.
func TestOptionalUnlessCopyAcceptedElectionMakesOneCopy(t *testing.T) {
	t.Parallel()
	e, cfg, m := newFixtureDeck(t, 313, optionalUnlessCopySrc)
	addMana(t, e, 0, "R1")
	addMana(t, e, 1, "22")
	life := e.G.Players[1].Life
	castOptionalUnlessMimic(t, e, m)
	submitChoices(t, e, 1) // decline the gate
	d := e.Pending()
	if d == nil || d.ResumeKind != "copy_optional" {
		t.Fatalf("expected the may-copy election, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index) // accept the copy
	// The copy's own clause: the drain pays its gate ("Pay 2 — no copy"), so
	// the cascade stops at exactly one copy. An election from the copy would
	// be a failure the drain reports.
	drainOptionalUnlessCopy(t, e, 30, true, false, false)
	if e.G.Players[1].Life != life-6 {
		t.Fatalf("life = %d, want %d (original + the accepted copy each deal 3)", e.G.Players[1].Life, life-6)
	}
	copies := 0
	for _, o := range e.G.Objs {
		if o.IsCopy {
			copies++
			if o.Zone != state.ZExile {
				t.Errorf("the accepted copy sits in %s, want Exile (resolved)", o.Zone)
			}
		}
	}
	if copies != 1 {
		t.Errorf("%d copies, want exactly 1", copies)
	}
	if !hasEventKind(e, events.ModeChosen) {
		t.Fatal("no ModeChosen event recorded the unless-pay answers")
	}
	replayCheck(t, e, cfg)
}
