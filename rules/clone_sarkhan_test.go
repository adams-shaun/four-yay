package rules

// Sarkhan, Soul Aflame is the corpus-pinned carrier for api:Clone's
// trigger-driven, RESOLVING shape (ticket api-clone-trigger-copy): a Dragon
// entering under your control fires the ChangesZone trigger whose Execute$
// is `DB$ Clone | Defined$ TriggeredCardLKICopy | NewName$ Sarkhan, Soul
// Aflame | AddTypes$ Legendary | Duration$ UntilEndOfTurn | Optional$ True`.
// The election is a real yes/no ask; the accepted copy overrides the name,
// keeps the copied characteristics, and expires at end-of-turn cleanup.

import (
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// sarkhanFixture moves Sarkhan Soul Aflame and a Dragon Hatchling onto seat
// 0's battlefield (the Dragon's entry fires Sarkhan's copy trigger) and
// drives to the may-copy election the trigger's resolution poses. Returns
// the engine, the replay config, both object ids and the pending election.
func sarkhanFixture(t *testing.T) (*Engine, Config, state.ObjID, state.ObjID, *decision.Decision) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Sarkhan, Soul Aflame", "Dragon Hatchling")
	sark := searchMoveByName(t, e, "Sarkhan, Soul Aflame", state.ZBattlefield)
	// Preconditions the assertions below stand on: Sarkhan on the battlefield
	// as its printed 2/4 Human Shaman self (so a copy is distinguishable by
	// its 0/1 Flying body), and the Dragon on the battlefield (the trigger's
	// ValidCard$ Dragon.YouCtrl capture).
	if o := e.G.Obj(sark); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Sarkhan fixture missing off the battlefield: %+v", o)
	}
	if f := e.G.Obj(sark).Face(); f == nil || f.Name != "Sarkhan, Soul Aflame" {
		t.Fatalf("Sarkhan fixture wrong face: %+v", f)
	}
	if d := e.Derived(sark); d.Power != 2 || d.Toughness != 4 {
		t.Fatalf("pre-copy Sarkhan P/T %d/%d, want 2/4", d.Power, d.Toughness)
	}
	drag := searchMoveByName(t, e, "Dragon Hatchling", state.ZBattlefield)
	if o := e.G.Obj(drag); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Dragon fixture missing off the battlefield: %+v", o)
	}
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "clone" {
		t.Fatalf("the Dragon entry did not pose a clone election: %+v", d)
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("election = %+v, want a Min==Max==1 KChoose for Sarkhan's controller", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("election options = %+v, want a yes/no pair with yes first", d.Options)
	}
	return e, cfg, sark, drag, d
}

// optionIndexOf returns the option whose Kind is kind.
func optionIndexOf(t *testing.T, d *decision.Decision, kind string) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index
		}
	}
	t.Fatalf("no %q option in %+v", kind, d.Options)
	return -1
}

// TestSarkhanDragonEntryPosesTheMayCopyElectionAndDeclineKeepsSarkhan is the
// election half of the pin: the Dragon entry poses the real may-copy ask,
// and the decline leaves Sarkhan exactly as it printed (no copy event).
func TestSarkhanDragonEntryPosesTheMayCopyElectionAndDeclineKeepsSarkhan(t *testing.T) {
	t.Parallel()
	e, cfg, sark, _, d := sarkhanFixture(t)
	no := optionIndexOf(t, d, "no")
	submitChoices(t, e, no)
	passUntilStackEmpty(t, e, 40)

	// The decline: Sarkhan keeps its printed 2/4 Human Shaman body and no
	// ClonePermanent event named it.
	if d := e.Derived(sark); d.Power != 2 || d.Toughness != 4 {
		t.Fatalf("declined Sarkhan P/T %d/%d, want the printed 2/4", d.Power, d.Toughness)
	}
	if hasEvent(e, events.ClonePermanent, sark) {
		t.Fatal("the decline still cloned")
	}
	if slices.Contains(e.Derived(sark).Types, "Dragon") {
		t.Fatal("declined Sarkhan reads as a Dragon")
	}
	replayCheck(t, e, cfg)
}

