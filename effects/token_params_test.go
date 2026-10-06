package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestTokenKnownKeysSorted: the unread lookup binary-searches the table.
func TestTokenKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(tokenKnownKeys[:]) ||
		len(slices.Compact(slices.Clone(tokenKnownKeys[:]))) != len(tokenKnownKeys) {
		t.Fatal("tokenKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileToken pins the compiled shapes the resolution reads: the raw
// Num-grammar values, the split script list, the exact-spelling remember
// flag, the trimmed selectors and riders, and the unread report.
func TestCompileToken(t *testing.T) {
	sa := &cards.SA{API: "Token", Params: map[string]string{
		"TokenAmount": " X ", "TokenOwner": " Opponent", "TokenScript": " w_1_1_soldier, ,c_a_treasure_sac ",
		"RememberOriginalTokens": "True", "AttachedTo": " Targeted ", "TokenPower": "+2", "TokenToughness": "Y",
		"WithCountersType": " P1P1 ", "WithCountersAmount": " X ", "PumpKeywords": "Menace & Haste",
		"PumpDuration": " UntilYourNextTurn ", "TokenTapped": " true ", "TokenRemembered": " ExiledCards ",
		"TokenAttacking": " True ", "ImprintTokens": "TRUE", "Hidden": "True",
	}}
	p := TokenOf(sa)
	if p.Amount != (ParamText{Text: " X ", Present: true}) || p.Owner != " Opponent" ||
		!slices.Equal(p.Scripts, []string{"w_1_1_soldier", "c_a_treasure_sac"}) || !p.Remember ||
		p.AttachedTo != "Targeted" || p.Power != (ParamText{Text: "+2", Present: true}) ||
		p.Toughness != (ParamText{Text: "Y", Present: true}) || p.WithCountersType != "P1P1" ||
		p.WithCountersAmount != (ParamText{Text: " X ", Present: true}) ||
		!slices.Equal(p.PumpKeywords, []string{"Menace", "Haste"}) || p.PumpDuration != "UntilYourNextTurn" ||
		!p.Tapped || p.Remembered != "ExiledCards" || p.Attacking != "True" || !p.ImprintTokens {
		t.Fatalf("shape = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if TokenOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	def := TokenOf(&cards.SA{API: "Token", Params: map[string]string{"TokenScript": "c_a_clue_draw",
		"RememberTokens": "true"}})
	if def.Amount.Present || def.Owner != "" || def.Remember || def.Power.Present || def.Toughness.Present ||
		def.WithCountersAmount.Present || def.PumpKeywords != nil || def.Tapped || def.ImprintTokens ||
		def.Attacking != "" || def.Remembered != "" || len(def.Unread) != 0 {
		t.Fatalf("defaults = %+v", def)
	}
}

// TestTokenOfIsAllocationFree: a configured record or a front-cache hit
// allocates nothing.
func TestTokenOfIsAllocationFree(t *testing.T) {
	bound := slottedSA(t, "Token", map[string]string{"TokenScript": "c_a_treasure_sac", "TokenAmount": "2"})
	f := NewSAFacts(bound)
	f.Publish()
	if LoadSAFacts(bound) != f {
		t.Fatal("precondition: the configured record is not published on bound's facts slot")
	}
	cached := &cards.SA{API: "Token", Params: map[string]string{"TokenScript": "w_1_1_soldier"}}
	TokenOf(cached)
	if n := allocsPerRun(100, func() {
		_ = TokenOf(bound)
		_ = TokenOf(cached)
	}); n != 0 {
		t.Fatalf("TokenOf allocated %v objects per run; want 0", n)
	}
	if f.Token == nil || !f.Token.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the Token half")
	}
}
