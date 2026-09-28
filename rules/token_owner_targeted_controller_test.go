package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTokenOwnerTargetedControllerSurvivesSuspension is the regression for
// the target-controller LKI crossing a mid-resolution suspension (Generous
// Gift's Destroy then Token | TokenOwner$ TargetedController shape, with an
// ask inserted between them). The Destroy resets the victim's LIVE controller
// to its owner before the chained Token runs; the resumed frame rebuilds its
// Ctx from the live objects, so without the snapshot the token is minted for
// the caster/owner rather than the controller the target had at the start of
// resolution.
//
// Setup: the caster is seat 0; the victim is OWNED by seat 0 but CONTROLLED by
// seat 1 (a stolen permanent), so the pre-destruction controller (1), the
// post-destroy live controller/owner (0) and the caster (0) are all distinct
// enough to tell the three apart. The inserted ChoosePlayer ask forces the
// resolution to suspend, and the chained Token resolves only after the answer.
func TestTokenOwnerTargetedControllerSurvivesSuspension(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := New(Config{Seed: 741, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: reg.Tokens})

	// Victim owned by seat 0.
	victim := e.G.AddObject(card(t, "Name:Relic\nTypes:Artifact\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim.ID, From: state.ZLibrary, To: state.ZBattlefield})

	// Steal it to seat 1: owner 0, controller 1.
	steal := card(t, "Name:Steal\nTypes:Sorcery\nManaCost:1 R\nA:SP$ GainControl | ValidTgts$ Permanent\nOracle:x\n")
	effects.Resolve(e, &effects.Ctx{Controller: 1, Targets: []state.Target{{Obj: victim.ID}}, TargetsOffered: true},
		steal.Faces[0].SpellAbility())
	if o := e.G.Obj(victim.ID); o == nil || o.Zone != state.ZBattlefield || o.Owner != 0 || o.Controller != 1 {
		t.Fatalf("precondition: victim = %+v, want owner 0 / controller 1 on the battlefield", o)
	}

	// The Generous Gift chain, with a ChoosePlayer ask between Destroy and the
	// Token so the resolution suspends.
	gift := card(t, "Name:Generous Gift\nManaCost:2 W\nTypes:Instant\n"+
		"A:SP$ Destroy | Defined$ Targeted | ValidTgts$ Permanent | SubAbility$ DBChoose\n"+
		"SVar:DBChoose:DB$ ChoosePlayer | Choices$ Player | SubAbility$ DBToken\n"+
		"SVar:DBToken:DB$ Token | TokenScript$ g_3_3_elephant | TokenOwner$ TargetedController\n"+
		"Oracle:x\n")
	src := e.G.AddObject(gift, 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZStack})
	// The spell's chosen target lives on the stack object: a resumed frame
	// rebuilds its Ctx from o.Targets, so recording it here is what lets the
	// chained Token read the target (and its LKI) after the suspension.
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: src.ID, IDs: []state.ObjID{victim.ID}})
	head := gift.Faces[0].Abilities[0]
	if head.Sub == nil || head.Sub.Sub == nil {
		t.Fatalf("precondition: chain not linked: head.Sub=%v", head.Sub)
	}
	if head.Sub.API != "ChoosePlayer" || head.Sub.Sub.API != "Token" {
		t.Fatalf("precondition: chain = %s -> %s, want ChoosePlayer -> Token", head.Sub.API, head.Sub.Sub.API)
	}
	ctx := &effects.Ctx{Source: src.ID, Controller: 0, Targets: src.Targets,
		TargetsOffered: true, OfferedSA: head, SVars: gift.Faces[0].SVars}
	e.contChain = e.contChain[:0]
	e.repeatReported = nil
	effects.Resolve(e, ctx, head)
	if e.resume != nil {
		e.resume.outer = e.buildContinuationChain(e.contChain, src.ID, nil)
	}

	// The chain must actually have suspended on the ChoosePlayer ask: without
	// this the test passes trivially on the uninterrupted path.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" {
		t.Fatalf("expected the ChoosePlayer ask to suspend the chain, got %+v", d)
	}
	if o := e.G.Obj(victim.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: victim = %+v, want already destroyed before the ask", o)
	}

	// Answer the ask (any living player), which resumes the chain at the
	// ChoosePlayer SA and walks its Token sub-ability.
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)

	// The 3/3 Elephant belongs to the target's PRE-DESTRUCTION controller
	// (seat 1), never to the caster/owner (seat 0).
	if got := tokensNamed(e, 0, "Elephant"); got != 0 {
		t.Fatalf("caster/owner battlefield = %v, want no Elephant token", e.G.Zone(state.ZBattlefield, 0))
	}
	if got := tokensNamed(e, 1, "Elephant"); got != 1 {
		t.Fatalf("target controller battlefield = %v, want exactly one Elephant token", e.G.Zone(state.ZBattlefield, 1))
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unrecognized TokenOwner") {
			t.Fatalf("TargetedController emitted the fallback Note: %q", ev.Text)
		}
	}
}
