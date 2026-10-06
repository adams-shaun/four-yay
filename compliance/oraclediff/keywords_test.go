package oraclediff

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// kwPerm builds a battlefield permanent with the given printed keywords.
func kwPerm(name string, keywords ...string) rules.OracleSnapPerm {
	return rules.OracleSnapPerm{
		Ref: "p1:" + name, Name: name, Controller: 1, Owner: 1,
		PT: "2/2", Types: []string{"Bear", "Creature"}, Colors: "G",
		Keywords: append([]string(nil), keywords...),
	}
}

// kwSnap is one checkpoint holding a single permanent with keywords.
func kwSnap(cp string, keywords ...string) rules.OracleSnapshot {
	return rules.OracleSnapshot{
		Checkpoint: cp, Turn: 1, Step: "main1",
		Players: []rules.OracleSnapPlayer{
			{Seat: 0, Life: 20, Hand: []string{}, Graveyard: []string{}, LibraryCount: 39, LibraryTop: []string{"Wastes"}},
			{Seat: 1, Life: 20, Hand: []string{}, Graveyard: []string{}, LibraryCount: 38, LibraryTop: []string{"Shock"}},
		},
		Permanents: []rules.OracleSnapPerm{kwPerm("Grizzly Bears", keywords...)},
	}
}

// TestKeywordOptInComparesFlying pins the core of L10: two snapshots that
// differ only in a permanent's Flying agree without the opt-in and diverge
// on the permanents field with it. This test CAN fail: it asserts the two
// keyword lists actually differ before it asserts the opt-in behaviour.
func TestKeywordOptInComparesFlying(t *testing.T) {
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "Flying")}}
	x := XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup")}}

	// Precondition: the two keyword lists differ, so an opt-in that did
	// nothing would be the only reason the comparisons below could pass.
	if len(g.Snapshots[0].Permanents[0].Keywords) == len(x.Snapshots[0].Permanents[0].Keywords) {
		t.Fatalf("precondition: keyword lists must differ, both %v",
			g.Snapshots[0].Permanents[0].Keywords)
	}

	if v := Compare(g, nil, x); v.Status != Agree {
		t.Fatalf("without the opt-in Compare = %+v, want AGREE", v)
	}
	v := CompareOpts(g, nil, x, []string{CompareKeywords})
	if v.Status != Diverge || v.Field != "permanents" {
		t.Fatalf("with the keywords opt-in Compare = %+v, want DIVERGE on permanents", v)
	}
	if !strings.Contains(v.Gorge, "kw=flying") || strings.Contains(v.XMage, "kw=") {
		t.Fatalf("keyword diff is not rendered as the opted-in field: gorge %q xmage %q", v.Gorge, v.XMage)
	}
}

