// Animate's Duration$ AsLongAsInPlay is the "for as long as CARDNAME remains
// on the battlefield" lifetime, and for the real corpus carriers the host is
// the ANIMATING permanent, not the animated target:
//
//	SVar:TrigAnimate:DB$ Animate | ValidTgts$ Artifact.YouCtrl | TgtPrompt$ Select target artifact you control | Power$ 5 | Toughness$ 5 | Types$ Artifact,Creature | Duration$ AsLongAsInPlay
//
// Before this ticket effects.animateUntilEOT classified AsLongAsInPlay with
// the absent-Duration$/UntilEndOfTurn bucket, so every animation layer was
// registered UntilEOT and rules' EndOfTurnCleanup dropped the 5/5 at the
// controller's cleanup even though Skilled Animator and the artifact both
// stayed on the battlefield. The lifetime anchor was the bug's second half:
// the effect's Source is the ANIMATED object (Affects Card.Self), so merely
// clearing UntilEOT would have let the 5/5 outlive its Skilled Animator.
// animateHostScoped now recognises AsLongAsInPlay, so the grant is anchored
// to the ANIMATING source through ContinuousEffect.DurationSource (the
// Exchange of Words mechanism rules' continuousLive already honours), and
// registerAnimateEffects wires the CR 400.7 move-driven lifetime so a
// bounced-and-returned target is a plain artifact again rather than a
// reactivated animation.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSkilledAnimatorAsLongAsInPlayDuration: Skilled Animator's ETB makes a
// target artifact a 5/5 artifact creature for as long as Skilled Animator
// remains on the battlefield. The animation survives its controller's cleanup
// while both permanents remain, ends when Skilled Animator leaves, and is not
// restored by the target's own leave-and-return while the host is gone.
func TestSkilledAnimatorAsLongAsInPlayDuration(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Skilled Animator"), lookup(t, reg, "Sol Ring")},
		[]*cards.Card{})

	// The animation target: a plain artifact, no P/T and no creature type.
	// These are the preconditions every assertion below rests on -- an
	// artifact that were already a 5/5 creature would make "the animation
	// applied" vacuous, and the two values under comparison (0/0
	// non-creature vs 5/5 creature) must actually differ.
	ring := moveByName(t, e, 0, "Sol Ring", state.ZBattlefield)
	ro := e.G.Obj(ring)
	if ro == nil || ro.Zone != state.ZBattlefield || ro.Controller != 0 {
		t.Fatalf("precondition: Sol Ring not on seat 0's battlefield: %+v", ro)
	}
	if e.IsCreature(ring) {
		t.Fatal("precondition: Sol Ring is already a creature")
	}
	initial := e.Derived(ring)
	if initial.Power != 0 || initial.Toughness != 0 {
		t.Fatalf("precondition: unanimated Sol Ring = %d/%d, want 0/0", initial.Power, initial.Toughness)
	}
	artifact, creature := false, false
	for _, typ := range initial.Types {
		artifact = artifact || typ == "Artifact"
		creature = creature || typ == "Creature"
	}
	if !artifact || creature {
		t.Fatalf("precondition: unanimated Sol Ring types = %v, want Artifact and not Creature", initial.Types)
	}

	// Skilled Animator enters: its ChangesZone ETB trigger fires and asks for
	// the target artifact.
	anim := moveByName(t, e, 0, "Skilled Animator", state.ZBattlefield)
	ao := e.G.Obj(anim)
	if ao == nil || ao.Zone != state.ZBattlefield || ao.Controller != 0 {
		t.Fatalf("precondition: Skilled Animator not on seat 0's battlefield: %+v", ao)
	}
	d := passToTargetAsk(t, e)
	idx := -1
	for _, o := range d.Options {
		if o.Obj == ring {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Sol Ring not offered as Skilled Animator's Animate target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 30)

	// The animation resolved onto the real shared path: a layer-7b P/T set and
	// a layer-4 type grant, both anchored to the ANIMATING source (DurationSource
	// = Skilled Animator) rather than dropped at cleanup. Asserting the
	// registration is what proves the handler ran, not a silent no-op.
	lpt := false
	ltype := false
	for _, ce := range e.active() {
		if ce.Source != ring {
			continue
		}
		switch ce.Layer {
		case state.LPT:
			lpt = true
			if ce.DurationSource != anim {
				t.Fatalf("LPT grant DurationSource = %d, want the ANIMATING source %d", ce.DurationSource, anim)
			}
		case state.LType:
			ltype = true
			if ce.DurationSource != anim {
				t.Fatalf("LType grant DurationSource = %d, want the ANIMATING source %d", ce.DurationSource, anim)
			}
		}
	}
	if !lpt || !ltype {
		t.Fatalf("animation halves not registered (LPT=%v LType=%v, continuous=%+v)", lpt, ltype, e.continuous)
	}

	d0 := e.Derived(ring)
	if !e.IsCreature(ring) {
		t.Fatalf("Skilled Animator did not make Sol Ring a creature: types=%v", d0.Types)
	}
	if d0.Power != 5 || d0.Toughness != 5 {
		t.Fatalf("animated Sol Ring = %d/%d, want 5/5", d0.Power, d0.Toughness)
	}

	// Control: the animation survives seat 0's end-of-turn cleanup while both
	// permanents remain (Duration$ AsLongAsInPlay is NOT UntilEOT). Turn 2 is
	// the opponent's turn, so the boundary has been crossed for real.
	driveToStep(t, e, 2, 1, state.StepMain1)
	if o := e.G.Obj(anim); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Skilled Animator left before the cleanup check: %+v", o)
	}
	if o := e.G.Obj(ring); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Sol Ring left before the cleanup check: %+v", o)
	}
	d1 := e.Derived(ring)
	if !e.IsCreature(ring) || d1.Power != 5 || d1.Toughness != 5 {
		t.Fatalf("AsLongAsInPlay animation did not survive the cleanup: %d/%d creature=%v",
			d1.Power, d1.Toughness, e.IsCreature(ring))
	}

	// CR 400.7: the target leaves and returns while the host stays. The
	// returned object is a NEW object, so the old grant must not reactivate
	// on it.
	e.emit(events.Event{Kind: events.MoveZone, Obj: ring,
		From: state.ZBattlefield, To: state.ZHand})
	if e.IsCreature(ring) {
		t.Fatal("the animation survived the animated object's own zone change (CR 400.7 violation)")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: ring,
		From: state.ZHand, To: state.ZBattlefield})
	if o := e.G.Obj(ring); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Sol Ring did not return to the battlefield: %+v", o)
	}
	if e.IsCreature(ring) {
		t.Fatal("the animation re-applied on the target's re-entry (CR 400.7 violation)")
	}
	if d := e.Derived(ring); d.Power == 5 && d.Toughness == 5 {
		t.Fatalf("the P/T grant re-applied on the target's re-entry: %d/%d", d.Power, d.Toughness)
	}

	// A second, fresh animation run to pin the host-leaves end: the ETB cannot
	// retrigger, so re-run the same ETB through a clean engine rather than
	// reasoning about a stale registration.
	t.Run("host leaves", func(t *testing.T) {
		reg := testutil.CorpusRegistry(t)
		e2, cfg2 := corpusEngineCfg(t, reg,
			[]*cards.Card{lookup(t, reg, "Skilled Animator"), lookup(t, reg, "Sol Ring")},
			[]*cards.Card{})
		ring2 := moveByName(t, e2, 0, "Sol Ring", state.ZBattlefield)
		anim2 := moveByName(t, e2, 0, "Skilled Animator", state.ZBattlefield)
		if e2.IsCreature(ring2) {
			t.Fatal("precondition: Sol Ring is already a creature")
		}
		d := passToTargetAsk(t, e2)
		idx := -1
		for _, o := range d.Options {
			if o.Obj == ring2 {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("Sol Ring not offered as Skilled Animator's Animate target: %+v", d.Options)
		}
		submitChoices(t, e2, idx)
		passUntilStackEmpty(t, e2, 30)
		if !e2.IsCreature(ring2) {
			t.Fatal("precondition: the animation did not apply before the host departed")
		}

		// Skilled Animator leaves the battlefield: the grant ends while the
		// artifact remains.
		e2.emit(events.Event{Kind: events.MoveZone, Obj: anim2,
			From: state.ZBattlefield, To: state.ZGraveyard})
		if o := e2.G.Obj(anim2); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition: Skilled Animator did not leave: %+v", o)
		}
		if o := e2.G.Obj(ring2); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Sol Ring left with its animator: %+v", o)
		}
		if e2.IsCreature(ring2) {
			t.Fatalf("the AsLongAsInPlay animation outlived Skilled Animator: types=%v", e2.Derived(ring2).Types)
		}
		if d := e2.Derived(ring2); d.Power == 5 && d.Toughness == 5 {
			t.Fatalf("the P/T grant outlived Skilled Animator: %d/%d", d.Power, d.Toughness)
		}
		// CR 400.7: returning host is a new object and cannot resurrect the
		// old grant even though the animated artifact never moved.
		e2.emit(events.Event{Kind: events.MoveZone, Obj: anim2,
			From: state.ZGraveyard, To: state.ZBattlefield})
		if o := e2.G.Obj(anim2); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Skilled Animator did not return: %+v", o)
		}
		if e2.IsCreature(ring2) || e2.Derived(ring2).Power == 5 && e2.Derived(ring2).Toughness == 5 {
			t.Fatal("the old animation reactivated when Skilled Animator returned")
		}
		replayCheck(t, e2, cfg2)
	})

	replayCheck(t, e, cfg)
}
