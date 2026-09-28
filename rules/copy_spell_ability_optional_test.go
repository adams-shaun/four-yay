package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Optional$ True CopySpellAbility family, pinned end-to-end on the real
// corpus card Sevinne's Reclamation:
//
//	SVar:DBCopy:DB$ CopySpellAbility | Defined$ Parent | Optional$ True |
//	  MayChooseTarget$ True | ConditionDefined$ Self |
//	  ConditionPresent$ Card.wasCastFromGraveyard
//
// ("If this spell was cast from a graveyard, you may copy this spell and may
// choose a new target for the copy.") Before task api-copyspellability-
// optional the copy was made unconditionally; Optional$ True now poses a real
// KChoose yes/no over the ordinary mid-resolution machinery, and the answered
// election rides Ctx.CopyOpt.
//
// Every test asserts its own preconditions (the corpus card really carries the
// optional copy clause; the spell really reached the copy sub through a
// graveyard-origin cast) so a vacuous setup fails loudly rather than passing.

// sevinneOptionalCopyClause reports whether the compiled corpus card carries
// the Optional$ True CopySpellAbility this task reads. It is the precondition
// that separates the real carrier from a synthetic look-alike.
func sevinneOptionalCopyClause(t *testing.T, sf *cards.Card) bool {
	t.Helper()
	for _, face := range sf.Faces {
		for _, ab := range face.Abilities {
			for sub := ab; sub != nil; sub = sub.Sub {
				if sub.API == "CopySpellAbility" && sub.Params["Optional"] == "True" {
					return true
				}
			}
		}
	}
	return false
}

// sevinneCopyElectionSetup builds a two-seat real-corpus engine with Sevinne's
// Reclamation and two Grizzly Bears in seat 0's graveyard, casts Sevinne via
// its flashback cost, answers the ChangeZone target ask with the first Bear,
// and returns the engine, config, and the ids of Sevinne and the two Bears --
// parked at the may-copy election.
func sevinneCopyElectionSetup(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	sf := searchCorpusCard(t, reg, "Sevinne's Reclamation")
	if len(sf.Faces) == 0 || sf.Faces[0].Name != "Sevinne's Reclamation" {
		t.Fatalf("corpus card is not Sevinne's Reclamation: %+v", sf.Faces)
	}
	if !sevinneOptionalCopyClause(t, sf) {
		t.Fatal("Sevinne's Reclamation corpus card has no Optional$ True CopySpellAbility")
	}
	// PRECONDITION: the card must carry a flashback cost, since the copy clause
	// is gated on a graveyard-origin cast and the setup below uses flashback.
	if _, ok := sf.Faces[0].KeywordParam("Flashback"); !ok {
		t.Fatal("Sevinne's Reclamation corpus card has no Flashback keyword")
	}

	eng, cfg := miscHandsEngine(t, reg,
		[]string{"Sevinne's Reclamation", "Grizzly Bears", "Grizzly Bears"}, nil, nil, nil)
	cfg.Seed = seed
	sev := miscHandObj(t, eng, 0, "Sevinne's Reclamation")
	bears := []state.ObjID{
		miscMoveByName(t, eng, 0, "Grizzly Bears", state.ZGraveyard),
		miscMoveByName(t, eng, 0, "Grizzly Bears", state.ZGraveyard),
	}
	eng.emit(events.Event{Kind: events.MoveZone, Obj: sev, From: state.ZHand, To: state.ZGraveyard})
	eng.pending = nil
	eng.priorityRound()

	// {4}{W} flashback: five white mana covers 4 generic + W.
	addMana(t, eng, 0, "WWWWW")

	// Cast via flashback.
	fbIdx := -1
	for _, o := range castOptions(t, eng) {
		if o.Mode == "flashback" && o.Obj == sev {
			fbIdx = o.Index
		}
	}
	if fbIdx < 0 {
		t.Fatalf("flashback cast of Sevinne's Reclamation not offered from the graveyard: %+v", castOptions(t, eng))
	}
	submitChoices(t, eng, fbIdx)

	// The ChangeZone target ask (a permanent card with mana value 3 or less
	// from seat 0's graveyard). Choose the first Bear.
	d := eng.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Sevinne's Reclamation target ask, got %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == bears[0] {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("the graveyard Grizzly Bears was not offered as a target: %+v", d.Options)
	}
	submitChoices(t, eng, tgt)

	// PRECONDITION the Optional$ read depends on: the cast really left a
	// graveyard-origin provenance bit, so the ConditionDefined$ Self |
	// ConditionPresent$ Card.wasCastFromGraveyard gate holds and the copy sub
	// is genuinely reached rather than skipped.
	if o := eng.G.Obj(sev); o == nil || state.WasCastFromGraveyard(o.CastFlags) == false {
		t.Fatalf("precondition: Sevinne's Reclamation cast did not record a graveyard-origin flag: %+v", o)
	}

	// Drive to the may-copy election.
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "copy_optional" {
		t.Fatalf("expected the Optional$ True may-copy election (KChoose copy_optional), got %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("the may-copy election was asked of seat %d, want seat 0", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("may-copy election options = %+v, want [yes, no]", d.Options)
	}
	return eng, cfg, sev, bears[0], bears[1]
}

