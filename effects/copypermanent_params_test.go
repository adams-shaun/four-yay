package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestCopyPermanentKnownKeysSorted: the unread lookup binary-searches the
// table.
func TestCopyPermanentKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(copyPermanentKnownKeys[:]) || len(slices.Compact(slices.Clone(copyPermanentKnownKeys[:]))) != len(copyPermanentKnownKeys) {
		t.Fatal("copyPermanentKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileCopyPermanent pins the compiled shapes the resolution reads: the
// skipped-family Note and its blocking verdict, the Zndrsplt choice shape,
// the modification and rider texts, and the unread report.
func TestCompileCopyPermanent(t *testing.T) {
	sa := &cards.SA{API: "CopyPermanent", Params: map[string]string{
		"Defined": " Targeted ", "ValidTgts": "Creature", "WithDifferentNames": "True", "AtEOT": " Exile ",
		"AtEOTTrig": "Sacrifice", "SetPower": "X", "SetCreatureTypes": "Frog", "RemoveSubTypes": "True",
		"NumCopies": "2", "AddKeywords": "Flying & Haste", "PumpDuration": " EOT ", "WithCountersType": " P1P1 ",
		"TokenTapped": "True", "TokenAttacking": "True", "Populate": "true", "RememberTokens": "True",
		"ImprintTokens": "True", "AttachedTo": " Remembered ", "AddTriggers": "TrigA,TrigB",
		"Controller": " You ", "Hidden": "True",
	}}
	p := CopyPermanentOf(sa)
	if p.Blocked || p.SkippedNote != "CopyPermanent does not implement WithDifferentNames$; the copy keeps the original's printed characteristics" {
		t.Fatalf("skipped = %q blocked=%v", p.SkippedNote, p.Blocked)
	}
	if p.Defined != "Targeted" || !p.HasTgts || !p.Populate || !p.RememberTokens || !p.ImprintTokens ||
		p.AttachedTo != "Remembered" || p.AddTriggers != "TrigA,TrigB" || p.Controller != "You" {
		t.Fatalf("selection = %+v", p)
	}
	if p.AtEOT != "Exile" || p.AtEOTTrig != "Sacrifice" || p.SetPower != (ParamText{Text: "X", Present: true}) ||
		p.SetCreatureTypes != (ParamText{Text: "Frog", Present: true}) || !p.RemoveSubTypes.Present ||
		p.NumCopies != (ParamText{Text: "2", Present: true}) || !p.AddKeywordsPresent || p.AddKeywords != "Flying & Haste" ||
		p.PumpDuration != "EOT" || p.WithCountersType != "P1P1" || p.WithCountersAmount.Present ||
		p.TokenTapped != "True" || p.TokenAttacking != "True" {
		t.Fatalf("riders = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if CopyPermanentOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	zn := CopyPermanentOf(&cards.SA{API: "CopyPermanent", Params: map[string]string{
		"Choices": "Creature.RememberedPlayerCtrl", "Chooser": "Remembered", "Controller": "Remembered"}})
	if !zn.SupportsChoice || zn.Blocked || zn.SkippedNote != "" {
		t.Fatalf("Zndrsplt shape = %+v", zn)
	}
	bl := CopyPermanentOf(&cards.SA{API: "CopyPermanent", Params: map[string]string{
		"DefinedName": "X", "Choices": "Creature", "Chooser": "You"}})
	// DefinedName$ is now CONSUMED (the copy is the named card from the
	// corpus); this shape still blocks because its Choices$/Chooser$ pair is
	// not the supported Zndrsplt form. The skipped Note therefore no longer
	// names DefinedName$.
	if !bl.Blocked || bl.SupportsChoice || bl.DefinedName != "X" ||
		bl.SkippedNote != "CopyPermanent does not implement Choices$, Chooser$; the copy keeps the original's printed characteristics" {
		t.Fatalf("blocked shape = %+v", bl)
	}
}

// TestCopyPermanentOfIsAllocationFree: a configured record or a front-cache
// hit allocates nothing.
func TestCopyPermanentOfIsAllocationFree(t *testing.T) {
	bound := slottedSA(t, "CopyPermanent", map[string]string{"Defined": "Self", "NumCopies": "1"})
	f := NewSAFacts(bound)
	f.Publish()
	if LoadSAFacts(bound) != f {
		t.Fatal("precondition: the configured record is not published on bound's facts slot")
	}
	cached := &cards.SA{API: "CopyPermanent", Params: map[string]string{"Defined": "Remembered"}}
	CopyPermanentOf(cached)
	if n := allocsPerRun(100, func() {
		_ = CopyPermanentOf(bound)
		_ = CopyPermanentOf(cached)
	}); n != 0 {
		t.Fatalf("CopyPermanentOf allocated %v objects per run; want 0", n)
	}
	if f.CopyPermanent == nil || !f.CopyPermanent.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the CopyPermanent half")
	}
}
