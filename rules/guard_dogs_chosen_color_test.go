package rules

// Guard Dogs -- the SharesColorWith ChosenCard colour gate, end to end on the
// real corpus card ({2}{W}, {T}: Choose a permanent you control. Prevent all
// combat damage target creature would deal this turn if it shares a color
// with that permanent.). The card has THREE steps: choose a permanent, choose
// a target creature, and register the prevention only when the target shares
// a colour with the chosen permanent.
//
// Before this ticket the predicate `Card.SharesColorWith ChosenCard` was
// unrecognised, so effects.conditionMet returned UNRESOLVED and the registry's
// documented fail-open convention ran the sub unconditionally: the card
// over-prevented (it registered the prevention regardless of colour).
// Recognising the predicate alone was not enough -- the DB sub's own
// ValidTgts$ ask is posed by chosenTargetsFor INSIDE the body dispatch, AFTER
// the gate, so on the empty group the recognised predicate resolved the gate
// false and the whole sub (target ask included) was skipped. Both halves land
// together: conditions.go's Targeted branch now leaves an un-covered
// ValidTgts$ SA UNRESOLVED so the ask is posed, and matchSharesColorWith reads
// the event-backed chosen set so the answered re-entry resolves for real.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// guardDogsPrevention counts the LIVE DamageDone replacements sourced by the
// Guard Dogs that name the given object, i.e. the RPrevent registration
// DBPrevent's DB$ Effect makes when its colour gate resolves true. It reads
// the engine's own continuous-effect registry (rules/layers.go's active()),
// so a registration that expired or was never made cannot be mistaken for a
// live one.
func guardDogsPrevention(e *Engine, dogs, source state.ObjID) int {
	n := 0
	for _, ce := range e.active() {
		if ce.ReplacementEvent != "DamageDone" || ce.Source != dogs {
			continue
		}
		for _, id := range ce.Remembered {
			if id == source {
				n++
			}
		}
	}
	return n
}

// guardDogsDrive activates the real Guard Dogs ability, chooses the named
// permanent and targets the named creature, then settles the chain. It
// asserts the preconditions the colour comparison and the registry read
// depend on: both permanents are on the battlefield under seat 0, their
// printed colours are exactly as expected, and the chosen permanent is the
// object the card's ChooseCard step actually recorded.
func guardDogsDrive(t *testing.T, chosen, chosenColor, target, targetColor string) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Guard Dogs", chosen, target}, nil)
	dogs := findOnBoard(t, e, 0, "Guard Dogs")
	chosenID := findOnBoard(t, e, 0, chosen)
	targetID := findOnBoard(t, e, 0, target)
	if o := e.G.Obj(chosenID); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: %s is not on the battlefield (zone %s)", chosen, o.Zone)
	}
	if o := e.G.Obj(targetID); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: %s is not on the battlefield (zone %s)", target, o.Zone)
	}
	if got := e.Colors(chosenID); got != chosenColor {
		t.Fatalf("precondition: chosen %s colours = %q, want %q", chosen, got, chosenColor)
	}
	if got := e.Colors(targetID); got != targetColor {
		t.Fatalf("precondition: target %s colours = %q, want %q", target, got, targetColor)
	}
	if chosenID == targetID {
		t.Fatalf("precondition: chosen and target are the same object %d", chosenID)
	}

	// Fund {2}{W} and offer the ability at priority.
	e.G.Players[0].Pool[state.MW] = 4
	e.G.Players[0].Pool[state.MC] = 8
	e.priorityRound()
	opt, ok := findAbilityOption(e, dogs, 0)
	if !ok {
		t.Fatalf("precondition: Guard Dogs' ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	for i := 0; i < 16; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		switch {
		case d.Kind == decision.KChoose && d.ResumeKind == "choice":
			idx := -1
			for _, o := range d.Options {
				if o.Obj == chosenID {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("chosen permanent %d not offered: %+v", chosenID, d.Options)
			}
			submitChoices(t, e, idx)
		case d.Kind == decision.KChoose && d.ResumeKind == "tgts":
			idx := -1
			for _, o := range d.Options {
				if o.Obj == targetID {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("target %d not offered: %+v", targetID, d.Options)
			}
			submitChoices(t, e, idx)
		default:
			submitChoices(t, e, 0)
		}
	}

	// The choose step must have recorded the chosen permanent on Guard Dogs'
	// event-backed chosen list for at least part of the resolution; by the end
	// DBCleanup has cleared it again. Assert the ability fully resolved rather
	// than that it is still recorded: the target ask is the observable that
	// proves the ordering fix, and this drive asserts it reached the target.
	if len(e.G.Stack) != 0 {
		t.Fatalf("precondition: the ability did not resolve: stack %v", e.G.Stack)
	}
	if e.G.Obj(dogs).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Guard Dogs left the battlefield: zone %s", e.G.Obj(dogs).Zone)
	}
	return e, dogs, targetID
}

// TestGuardDogsChosenColorGate pins the two branches of the colour gate on the
// live card: a target sharing a colour with the chosen permanent registers the
// combat-damage prevention; a target that shares nothing does not.
func TestGuardDogsChosenColorGate(t *testing.T) {
	t.Parallel()

	// Matching: the green Grizzly Bears is chosen and the green Elvish Mystic
	// is targeted -- they share {G}, so DBPrevent's effect gate resolves and
	// RPrevent's combat-damage prevention is registered.
	e, dogs, mystic := guardDogsDrive(t, "Grizzly Bears", "G", "Elvish Mystic", "G")
	if n := guardDogsPrevention(e, dogs, mystic); n != 1 {
		t.Fatalf("green target shares {G} with the green chosen permanent: %d live preventions, want 1", n)
	}

	// Not matching: the green chosen permanent and the white Savannah Lions
	// share nothing, so CR 608.2's "if" fails, the sub is skipped and NO
	// prevention is registered.
	e2, dogs2, lion := guardDogsDrive(t, "Grizzly Bears", "G", "Savannah Lions", "W")
	if n := guardDogsPrevention(e2, dogs2, lion); n != 0 {
		t.Fatalf("white target shares nothing with the green chosen permanent: %d live preventions, want 0", n)
	}
}
