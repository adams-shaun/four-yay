package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// unswitchedCopySrc is a self-copying spell whose copy clause is the
// UNSWITCHED shape — an UnlessCost$ with NO UnlessSwitched$. Its oracle is
// the inverse of Chain Lightning's: "that player may pay {2}. If they don't,
// you may copy this spell", so paying STOPS the copy and declining MAKES
// one. Before the fix effCopySpellAbility treated this shape as if paying
// caused the copy — exactly the inverted orientation this engine-level
// carrier pins as wrong.
const unswitchedCopySrc = `Name:Mimic
ManaCost:1 R
Types:Sorcery
A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3 | SubAbility$ DBCopy
SVar:DBCopy:DB$ CopySpellAbility | Defined$ Parent | Controller$ TargetedController | UnlessPayer$ TargetedController | UnlessCost$ 2 | MayChooseTarget$ True
Oracle:x
`

// castUnswitchedMimic casts the Mimic fixture at seat 1 and pumps priority
// (with both seats passing) until the engine poses the mid-resolution
// unless_pay decision, returning it.
func castUnswitchedMimic(t *testing.T, e *Engine, m state.ObjID) *decision.Decision {
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
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a mid-resolution pay decision, got %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("unless_pay payer = seat %d, want the target's controller (seat 1)", d.Player)
	}
	// The resolution must have suspended WITH the spell still on the stack
	// (the copies B3 keep: the suspend-with-the-spell-still-on-stack proof).
	if o := e.G.Obj(m); o.Zone != state.ZStack {
		t.Fatalf("resolution must suspend WITH the spell on the stack, zone %s", o.Zone)
	}
	return d
}

// TestUnswitchedCopyShapePayingStopsTheCopies is the engine-level B1 carrier
// for the UNSWITCHED orientation. On the unswitched shape, paying the
// UnlessCost$ STOPS the copy (the payer buys it off) and declining MAKES the
// copy — the inverse of the switched Chain Lightning shapes, which main
// already gets right. The return path through rules' resumeResolution and
// payMana is the same one the switched shapes use, so this also proves the
// bounded-recursion termination: a declined unswitched copy is itself a
// Mimic whose own (unswitched) clause re-asks, and paying it off stops the
// cascade at exactly one copy.
func TestUnswitchedCopyShapePayingStopsTheCopies(t *testing.T) {
	t.Parallel()
	t.Run("pay makes no copy", func(t *testing.T) {
		e, cfg, m := newFixtureDeck(t, 99, unswitchedCopySrc)
		addMana(t, e, 0, "R1")
		addMana(t, e, 1, "22")
		life := e.G.Players[1].Life
		d := castUnswitchedMimic(t, e, m)
		submitChoices(t, e, 0) // "Pay 2 — no copy"
		passUntilStackEmpty(t, e, 20)
		if e.G.Players[1].Life != life-3 {
			t.Fatalf("life = %d, want %d (no copy after the payer paid)", e.G.Players[1].Life, life-3)
		}
		for _, o := range e.G.Objs {
			if o.IsCopy {
				t.Fatal("a copy was made despite the payer covering the cost on the unswitched shape")
			}
		}
		if !hasEventKind(e, events.ModeChosen) {
			t.Fatal("no ModeChosen event recorded the unless-pay answer")
		}
		_ = d
		replayCheck(t, e, cfg)
	})
	t.Run("decline makes one copy", func(t *testing.T) {
		e, cfg, m := newFixtureDeck(t, 100, unswitchedCopySrc)
		addMana(t, e, 0, "R1")
		addMana(t, e, 1, "22")
		life := e.G.Players[1].Life
		d := castUnswitchedMimic(t, e, m)
		submitChoices(t, e, 1) // "Don't pay — make a copy"
		// The copy is itself a Mimic whose own (unswitched) clause re-asks;
		// drainToEnd answers that second ask with its default option 0
		// ("pay"), paying off the {2} and stopping the cascade at one copy.
		drainToEnd(t, e, 30)
		if e.G.Players[1].Life != life-6 {
			t.Fatalf("life = %d, want %d (original + the one declined-pay copy each deal 3)", e.G.Players[1].Life, life-6)
		}
		copies := 0
		for _, o := range e.G.Objs {
			if o.IsCopy {
				copies++
				if o.Zone != state.ZExile {
					t.Errorf("a resolved copy sits in %s", o.Zone)
				}
			}
		}
		if copies != 1 {
			t.Errorf("%d copies, want exactly 1 (the declined-pay copy)", copies)
		}
		if !hasEventKind(e, events.ModeChosen) {
			t.Fatal("no ModeChosen event recorded the unless-pay answers")
		}
		_ = d
		replayCheck(t, e, cfg)
	})
}