// driveSevinneResolutionToEnd drains a Sevinne's Reclamation resolution in
// flight to completion, handling the mid-resolution decisions its copy clause
// can pose: a copy_targets target election (keep-current). It returns the
// number of nested copy_optional may-copy elections it declined, which MUST be
// zero: a stack copy is PUT on the stack, never cast (CR 707.10), so the
// copy's own "if this spell was cast from a graveyard" clause is false and
// the copy never poses its own election. The counter is kept so each caller
// asserts that closure explicitly (state.ObjectWasCastFromGraveyard is the
// never-cast guard that makes it hold). Any other decision is a failure.
func driveSevinneResolutionToEnd(t *testing.T, e *Engine, limit int) int {
	t.Helper()
	nestedDeclines := 0
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
		case d.Kind == decision.KChoose && d.ResumeKind == "copy_optional":
			submitChoices(t, e, d.Options[1].Index) // decline the nested copy
			nestedDeclines++
		case d.Kind == decision.KTarget && d.ResumeKind == "copy_targets":
			submitChoices(t, e, d.Options[0].Index) // keep current
		default:
			t.Fatalf("unexpected decision while draining Sevinne's Reclamation: %+v", d)
		}
	}
	return nestedDeclines
}

// TestSevinnesReclamationMayCopyElectionDeclineMakesNoCopy pins the decline
// arm: answering "no" makes no copy, and the original spell still resolves
// (the returned Bear reaches the battlefield), so the decline skipped only the
// copy, not the whole sub-chain.
func TestSevinnesReclamationMayCopyElectionDeclineMakesNoCopy(t *testing.T) {
	t.Parallel()
	eng, cfg, _, bear0, bear1 := sevinneCopyElectionSetup(t, 211)
	// Answer "no" (option 1).
	d := eng.Pending()
	submitChoices(t, eng, d.Options[1].Index)

	// Drain the rest of the resolution.
	driveSevinneResolutionToEnd(t, eng, 30)

	if z := eng.G.Obj(bear0).Zone; z != state.ZBattlefield {
		t.Fatalf("the returned Grizzly Bears sits in %s, want Battlefield: the decline skipped the whole sub-chain", z)
	}
	if z := eng.G.Obj(bear1).Zone; z != state.ZGraveyard {
		t.Fatalf("the second Grizzly Bears sits in %s, want Graveyard (the copy should not have returned it)", z)
	}
	if copy := copyOnStack(eng); copy != 0 {
		t.Fatalf("a copy %d remained on the stack after the decline", copy)
	}
	for _, o := range eng.G.Objs {
		if o.IsCopy {
			t.Fatalf("the decline still made a copy: %+v", o)
		}
	}
	replayCheck(t, eng, cfg)
}

// TestSevinnesReclamationMayCopyElectionAcceptMakesCopy pins the accept arm:
// answering "yes" places a real copy (CR 707.10) on the stack, and the
// MayChooseTarget$ election lets it retarget the second graveyard Bear.
func TestSevinnesReclamationMayCopyElectionAcceptMakesCopy(t *testing.T) {
	t.Parallel()
	eng, cfg, _, bear0, bear1 := sevinneCopyElectionSetup(t, 223)
	// Answer "yes" (option 0).
	d := eng.Pending()
	submitChoices(t, eng, d.Options[0].Index)

	// The copy is placed on the stack; with MayChooseTarget$ True it poses its
	// own copy_targets KTarget ask when it resolves.
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "copy_targets" {
		t.Fatalf("expected the copy's MayChooseTarget$ election (KTarget copy_targets), got %+v", d)
	}
	copyID := d.Source
	co := eng.G.Obj(copyID)
	if co == nil || !co.IsCopy || co.Zone != state.ZStack {
		t.Fatalf("copy ask source %d is not a stack copy: %+v", copyID, co)
	}
	// PRECONDITION: the original Bear was already returned, so the accepted
	// copy must choose the OTHER Bear as its new target (which also proves the
	// copy's target ask offers a legal graveyard card).
	if z := eng.G.Obj(bear0).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: the original's returned Bear sits in %s, want Battlefield", z)
	}
	bear1Opt := -1
	for _, o := range d.Options {
		if o.Obj == bear1 {
			bear1Opt = o.Index
		}
	}
	if bear1Opt < 0 {
		t.Fatalf("the copy's target ask did not offer the second graveyard Bear: %+v", d.Options)
	}
	submitChoices(t, eng, bear1Opt)

	// Drain the copy to resolution: it returns the second Bear. The copy is
	// itself a copy of Sevinne's Reclamation, so it too carries the copy
	// clause, but a copy was never cast (CR 707.10): the clause's
	// Card.wasCastFromGraveyard gate reads FALSE on the copy, so exactly the
	// first election fires and the returned nested-decline count must be zero.
	nestedDeclines := driveSevinneResolutionToEnd(t, eng, 40)

	if z := eng.G.Obj(bear1).Zone; z != state.ZBattlefield {
		t.Fatalf("the copied spell did not return the second Bear: it sits in %s, want Battlefield", z)
	}
	if copy := copyOnStack(eng); copy != 0 {
		t.Fatalf("the copy %d did not leave the stack", copy)
	}
	if nestedDeclines != 0 {
		t.Fatalf("the copy posed its own copy clause %d time(s): a copy was never cast, so Card.wasCastFromGraveyard must be false on it", nestedDeclines)
	}
	replayCheck(t, eng, cfg)
}
