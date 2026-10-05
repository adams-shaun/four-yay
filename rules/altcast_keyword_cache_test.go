package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestBestowAndImpendingKeywordCachesAreReused(t *testing.T) {
	c := card(t, "Name:Keyword Cache Probe\nManaCost:2 R\nTypes:Creature Test\nPT:2/2\n"+
		"K:Bestow:4 G:GainControl\nK:Impending:3:5 R\nOracle:x\n")
	e := New(Config{Names: []string{"you"}, Decks: [][]*cards.Card{{c}}})
	f := c.Faces[0]
	ff := e.walkFaceFactsOf(f)
	if ff == nil || !ff.keywordsCurrent(f) {
		t.Fatal("precondition: face has no current compiled keyword facts")
	}
	if ff.altCostMask&(1<<altBestow) == 0 || ff.impendingCount != 3 {
		t.Fatalf("precondition: cache lacks bestow or impending data: mask=%#x count=%d", ff.altCostMask, ff.impendingCount)
	}

	bestow, ok := altCastModes[altBestow].faceCost(f, ff)
	if !ok {
		t.Fatal("precondition: compiled Bestow cost is absent")
	}
	impending := cachedImpendingCount(f, ff)

	// Prove subsequent reads are sidecar reads, not parser calls: the keyword
	// slice identity remains current, but make its source text unparseable.
	// Cached values stay stable; the builders now return their fail-closed
	// results if either reader regresses to reparsing.
	for i, keyword := range f.Keywords {
		switch cards.KeywordHead(keyword) {
		case "Bestow":
			f.Keywords[i] = "Bestow:not a cost"
		case "Impending":
			f.Keywords[i] = "Impending:not a count"
		}
	}
	if _, ok := bestowCost(f); ok {
		t.Fatal("test setup: mutated Bestow parameter unexpectedly parses")
	}
	if got := impendingCount(f); got != 0 {
		t.Fatalf("test setup: mutated Impending parameter parsed to %d", got)
	}
	for i := 0; i < 2; i++ {
		got, gotOK := altCastModes[altBestow].faceCost(f, ff)
		if !gotOK || got.Generic != bestow.Generic || got.Colored != bestow.Colored {
			t.Fatalf("cached Bestow read %d = (%+v, %v), want compiled (%+v, true)", i, got, gotOK, bestow)
		}
		if got := cachedImpendingCount(f, ff); got != impending {
			t.Fatalf("cached Impending read %d = %d, want %d", i, got, impending)
		}
	}
}
