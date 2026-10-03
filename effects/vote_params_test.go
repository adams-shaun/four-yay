package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestVoteKnownKeysSorted: the unread lookup binary-searches the table.
func TestVoteKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(voteKnownKeys[:]) ||
		len(slices.Compact(slices.Clone(voteKnownKeys[:]))) != len(voteKnownKeys) {
		t.Fatal("voteKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileVote pins the compiled shapes the resolution reads: the three
// ballot selectors, the split option list, the True riders, the trimmed
// SVar names and prompt, and the unread report.
func TestCompileVote(t *testing.T) {
	sa := &cards.SA{API: "Vote", Params: map[string]string{
		"VoteCard": " Permanent.nonLand+YouDontCtrl ", "VotePlayer": " other ", "Choices": " DBA, ,DBB ",
		"Secretly": " true ", "StoreVoteNum": "True", "UpTo": "TRUE", "RememberVotedObjects": "True",
		"VoteTiedAbility": " DBTie ", "VoteSubAbility": " DBExile ", "VoteMessage": " Vote now ", "Hidden": "True",
	}}
	p := VoteOf(sa)
	if p.Card != "Permanent.nonLand+YouDontCtrl" || p.Player != "other" || !p.PlayerOther ||
		!slices.Equal(p.Choices, []string{"DBA", "DBB"}) || !p.Secretly || !p.StoreVoteNum || !p.UpTo ||
		!p.RememberVoted || p.TiedAbility != "DBTie" || p.SubAbility != "DBExile" || p.Message != "Vote now" {
		t.Fatalf("shape = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if VoteOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	def := VoteOf(&cards.SA{API: "Vote", Params: map[string]string{"Choices": " ", "VotePlayer": "Player"}})
	if def.Choices != nil || def.PlayerOther || def.Player != "Player" || def.Secretly || def.StoreVoteNum ||
		def.UpTo || def.RememberVoted || def.Card != "" || def.Message != "" || len(def.Unread) != 0 {
		t.Fatalf("defaults = %+v", def)
	}
}

// TestVoteOfIsAllocationFree: a configured record or a front-cache hit
// allocates nothing.
func TestVoteOfIsAllocationFree(t *testing.T) {
	bound := &cards.SA{API: "Vote", Params: map[string]string{"Choices": "DBA,DBB", "Defined": "Player"}}
	f := NewSAFacts(bound)
	f.Publish()
	cached := &cards.SA{API: "Vote", Params: map[string]string{"VoteCard": "Permanent.nonLand"}}
	VoteOf(cached)
	if n := testing.AllocsPerRun(100, func() {
		_ = VoteOf(bound)
		_ = VoteOf(cached)
	}); n != 0 {
		t.Fatalf("VoteOf allocated %v objects per run; want 0", n)
	}
	if f.Vote == nil || !f.Vote.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the Vote half")
	}
}
