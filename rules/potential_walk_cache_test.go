package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestPotentialWalkSharedAcrossReaders pins the posed decision's shared
// potential walk (potential_walk_cache.go): once a full-walk reader has asked
// on the engine, the offer builder, the PotentialActions projection and
// PotentialPaymentPlans together open exactly ONE legal-action walk per
// decision, and each answers exactly what an engine that never cached
// answers (a Clone, whose cache starts empty, at every read).
func TestPotentialWalkSharedAcrossReaders(t *testing.T) {
	prevPriced, prevCasts, prevWalk := pricedCandidatesVerify, castsOnlyWalkVerify, walkCacheVerify
	pricedCandidatesVerify, castsOnlyWalkVerify, walkCacheVerify = false, false, false
	defer func() { pricedCandidatesVerify, castsOnlyWalkVerify, walkCacheVerify = prevPriced, prevCasts, prevWalk }()
	e := onePassBenchBoard(t)
	fresh := func() *Engine { return e.Clone() }
	wantActs := fresh().PotentialActions(0)
	wantPlans := fresh().PotentialPaymentPlans(0)

	// First decision: no full-walk reader has asked yet, so the builder runs
	// its own casts-only walk and the projection then needs the full one.
	e.EnsurePaymentActions()
	walks := e.legalActionWalks
	if got := e.PotentialActions(0); !reflect.DeepEqual(got, wantActs) {
		t.Fatalf("PotentialActions = %+v, want %+v", got, wantActs)
	}
	if got := e.legalActionWalks - walks; got != 1 {
		t.Fatalf("first projection opened %d walks, want 1 (the full walk)", got)
	}
	walks = e.legalActionWalks
	if got := e.PotentialPaymentPlans(0); !reflect.DeepEqual(got, wantPlans) {
		t.Fatalf("PotentialPaymentPlans = %+v, want %+v", got, wantPlans)
	}
	// The planner's own witness confirmations walk other pools; only the
	// PotentialMana walk is shared, so compare against a cold engine's count.
	c := fresh()
	cw := c.legalActionWalks
	c.PotentialPaymentPlans(0)
	if got, cold := e.legalActionWalks-walks, c.legalActionWalks-cw; got != cold-1 {
		t.Fatalf("PotentialPaymentPlans opened %d walks after the projection, want %d (a cold engine's %d less the shared one)", got, cold-1, cold)
	}

	// Next decision at the same board: the builder now runs the full walk,
	// and the projection and planner are served it.
	onePassReask(e, 0)
	want := fresh().PaymentActionsForPriority(0, e.Pending().Seq)
	walks = e.legalActionWalks
	got := e.EnsurePaymentActions()
	if n := e.legalActionWalks - walks; n != 1 {
		t.Fatalf("builder opened %d walks, want 1", n)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payment actions = %+v, want %+v", got, want)
	}
	walks = e.legalActionWalks
	if got := e.PotentialActions(0); !reflect.DeepEqual(got, wantActs) {
		t.Fatalf("served PotentialActions = %+v, want %+v", got, wantActs)
	}
	if n := e.legalActionWalks - walks; n != 0 {
		t.Fatalf("projection after the builder opened %d walks, want 0", n)
	}

	// A direct pool write (no event) misses rather than serving stale.
	e.G.Players[0].Pool[state.ManaIndex('R')] = 1
	cold := fresh().PotentialActions(0)
	if got := e.PotentialActions(0); !reflect.DeepEqual(got, cold) {
		t.Fatalf("after a pool write PotentialActions = %+v, want %+v", got, cold)
	}
}
