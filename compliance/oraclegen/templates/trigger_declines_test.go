package templates_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/rules"
)

// Fireglass Mentor's "Choose one of them" over the Dig'ed exiled cards: the
// level-B trigger item must make gorge take the first exiled card AND script
// XMage a target naming it, because XMage poses the ask as a mandatory
// TargetCardInExile 1..1 that rejects even the skip token ("Wrong skip
// command found - it can be used with up to targets, but used in
// TargetCardInExile, from 1 to 1").
func TestTriggerExileLookPickForcesFirstCard(t *testing.T) {
	reg := corpusReg(t)
	c, ok := reg.Lookup("Fireglass Mentor")
	if !ok {
		t.Fatal("precondition: Fireglass Mentor is not in the corpus")
	}
	var req levelb.Requirement
	found := false
	for _, r := range levelb.Requirements(c) {
		if r.Key == "trigger#0.0" {
			req, found = r, true
		}
	}
	if !found {
		t.Fatal("precondition: Fireglass Mentor has no trigger#0.0 requirement")
	}
	it, skip := templates.GenerateB(reg, "Fireglass Mentor", req)
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}

	// Precondition: the trigger fired and its effect chain really offers the
	// exile look ask gorge declined — the resolve step holds gorge's forced
	// choose answer of the first exiled card.
	gorgeAt := -1
	for i, s := range it.Scenario.Steps {
		for _, a := range s.Answers {
			if a.Kind == "choose" && len(a.Pick) == 1 && a.Pick[0] == "p0:Wastes#27" {
				gorgeAt = i
			}
		}
	}
	if gorgeAt < 0 {
		t.Fatalf("gorge answers do not take the first exiled card in any step: %+v", it.Scenario.Steps)
	}

	// XMage's scripted answer: a target naming the looked-at card, never a
	// skip — the mandatory 1..1 target rejects both skip tokens.
	var gotTarget, gotSkip bool
	for _, a := range it.XAnswers[gorgeAt] {
		if a.Seat == 0 && a.Kind == "target" && a.Value == "Wastes" {
			gotTarget = true
		}
		if a.Value == "[target_skip]" || a.Value == "[choice_skip]" {
			gotSkip = true
		}
	}
	if !gotTarget {
		t.Fatalf("xmage_answers %+v do not target the exiled card", it.XAnswers[gorgeAt])
	}
	if gotSkip {
		t.Fatalf("xmage_answers %+v still carry a skip", it.XAnswers[gorgeAt])
	}
}

// Whiskervale Forerunner's "you may reveal a creature card ... you may put
// it onto the battlefield": the declined one-card pick after the peek must
// be scripted as the lone target skip — XMage poses the ask without a
// chooseUse, so the pair's "no" boolean has nothing to answer ("Found wrong
// choice command"). The cast-side twin of the same decision shape (Break
// Out) keeps the measured pair; only the card-side-proven look chooser is
// re-scripted.
func TestTriggerLookedPickDeclineIsTargetSkipOnly(t *testing.T) {
	reg := corpusReg(t)
	c, ok := reg.Lookup("Whiskervale Forerunner")
	if !ok {
		t.Fatal("precondition: Whiskervale Forerunner is not in the corpus")
	}
	var req levelb.Requirement
	found := false
	for _, r := range levelb.Requirements(c) {
		if r.Key == "trigger#0.0" {
			req, found = r, true
		}
	}
	if !found {
		t.Fatal("precondition: Whiskervale Forerunner has no trigger#0.0 requirement")
	}
	it, skip := templates.GenerateB(reg, "Whiskervale Forerunner", req)
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	declined := 0
	for _, d := range res.Decisions {
		if d.Resume != "choice" || d.Options != 1 || d.Min != 0 || len(d.Picks) != 0 ||
			d.Step < 0 || d.Step >= len(it.XAnswers) {
			continue
		}
		declined++
		got := it.XAnswers[d.Step]
		if len(got) != 1 || got[0] != (oraclegen.XAnswer{Seat: 0, Kind: "target", Value: "[target_skip]"}) {
			t.Fatalf("looked-pick step %d answers %+v, want the lone target skip", d.Step, got)
		}
	}
	if declined == 0 {
		t.Fatal("precondition: the replay carries no declined one-card look pick")
	}
}

// Bebop & Rocksteady's "sacrifice a permanent unless you discard a card":
// the decline (the pay branch is unreachable, so gorge offered only
// "Don't pay") must be scripted as the target queue's skip — XMage poses the
// cost's own ask on the target queue and never a chooseUse, so the "no"
// boolean has nothing to answer ("Found wrong choice command").
func TestTriggerSacrificeUnlessDiscardDeclineIsTargetSkip(t *testing.T) {
	reg := corpusReg(t)
	c, ok := reg.Lookup("Bebop & Rocksteady")
	if !ok {
		t.Fatal("precondition: Bebop & Rocksteady is not in the corpus")
	}
	for _, key := range []string{"trigger#0.0", "trigger#0.1"} {
		var req levelb.Requirement
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Key == key {
				req, found = r, true
			}
		}
		if !found {
			t.Fatalf("precondition: Bebop & Rocksteady has no %s requirement", key)
		}
		it, skip := templates.GenerateB(reg, "Bebop & Rocksteady", req)
		if skip != nil {
			t.Fatalf("GenerateB(%s): %s", key, skip.Reason)
		}
		res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
		if err != nil {
			t.Fatalf("%s: replay: %v", key, err)
		}

		// Precondition: the replay really carries the declined unless-pay
		// ask (the pay branch unreachable, one option offered) whose script
		// this asserts — the step's only answer is the target skip, and no
		// "no" boolean remains for XMage to reject.
		declined := 0
		for _, d := range res.Decisions {
			if d.Resume != "unless_pay" || d.Options != 1 || len(d.Picks) != 1 ||
				d.Step < 0 || d.Step >= len(it.XAnswers) {
				continue
			}
			declined++
			got := it.XAnswers[d.Step]
			if len(got) != 1 || got[0] != (oraclegen.XAnswer{Seat: 0, Kind: "target", Value: "[target_skip]"}) {
				t.Fatalf("%s: unless step %d answers %+v, want the lone target skip", key, d.Step, got)
			}
			for _, a := range got {
				if a.Kind == "choice" && a.Value == "no" {
					t.Fatalf("%s: unless step %d still scripts the chooseUse no: %+v", key, d.Step, got)
				}
			}
		}
		if declined == 0 {
			t.Fatalf("%s: the replay carries no declined unless-pay ask", key)
		}
	}
}