// TestSarkhanLeavesBeforeTheOptionalChoiceNeverAsks pins the findings-sol1
// MAJOR: Sarkhan leaves the battlefield (two Shocks, while its Dragon-entry
// trigger waits on the stack) before that trigger resolves. Resolving the
// trigger must NOT pose the may-copy election -- every become pair is dead
// (the copy loop would skip Sarkhan either way), so a decision whose every
// answer does nothing is never asked; the resolution completes silently and
// nothing is cloned.
func TestSarkhanLeavesBeforeTheOptionalChoiceNeverAsks(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Sarkhan, Soul Aflame", "Dragon Hatchling", "Shock", "Shock")
	sark := searchMoveByName(t, e, "Sarkhan, Soul Aflame", state.ZBattlefield)
	drag := searchMoveByName(t, e, "Dragon Hatchling", state.ZBattlefield)

	// Preconditions the assertions stand on: Sarkhan on the battlefield as
	// its printed 2/4 self (two Shocks are exactly lethal), the Dragon on
	// the battlefield, and Sarkhan's copy trigger ON THE STACK with seat 0
	// holding priority -- the state the regression runs against.
	if o := e.G.Obj(sark); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Sarkhan fixture missing off the battlefield: %+v", o)
	}
	if d := e.Derived(sark); d.Power != 2 || d.Toughness != 4 {
		t.Fatalf("pre-shock Sarkhan P/T %d/%d, want the printed 2/4", d.Power, d.Toughness)
	}
	if o := e.G.Obj(drag); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Dragon fixture missing off the battlefield: %+v", o)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("the Dragon entry did not leave Sarkhan's copy trigger on the stack")
	}

	// passPriority passes whoever currently holds priority.
	passPriority := func(stage string) {
		t.Helper()
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("%s: expected a priority decision, got %+v", stage, d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("%s: priority decision with no pass option: %+v", stage, d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("%s: submit pass: %v", stage, err)
		}
	}

	// Kill Sarkhan with two Shocks while the trigger waits on the stack.
	// A sorcery could not be cast here (the stack is not empty), so the
	// instant is the shape the real game reaches.
	for i := 0; i < 2; i++ {
		stage := fmt.Sprintf("shock %d", i+1)
		addMana(t, e, 0, "R")
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("%s: expected a priority decision, got %+v", stage, d)
		}
		shockID := state.ObjID(0)
		for _, id := range e.G.Zone(state.ZHand, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Shock" {
				shockID = id
				break
			}
		}
		if shockID == 0 {
			t.Fatalf("%s: no Shock left in seat 0's hand", stage)
		}
		cIdx := -1
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj == shockID {
				cIdx = o.Index
			}
		}
		if cIdx < 0 {
			t.Fatalf("%s: no cast option for Shock: %+v", stage, d.Options)
		}
		submitChoices(t, e, cIdx)
		d = e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("%s: expected a target decision, got %+v", stage, d)
		}
		tIdx := -1
		for _, o := range d.Options {
			if o.Obj == sark {
				tIdx = o.Index
			}
		}
		if tIdx < 0 {
			t.Fatalf("%s: Sarkhan not offered as a target: %+v", stage, d.Options)
		}
		submitChoices(t, e, tIdx)
		passPriority(stage + " pass 1")
		passPriority(stage + " pass 2")
	}

	// Sarkhan is dead (the SBA swept the 2/4 with 4 marked damage) and the
	// trigger is STILL on the stack -- the board the regression is about.
	if o := e.G.Obj(sark); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("two Shocks did not kill Sarkhan: %+v", o)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("the Dragon-entry trigger left the stack before resolution")
	}

	// Drain the stack: with the fix the trigger resolves without posing any
	// ask (passUntilStackEmpty fatals on a non-priority decision, so a
	// returned election fails the test here); with the pre-fix engine the
	// election appears exactly at this point.
	passUntilStackEmpty(t, e, 40)
	// The silence must be effClone's own dead-become return, not the
	// generic unimplemented-API fallback (which also asks nothing and clones
	// nothing): with api:Clone unregistered this Note is what the trigger's
	// resolution emits, so its absence proves the Clone handler ran.
	if hasNote(e, "unimplemented API Clone") {
		t.Fatal("the trigger resolved through the unimplemented-API fallback, not effClone")
	}
	if hasEventKind(e, events.ClonePermanent) {
		t.Fatal("the trigger cloned despite Sarkhan being gone")
	}
	if o := e.G.Obj(sark); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Sarkhan moved after the trigger resolved: %+v", o)
	}
	if o := e.G.Obj(drag); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the Dragon left the battlefield: %+v", o)
	}
	replayCheck(t, e, cfg)
}

