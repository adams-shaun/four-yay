package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The old count-bound deviation (AGENTS.md "Known approximations"): the
// repeatable-cost count ask (replicateAsk and its multikicker/squad siblings)
// priced each candidate over the RAW composed cost through castable, which
// skips every CR 601.2f RaiseCost/ReduceCost static, and against the floating
// pool alone, so a Convoke/Improvise contribution announced after the count
// could not extend it. countCandPayable now composes the candidate through
// manaToPay (the charge the payment makes) and folds in the
// Convoke/Harmonize/Improvise reduction the later announcement can still
// make. These tests pin both halves on real corpus cards.

// TestReplicateCountBoundSeesReduceCostModifier: Pyromatics ({1}{R},
// K:Replicate:1 R) with Baral, Chief of Compliance on the battlefield
// ("Instant and sorcery spells you cast cost {1} less to cast") and a pool of
// four red and three colourless. The pool affords three payments only with
// Baral's reduction: the raw total for N payments is 2N+2 (so a seventh unit
// is short of N=3), the composed total 2N+1 (N=3 costs exactly seven). The
// pre-fix bound priced the raw cost and offered 0..2.
func TestReplicateCountBoundSeesReduceCostModifier(t *testing.T) {
	t.Parallel()
	e, cfg, reg := replicateTapEngine(t, "Pyromatics", "Baral, Chief of Compliance")
	baralCard := searchCorpusCard(t, reg, "Baral, Chief of Compliance")
	baral := moveSeededCard(t, e, 0, baralCard, state.ZBattlefield)

	// Precondition the reduction depends on: Baral is on the battlefield, so
	// his ReduceCost static is active for seat 0's instant/sorcery spells.
	if o := e.G.Obj(baral); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Baral not on the battlefield: %s", o.Zone)
	}
	hero := searchMoveByName(t, e, "Pyromatics", state.ZHand)
	addMana(t, e, 0, "RRRRCCC")
	opt := replicateOption(t, e, hero, "replicated")
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the replicate count decision, got %+v", d)
	}
	// The precondition the assertion depends on: the composed charge reaches
	// 3. Priced over the RAW cost the same seven units afford only 2, so a
	// bound that ignores Baral offers exactly 3 options (0..2).
	if len(d.Options) != 4 || d.Options[3].Amount != 3 {
		t.Fatalf("count ask did not see Baral's ReduceCost (want 0..3): %+v", d)
	}
	chooseReplicate(t, e, 3)

	// Pyromatics targets any: aim the opponent.
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
		t.Fatalf("seat 1 not offered: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 30)

	if !logHasFlag(e, hero, "replicated") {
		t.Fatal("the pay-time CastInfo carries no replicated flag")
	}
	copies := 0
	for _, o := range e.G.Objs {
		if o.IsCopy {
			copies++
		}
	}
	if copies != 3 {
		t.Fatalf("%d copies, want 3 (the reduction-extended count)", copies)
	}
	replayCheck(t, e, cfg)
}

// TestReplicateCountBoundSeesImproviseContribution: Pyromatics with Ironheart,
// Clever Champion ("Noncreature spells you cast have improvise") and a second
// artifact on the battlefield, against a pool of three red and one colourless.
// The pool alone affords the replicated-mode OFFER (base {1}{R} + one
// {1}{R} = {2}{R}{R}, four units) and exactly one payment in the count ask;
// improvise taps the two artifacts for two generic, so N=2 costs
// {3}{R}{R}{R} - {2} = {1}{R}{R}{R}, four units, and is payable. The pre-fix
// count bound offered 0..1.
func TestReplicateCountBoundSeesImproviseContribution(t *testing.T) {
	t.Parallel()
	e, cfg, reg := replicateTapEngine(t, "Pyromatics",
		"Ironheart, Clever Champion", "Ornithopter")
	ironheart := moveSeededCard(t, e, 0, searchCorpusCard(t, reg, "Ironheart, Clever Champion"), state.ZBattlefield)
	thopter := moveSeededCard(t, e, 0, searchCorpusCard(t, reg, "Ornithopter"), state.ZBattlefield)
	// Precondition the improvise credit depends on: both artifacts are
	// untapped on the battlefield, so both can help pay.
	for _, id := range []state.ObjID{ironheart, thopter} {
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield || o.Tapped {
			t.Fatalf("precondition: artifact %d zone=%s tapped=%v", id, o.Zone, o.Tapped)
		}
	}

	hero := searchMoveByName(t, e, "Pyromatics", state.ZHand)
	addMana(t, e, 0, "RRRC") // three red and one colourless: one payment, plus improvise
	opt := replicateOption(t, e, hero, "replicated")
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the replicate count decision, got %+v", d)
	}
	// Pool-only the same four units afford N=1; the two artifacts' improvise
	// generic reaches N=2.
	if len(d.Options) != 3 || d.Options[2].Amount != 2 {
		t.Fatalf("count ask did not see the improvise contribution (want 0..2): %+v", d)
	}
	chooseReplicate(t, e, 2)

	// CR 702.66a: the improvise announcement taps both artifacts for the two
	// generic the replicated total still needs.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the improvise announcement, got %+v", d)
	}
	var taps []int
	for _, o := range d.Options {
		if o.Kind == "improvise_generic" && (o.Obj == ironheart || o.Obj == thopter) {
			taps = append(taps, o.Index)
		}
	}
	if len(taps) != 2 {
		t.Fatalf("both artifacts must be offered for improvise: %+v", d.Options)
	}
	submitChoices(t, e, taps...)

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
		t.Fatalf("seat 1 not offered: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 30)

	if !e.G.Obj(ironheart).Tapped || !e.G.Obj(thopter).Tapped {
		t.Fatalf("improvise did not tap both artifacts (%v, %v)",
			e.G.Obj(ironheart).Tapped, e.G.Obj(thopter).Tapped)
	}
	if !logHasFlag(e, hero, "replicated") {
		t.Fatal("the pay-time CastInfo carries no replicated flag")
	}
	copies := 0
	for _, o := range e.G.Objs {
		if o.IsCopy {
			copies++
		}
	}
	if copies != 2 {
		t.Fatalf("%d copies, want 2 (the improvise-extended count)", copies)
	}
	replayCheck(t, e, cfg)
}
