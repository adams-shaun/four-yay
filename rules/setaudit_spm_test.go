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
// (rules/sba.go, rules/ignorelegendrule_test.go). Both names are now passed to
// effects.RegisterNonAPI (rules/cast.go's alt-cost family init and
// rules/statics.go's stat init), so the census counts their carriers playable.
// This is the permanent green guard: reverting either registration fails it.
func TestSetAudit_spm_Coverage_ImplementedPrimsRegistered(t *testing.T) {
	t.Parallel()
	sup := effects.Supported()
	for _, p := range []string{"kw:Mayhem", "stat:IgnoreLegendRule"} {
		if !sup[p] {
			t.Errorf("effects.Supported() lacks %q although the engine implements it (mayhem_test.go / ignorelegendrule_test.go); the census counts every carrier unplayable", p)
		}
	}
}