// TestSarkhanAcceptedCopyIsNamedLegendaryAndExpiresAtCleanup is the accepted
// half: the copy takes the Dragon's characteristics with the overridden
// name and the added Legendary type, and end-of-turn cleanup reverts it to
// the printed 2/4 self.
func TestSarkhanAcceptedCopyIsNamedLegendaryAndExpiresAtCleanup(t *testing.T) {
	t.Parallel()
	e, cfg, sark, drag, d := sarkhanFixture(t)
	yes := optionIndexOf(t, d, "yes")
	submitChoices(t, e, yes)
	passUntilStackEmpty(t, e, 40)

	// The copy: the Dragon's body (0/1 Flying, red) under the overridden
	// name, Legendary added on top, and Sarkhan now reads as a Dragon.
	f := e.G.Obj(sark).Face()
	if f == nil || f.Name != "Sarkhan, Soul Aflame" {
		t.Fatalf("copy name %v, want the overridden Sarkhan, Soul Aflame", f)
	}
	dv := e.Derived(sark)
	if dv.Power != 0 || dv.Toughness != 1 {
		t.Fatalf("copy P/T %d/%d, want Dragon Hatchling's 0/1", dv.Power, dv.Toughness)
	}
	if !slices.Contains(dv.Keywords, "Flying") {
		t.Fatalf("copy keywords %v, want Flying from the copied Dragon", dv.Keywords)
	}
	if !slices.Contains(dv.Types, "Legendary") || !slices.Contains(dv.Types, "Dragon") {
		t.Fatalf("copy types %v, want Legendary (the AddTypes$ rider) and Dragon (the copied body)", dv.Types)
	}
	if slices.Contains(dv.Types, "Shaman") {
		t.Fatalf("copy types %v: the copied Dragon must not carry the printed Shaman", dv.Types)
	}
	// The copied Dragon itself never moved.
	if o := e.G.Obj(drag); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the copied Dragon left the battlefield: %+v", o)
	}
	replayCheck(t, e, cfg)

	// Expiry: the copy's Duration$ UntilEndOfTurn ends at this turn's
	// cleanup; Sarkhan reverts to its printed 2/4 Human Shaman.
	e.pending = nil
	e.setStep(state.StepCleanup)
	e.priorityRound()
	f = e.G.Obj(sark).Face()
	if f == nil || f.Name != "Sarkhan, Soul Aflame" {
		t.Fatalf("after cleanup the object is %v, want Sarkhan, Soul Aflame", f)
	}
	dv = e.Derived(sark)
	if dv.Power != 2 || dv.Toughness != 4 {
		t.Fatalf("after cleanup P/T %d/%d, want the printed 2/4", dv.Power, dv.Toughness)
	}
	if slices.Contains(dv.Keywords, "Flying") || slices.Contains(dv.Types, "Dragon") {
		t.Fatalf("after cleanup the copy did not expire: keywords %v types %v", dv.Keywords, dv.Types)
	}
	if !slices.Contains(dv.Types, "Legendary") || !slices.Contains(dv.Types, "Human") {
		t.Fatalf("after cleanup types %v, want the printed Legendary Human Shaman", dv.Types)
	}
}
