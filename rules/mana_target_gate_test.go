package rules

// Task agent-20261009T085207Z-2ef0d22c: CR 605.1a -- an ability that requires
// a target is not a mana ability. Radiant Lotus ("{T}, Sacrifice one or more
// artifacts: Choose a color. Target player adds three mana of the chosen color
// for each artifact sacrificed this way") is an AB$ Mana with ValidTgts$ and an
// announced Sac<X/Artifact>. Classified as a mana ability it was offered by
// neither walk (the off-stack path cannot price an announced Sac<X>); it now
// takes the ordinary activated-ability path (X announcement, sacrifice,
// target, stack) and the mana lands on the chosen target. The Warring Triad,
// the other printed carrier whose targeting used to be accepted off the stack,
// moves with it.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// optionByLabel returns the pending decision's option of kind with label.
func gateOptionByLabel(t *testing.T, e *Engine, kind, label string) decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending while looking for %s %q", kind, label)
	}
	for _, o := range d.Options {
		if o.Kind == kind && o.Label == label {
			return o
		}
	}
	t.Fatalf("no %s option %q in %q: %+v", kind, label, d.Prompt, d.Options)
	return decision.Option{}
}

// hasOptionKindFor reports whether the pending decision offers kind for obj.
func hasOptionKindFor(e *Engine, kind string, obj state.ObjID) bool {
	d := e.Pending()
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == kind && o.Obj == obj {
			return true
		}
	}
	return false
}

// requireTargetingManaAbility asserts the precondition both tests share: the
// card's ability really is an AB$ Mana that targets, and the classifier now
// keeps it off the mana path.
func requireTargetingManaAbility(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	f := e.G.Obj(id).Face()
	var ab *cards.SA
	for _, a := range f.Abilities {
		if a != nil && a.IsActivated() && a.API == "Mana" {
			ab = a
		}
	}
	if ab == nil {
		t.Fatalf("precondition: %s has no activated Mana ability", f.Name)
	}
	if !effects.TargetsOf(ab).Targeted() {
		t.Fatalf("precondition: %s's Mana ability does not target: %s", f.Name, ab.Line)
	}
	if cards.IsManaAbilitySA(ab) {
		t.Fatalf("%s's targeting Mana ability classifies as a mana ability (CR 605.1a)", f.Name)
	}
	if len(f.ManaAbilities()) != 0 {
		t.Fatalf("%s lists %d mana abilities, want none", f.Name, len(f.ManaAbilities()))
	}
}

// passToResolution passes priority until the top of the stack resolves (the
// stack is empty) or a non-priority decision (a colour ask) is posed.
func passToResolution(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 8 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			return
		}
		passPriority(t, e)
	}
}

// TestRadiantLotusActivatesThroughTheOrdinaryPathAndAddsToTheTarget drives the
// real offer and activation: the ability is an "ability" option (not a mana
// "activate"), announces X, sacrifices the elected artifact, targets the
// opponent, and on resolution the ACTIVATOR picks the colour while the
// opponent receives three mana of it.
func TestRadiantLotusActivatesThroughTheOrdinaryPathAndAddsToTheTarget(t *testing.T) {
	t.Parallel()
	e, cfg, ids := edrBoard(t, testutil.CorpusRegistry(t), 101, map[string]state.Zone{
		"Radiant Lotus": state.ZBattlefield, "Sol Ring": state.ZBattlefield})
	lotus, ring := ids["Radiant Lotus"], ids["Sol Ring"]
	requireTargetingManaAbility(t, e, lotus)
	if o := e.G.Obj(ring); o.Zone != state.ZBattlefield || !slices.Contains(o.Face().Types, "Artifact") {
		t.Fatalf("precondition: Sol Ring is not an artifact on the battlefield: zone=%s", o.Zone)
	}
	if hasOptionKindFor(e, "activate", lotus) {
		t.Fatalf("Radiant Lotus offered as a mana-ability activation: %+v", e.Pending().Options)
	}
	submitChoices(t, e, abilityOption(t, e, lotus, 0).Index)
	submitChoices(t, e, gateOptionByLabel(t, e, "x", "X = 1").Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("expected the one-artifact sacrifice ask, got %+v", d)
	}
	var pick = -1
	for _, o := range e.Pending().Options {
		if o.Kind == "sacrifice" && o.Obj == ring {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("Sol Ring is not a sacrifice option: %+v", e.Pending().Options)
	}
	submitChoices(t, e, pick)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the target-player ask, got %+v", d)
	}
	submitChoices(t, e, gateOptionByLabel(t, e, "player", "b").Index)
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack depth after activation = %d, want the ability on the stack", len(e.G.Stack))
	}
	if z := e.G.Obj(ring).Zone; z != state.ZGraveyard {
		t.Fatalf("Sol Ring is in %s, want sacrificed as a cost", z)
	}
	passToResolution(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 {
		t.Fatalf("expected seat 0 (the activator, Chooser$ You) to choose the colour, got %+v", d)
	}
	answerManaChoose(t, e, "Add G")
	if got := e.G.Players[1].Pool[state.MG]; got != 3 || e.G.Players[1].Pool.Total() != 3 {
		t.Fatalf("target's pool %v, want exactly {G}{G}{G}", e.G.Players[1].Pool)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("activator's pool %v, want empty (the target received the mana)", e.G.Players[0].Pool)
	}
	if !e.G.Obj(lotus).Tapped {
		t.Fatal("Radiant Lotus was not tapped as a cost")
	}
	replayCheck(t, e, cfg)
}

// TestWarringTriadTargetingManaAbilityUsesTheStack: the other printed carrier
// with ValidTgts$ on its head. "{T}, Mill a card: Target player adds one mana
// of any color" requires a target, so it is an ordinary ability that waits on
// the stack and mills as a cost, not an instant mana activation.
func TestWarringTriadTargetingManaAbilityUsesTheStack(t *testing.T) {
	t.Parallel()
	e, cfg, ids := edrBoard(t, testutil.CorpusRegistry(t), 102, map[string]state.Zone{
		"The Warring Triad": state.ZBattlefield})
	triad := ids["The Warring Triad"]
	requireTargetingManaAbility(t, e, triad)
	if hasOptionKindFor(e, "activate", triad) {
		t.Fatalf("The Warring Triad offered as a mana-ability activation: %+v", e.Pending().Options)
	}
	library := len(e.G.Zone(state.ZLibrary, 0))
	submitChoices(t, e, abilityOption(t, e, triad, 0).Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the target-player ask, got %+v", d)
	}
	submitChoices(t, e, gateOptionByLabel(t, e, "player", "b").Index)
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack depth after activation = %d, want the ability on the stack", len(e.G.Stack))
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != library-1 {
		t.Fatalf("library = %d, want %d (Mill<1> paid as a cost)", got, library-1)
	}
	if e.G.Players[1].Pool.Total() != 0 {
		t.Fatalf("mana arrived before the ability resolved: %v", e.G.Players[1].Pool)
	}
	passToResolution(t, e)
	answerManaChoose(t, e, "Add U")
	if got := e.G.Players[1].Pool[state.MU]; got != 1 || e.G.Players[1].Pool.Total() != 1 {
		t.Fatalf("target's pool %v, want exactly {U}", e.G.Players[1].Pool)
	}
	replayCheck(t, e, cfg)
}
