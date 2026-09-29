package rules

// Set audit: Marvel's Spider-Man (spm). Most of this file is AUDIT-only: it
// records defects found in the set, one test per card/mechanic, each guarded
// by GORGE_SET_AUDIT so a failing finding does not break the gates. Findings
// are filed as tickets under .ds4/new-tickets/. See .ds4/report-t0.md. The
// Web-slinging finding is the exception: the alternative cost was implemented
// in rules/webslinging.go, so its test is a permanent green regression guard
// without the skip guard.

import (
	"os"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// requireSetAudit skips the calling test unless GORGE_SET_AUDIT is set. Use
// for a test that records a defect (fails on the correct behaviour).
func requireSetAudit(t *testing.T, why string) {
	t.Helper()
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip(why)
	}
}

// castOptionsWithMode returns the cast options for obj carrying the given
// alt-cost mode label.
func castOptionsWithMode(e *Engine, p state.PlayerID, obj state.ObjID, mode string) []decision.Option {
	var out []decision.Option
	for _, o := range e.legalActions(p) {
		if o.Kind == "cast" && o.Obj == obj && o.Mode == mode {
			out = append(out, o)
		}
	}
	return out
}

// TestSetAudit_spm_WebSlinging_AlternativeCostOffered: Web-slinging (CR
// 702.186a) is an alternative cost: "You may cast this spell for its
// web-slinging cost if you also return a tapped creature you control to its
// owner's hand." Spider-Man, Web-Slinger ({2}{W}, web-slinging {W}) must be
// castable for {W} while a tapped creature is available to return, even with
// no {2}{W} in the pool. The finding is implemented (rules/webslinging.go),
// so this is a permanent green regression guard without the skip guard.
func TestSetAudit_spm_WebSlinging_AlternativeCostOffered(t *testing.T) {
	t.Parallel()

	e := handEngine(t, corpusCard(t, "Spider-Man, Web-Slinger"))
	spidey := e.G.Zone(state.ZHand, 0)[0]
	if got := e.G.Obj(spidey).Face().Name; got != "Spider-Man, Web-Slinger" {
		t.Fatalf("setup: hand card = %q, want Spider-Man, Web-Slinger", got)
	}
	// Precondition: the parsed face carries the keyword (otherwise the
	// assertion below would be vacuous for a parsing reason, not the
	// missing mechanic).
	if !e.G.Obj(spidey).Face().HasKeyword("Web-slinging") {
		t.Fatalf("setup: parsed face lost Web-slinging: %v", e.G.Obj(spidey).Face().Keywords)
	}
	// A tapped creature of seat 0's, the return cost's fuel.
	bear := onBoard(t, e, 0, "Name:Test Tapped Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.Tap, Obj: bear})
	if !e.G.Obj(bear).Tapped {
		t.Fatal("setup: bear must be tapped to pay the return cost")
	}
	// Only {W}: the printed {2}{W} is unaffordable on purpose, so an offer
	// can only be the {W} web-slinging alternative.
	addMana(t, e, 0, "W")

	if opts := castOptionsWithMode(e, 0, spidey, "web-slinging"); len(opts) != 1 {
		t.Fatalf("web-slinging cast not offered with {W} and a tapped creature available: %+v", e.legalActions(0))
	}
}

// TestSetAudit_spm_Coverage_ImplementedPrimsRegistered: the coverage census
// (cards.Registry.Unsupported, which feeds `make report`, deck validation and
// cmd/cardfuzz eligibility) counts kw:Mayhem and stat:IgnoreLegendRule as
// unsupported primitives although the engine implements BOTH -- kw:Mayhem
// has its offer, charge, provenance flag and CastSa condition read
// (rules/legal.go, rules/altcast.go, rules/mayhem_test.go) and
// stat:IgnoreLegendRule is honoured by the CR 704.5j legend SBA
// (rules/sba.go, rules/ignorelegendrule_test.go). Neither name is ever passed
// to effects.RegisterNonAPI, so every carrier -- 12 Mayhem cards and
// Spider-Verse in this set alone -- is reported unplayable and excluded from
// fuzz/eligibility even though the engine plays it. The keyword behaviour
// itself is pinned by the existing suites; the defect is the census bookkeeping.
func TestSetAudit_spm_Coverage_ImplementedPrimsRegistered(t *testing.T) {
	t.Parallel()
	requireSetAudit(t, "set-audit finding (spm): kw:Mayhem and stat:IgnoreLegendRule are implemented but never registered with effects.RegisterNonAPI, so the census reports every carrier unplayable. Follow-up: register the implemented spm primitives with the coverage census")
	sup := effects.Supported()
	for _, p := range []string{"kw:Mayhem", "stat:IgnoreLegendRule"} {
		if !sup[p] {
			t.Errorf("effects.Supported() lacks %q although the engine implements it (mayhem_test.go / ignorelegendrule_test.go); the census counts every carrier unplayable", p)
		}
	}
}

// TestSetAudit_spm_Kraven_GreatestPowerDeathTrigger: Kraven the Hunter's
// death trigger is gated on "a creature an opponent controls WITH THE
// GREATEST POWER among creatures that player controls" dying, expressed in
// the script as CheckSVar over Count$ValidSelf
// Creature.greatestPowerControlledByCardController (GE1). The ValidSelf count
// head is unmodelled (cards/validate.go: count:ValidSelf), so the gate cannot
// be read and the trigger never fires (CR 603.4: the trigger condition is
// checked against the game state, not skipped). Big dies -> draw + counter;
// a smaller creature dying while a greater one is on the battlefield -> no
// trigger. OBSERVED: the gate is unread outright -- the trigger fires on
// EVERY opposing creature's death, so Kraven draws and grows off a 2/2's
// death too (the control half is where the test currently fails).
func TestSetAudit_spm_Kraven_GreatestPowerDeathTrigger(t *testing.T) {
	t.Parallel()
	requireSetAudit(t, "set-audit finding (spm): Kraven the Hunter's greatest-power death gate (Count$ValidSelf greatestPowerControlledByCardController) is unmodelled and the trigger fires on every opposing creature's death. Follow-up: implement the ValidSelf count head for Kraven the Hunter's greatest-power gate")
	e := layerEngine(t)
	kraven := onBoardCard(t, e, 0, corpusCard(t, "Kraven the Hunter"))
	if got := e.G.Obj(kraven).Face().Triggers[0].Mode; got != "ChangesZone" {
		t.Fatalf("setup: Kraven trigger mode = %q, want ChangesZone", got)
	}
	big := onBoard(t, e, 1, "Name:Big Beast\nManaCost:5\nTypes:Creature Beast\nPT:5/5\nOracle:x\n")
	small := onBoard(t, e, 1, "Name:Small Beast\nManaCost:1\nTypes:Creature Beast\nPT:2/2\nOracle:x\n")
	hand0 := len(e.G.Zone(state.ZHand, 0))

	// Control half: the SMALL creature dying while the 5/5 is still there is
	// not "a creature with the greatest power" -- no trigger.
	e.emit(events.Event{Kind: events.Damage, Obj: small, Amount: 99})
	e.checkStateBased()
	if got := e.G.Obj(small).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: small beast zone after lethal damage = %v, want graveyard", got)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("the 2/2's death fired Kraven while a 5/5 was the greatest power: stack %v", e.G.Stack)
	}

	// The 5/5 dying IS a greatest-power creature dying: draw one card and put
	// a +1/+1 counter on Kraven (CR 603.4 / the card's oracle text).
	e.emit(events.Event{Kind: events.Damage, Obj: big, Amount: 99})
	e.checkStateBased()
	if got := e.G.Obj(big).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: big beast zone after lethal damage = %v, want graveyard", got)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("Kraven's greatest-power death trigger did not fire (stack %v)", e.G.Stack)
	}
	e.resolveTop()
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0+1 {
		t.Fatalf("Kraven's trigger drew %d cards, want 1 (hand %d -> %d)", got-hand0, hand0, got)
	}
	if got := counterOf(t, e, kraven, "P1P1"); got != 1 {
		t.Fatalf("Kraven's P1P1 counters after his trigger = %d, want 1", got)
	}
}

// counterOf reads a counter kind off a battlefield object (the same shape the
// cloak/condition tests use).
func counterOf(t *testing.T, e *Engine, id state.ObjID, kind string) int {
	t.Helper()
	o := e.G.Obj(id)
	for _, c := range o.Counters {
		if c.Kind == kind {
			return int(c.N)
		}
	}
	return 0
}