// TestKeywordOptInLeavesNoFieldOutputByteIdentical pins that the opt-in
// changes nothing when no fields are requested: Canonical, Freeze and Meets
// over a keyword-carrying snapshot are byte-identical to their nil-field
// forms, matching what main produced before L10.
func TestKeywordOptInLeavesNoFieldOutputByteIdentical(t *testing.T) {
	res := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		kwSnap("setup", "Flying", "First Strike"),
		kwSnap("step 0 (resolve)", "Flying", "First Strike"),
	}}

	// The nil-field output is the level-A contract, pinned by value (not
	// merely by comparing two calls that both changed together). Main's
	// permKeys omitted keywords entirely, so no "kw=" may appear.
	wantCanon := "== setup\n" +
		"turn: 1\n" +
		"step: main1\n" +
		"active: 0\n" +
		"over: false\n" +
		"players: 2\n" +
		"p0.life: 20\n" +
		"p0.counters: \n" +
		"p0.hand: []\n" +
		"p0.graveyard: []\n" +
		"p0.exile: []\n" +
		"p0.library_count: 39\n" +
		"p0.library_top: [Wastes]\n" +
		"p0.pool: \n" +
		"p1.life: 20\n" +
		"p1.counters: \n" +
		"p1.hand: []\n" +
		"p1.graveyard: []\n" +
		"p1.exile: []\n" +
		"p1.library_count: 38\n" +
		"p1.library_top: [Shock]\n" +
		"p1.pool: \n" +
		"permanents: c1 o1 Grizzly Bears [bear creature] {G} 2/2\n" +
		"stack: []\n" +
		"== step 0 (resolve)\n" +
		"turn: 1\n" +
		"step: main1\n" +
		"active: 0\n" +
		"over: false\n" +
		"players: 2\n" +
		"p0.life: 20\n" +
		"p0.counters: \n" +
		"p0.hand: []\n" +
		"p0.graveyard: []\n" +
		"p0.exile: []\n" +
		"p0.library_count: 39\n" +
		"p0.library_top: [Wastes]\n" +
		"p0.pool: \n" +
		"p1.life: 20\n" +
		"p1.counters: \n" +
		"p1.hand: []\n" +
		"p1.graveyard: []\n" +
		"p1.exile: []\n" +
		"p1.library_count: 38\n" +
		"p1.library_top: [Shock]\n" +
		"p1.pool: \n" +
		"permanents: c1 o1 Grizzly Bears [bear creature] {G} 2/2\n" +
		"stack: []\n"
	if got := Canonical(res.Snapshots); got != wantCanon {
		t.Fatalf("Canonical (no fields) changed:\ngot:\n%s\nwant:\n%s", got, wantCanon)
	}
	if got := CanonicalOpts(res.Snapshots, nil); got != wantCanon {
		t.Fatalf("CanonicalOpts(nil) differs from Canonical:\ngot:\n%s", got)
	}
	if strings.Contains(Canonical(res.Snapshots), "kw=") {
		t.Fatal("level-A Canonical grew a kw= field")
	}

	// The opted-in form DOES carry the field, so the byte-identity above is
	// a real opt-in boundary, not a no-op.
	if got := CanonicalOpts(res.Snapshots, []string{CompareKeywords}); !strings.Contains(got, "kw=first strike,flying") {
		t.Fatalf("keywords opt-in did not add kw=:\n%s", got)
	}

	// Freeze and Meets are byte-identical without the opt-in.
	fzNil := Freeze(res)
	fzEmpty := FreezeOpts(res, nil)
	if len(fzNil) != len(fzEmpty) {
		t.Fatalf("FreezeOpts(nil) field count %d != Freeze %d", len(fzEmpty), len(fzNil))
	}
	for i := range fzNil {
		if fzNil[i] != fzEmpty[i] {
			t.Fatalf("FreezeOpts(nil)[%d] = %+v != Freeze %+v", i, fzEmpty[i], fzNil[i])
		}
	}
	// A frozen keyword field can only arise from a keyword CHANGE after
	// setup; here the keyword set is constant, so none may appear.
	for _, f := range fzNil {
		if strings.Contains(f.Value, "kw=") {
			t.Fatalf("Freeze grew a kw= field with no opt-in: %+v", f)
		}
	}
	if ok, why := Meets(fzNil, res); !ok {
		t.Fatalf("Meets (no fields) = false: %s", why)
	}
	if ok, why := MeetsOpts(fzNil, res, nil); !ok {
		t.Fatalf("MeetsOpts(nil) = false: %s", why)
	}
}

// TestKeywordVocabularyFoldsAndFilters pins the fold and the evergreen
// intersection: gorge's "First Strike" and XMage's "First strike" reach the
// same key, and a keyword outside the evergreen set (Ward:2) is ignored.
func TestKeywordVocabularyFoldsAndFilters(t *testing.T) {
	// gorge spells it "First Strike"; XMage spells it "First strike".
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "First Strike", "Flying")}}
	x := XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "First strike", "flying")}}
	if v := CompareOpts(g, nil, x, []string{CompareKeywords}); v.Status != Agree {
		t.Fatalf("case/word variants did not fold: %+v", v)
	}

	// Precondition for the next block: the lists do differ textually, so the
	// agreement above is a fold, not a coincidence of equal input.
	if g.Snapshots[0].Permanents[0].Keywords[0] == x.Snapshots[0].Permanents[0].Keywords[0] {
		t.Fatal("precondition: glossy/xmage spellings must differ")
	}

	// A non-evergreen keyword is ignored on both sides.
	gWard := rules.OracleResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "First Strike", "Ward:2")}}
	xWard := XResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "First strike", "Ward:3")}}
	if v := CompareOpts(gWard, nil, xWard, []string{CompareKeywords}); v.Status != Agree {
		t.Fatalf("non-evergreen keyword was not ignored: %+v", v)
	}
	if got := CanonicalOpts(gWard.Snapshots, []string{CompareKeywords}); strings.Contains(got, "ward") {
		t.Fatalf("non-evergreen keyword reached the key:\n%s", got)
	}

	// A permanent with no evergreen keyword omits the field entirely.
	bare := kwSnap("setup", "Ward:2")
	if got := CanonicalOpts([]rules.OracleSnapshot{bare}, []string{CompareKeywords}); strings.Contains(got, "kw=") {
		t.Fatalf("a permanent with no evergreen keyword still produced kw=:\n%s", got)
	}
}

// TestKeywordCompareOptionVocabularyIsValidated pins that the one legal
// Compare value is accepted and anything else is rejected.
func TestKeywordCompareOptionVocabularyIsValidated(t *testing.T) {
	if err := ValidateCompare(nil); err != nil {
		t.Fatalf("nil compare rejected: %v", err)
	}
	if err := ValidateCompare([]string{CompareKeywords}); err != nil {
		t.Fatalf("%q rejected: %v", CompareKeywords, err)
	}
	if err := ValidateCompare([]string{"keywords", "bogus"}); err == nil {
		t.Fatal("an unknown compare field was accepted")
	}
	if err := ValidateCompare([]string{"Keywords"}); err == nil {
		t.Fatal("a case variant of the one legal value was accepted")
	}
}
