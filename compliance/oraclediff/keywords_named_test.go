package oraclediff

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestKeywordNamedVocabulary pins the second, opt-in vocabulary: a permanent
// keyword ability outside the evergreen set folds to its NAME, so gorge's
// "Ward:PayLife<2>" and XMage's driver string "ward&mdash;Pay 2 life." agree
// under CompareKeywordsNamed. It can fail: it asserts the two spellings
// actually differ, then that a Ward on one side only diverges, so a fold that
// dropped everything could not pass.
func TestKeywordNamedVocabulary(t *testing.T) {
	// Precondition: gorge and XMage spell the same Ward differently, so the
	// agreement below is a fold, not equal input.
	gorgeText := "Ward:PayLife<2>"
	xmageText := "ward&mdash;Pay 2 life."
	if gorgeText == xmageText {
		t.Fatal("precondition: the two Ward spellings must differ")
	}

	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", gorgeText, "Prowess")}}
	x := XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", xmageText, "Prowess")}}
	if v := CompareOpts(g, nil, x, []string{CompareKeywordsNamed}); v.Status != Agree {
		t.Fatalf("named fold of %q vs %q = %+v, want AGREE", gorgeText, xmageText, v)
	}

	// The name is what is compared: Ward:1 on gorge and "ward {2}" on XMage
	// fold to the same "ward" and agree (the parameter is deliberately not
	// compared by this vocabulary).
	gOne := rules.OracleResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "Ward:1")}}
	xTwo := XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "ward {2}")}}
	if v := CompareOpts(gOne, nil, xTwo, []string{CompareKeywordsNamed}); v.Status != Agree {
		t.Fatalf("named fold of Ward:1 vs ward {2} = %+v, want AGREE", v)
	}
	if got := CanonicalOpts(gOne.Snapshots, []string{CompareKeywordsNamed}); !strings.Contains(got, "kw=ward") {
		t.Fatalf("named Ward did not render as kw=ward:\n%s", got)
	}

	// A Ward on one side only diverges: the vocabulary compares presence.
	xNone := XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup")}}
	if v := CompareOpts(gOne, nil, xNone, []string{CompareKeywordsNamed}); v.Status != Diverge || v.Field != "permanents" {
		t.Fatalf("Ward:1 vs no ward = %+v, want DIVERGE on permanents", v)
	}

	// Plain keywords still ignore Ward on both sides: the wider vocabulary is
	// strictly opt-in.
	if v := CompareOpts(g, nil, x, []string{CompareKeywords}); v.Status != Agree {
		t.Fatalf("plain keywords treated Ward as a difference: %+v", v)
	}
	if got := CanonicalOpts(g.Snapshots, []string{CompareKeywords}); strings.Contains(got, "ward") {
		t.Fatalf("plain keywords carried ward into the key:\n%s", got)
	}

	// The named fold for gorge cuts at the first ':' and for XMage at the
	// entity / brace / digit; First strike keeps its word form.
	gFire := rules.OracleResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "Firebending:2", "First Strike")}}
	xFire := XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "firebending 2", "First strike")}}
	if v := CompareOpts(gFire, nil, xFire, []string{CompareKeywordsNamed}); v.Status != Agree {
		t.Fatalf("named fold of Firebending/first strike = %+v, want AGREE", v)
	}
	if got := CanonicalOpts(gFire.Snapshots, []string{CompareKeywordsNamed}); !strings.Contains(got, "kw=firebending,first strike") {
		t.Fatalf("named key = %q, want kw=firebending,first strike", got)
	}

	// Flash is a plain word in the named vocabulary: gorge "Flash" and XMage
	// "flash" agree, flash on one side only diverges, and the key renders
	// "kw=flash". The plain evergreen fold still ignores it.
	gFlash := rules.OracleResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "Flash")}}
	xFlash := XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "flash")}}
	if gorgeFlash, xmageFlash := foldKeyword("Flash"), foldKeyword("flash"); gorgeFlash != "flash" || xmageFlash != "flash" || gorgeFlash != xmageFlash {
		t.Fatalf("precondition: Flash spellings fold to %q/%q, want the same \"flash\"", gorgeFlash, xmageFlash)
	}
	if v := CompareOpts(gFlash, nil, xFlash, []string{CompareKeywordsNamed}); v.Status != Agree {
		t.Fatalf("named fold of Flash vs flash = %+v, want AGREE", v)
	}
	if v := CompareOpts(gFlash, nil, XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup")}}, []string{CompareKeywordsNamed}); v.Status != Diverge || v.Field != "permanents" {
		t.Fatalf("Flash vs no flash = %+v, want DIVERGE on permanents", v)
	}
	if got := CanonicalOpts(gFlash.Snapshots, []string{CompareKeywordsNamed}); !strings.Contains(got, "kw=flash") {
		t.Fatalf("named Flash did not render as kw=flash:\n%s", got)
	}
	if v := CompareOpts(gFlash, nil, xFlash, []string{CompareKeywords}); v.Status != Agree {
		t.Fatalf("plain keywords treated Flash as a difference: %+v", v)
	}
	if got := CanonicalOpts(gFlash.Snapshots, []string{CompareKeywords}); strings.Contains(got, "flash") {
		t.Fatalf("plain keywords carried flash into the key:\n%s", got)
	}

	// Hexproof-from never folds onto plain hexproof: gorge's
	// quality-parameterised "Hexproof:CardColors" is dropped, while XMage's
	// plain "hexproof" folds as the evergreen keyword, so the two diverge.
	gHex := rules.OracleResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "Hexproof:CardColors")}}
	xHex := XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "hexproof")}}
	if v := CompareOpts(gHex, nil, xHex, []string{CompareKeywordsNamed}); v.Status != Diverge {
		t.Fatalf("hexproof-from folded onto hexproof: %+v", v)
	}
	if got := CanonicalOpts(gHex.Snapshots, []string{CompareKeywordsNamed}); strings.Contains(got, "hexproof") {
		t.Fatalf("hexproof-from reached the named key:\n%s", got)
	}
}

// TestKeywordNamedOptionIsValidated pins that the second vocabulary is in the
// closed option list and that a case variant is still rejected.
func TestKeywordNamedOptionIsValidated(t *testing.T) {
	if err := ValidateCompare([]string{CompareKeywordsNamed}); err != nil {
		t.Fatalf("%q rejected: %v", CompareKeywordsNamed, err)
	}
	if err := ValidateCompare([]string{CompareKeywords, CompareKeywordsNamed}); err != nil {
		t.Fatalf("both keyword vocabularies rejected: %v", err)
	}
	if err := ValidateCompare([]string{"Keywords_Named"}); err == nil {
		t.Fatal("a case variant of the named vocabulary was accepted")
	}
}
