package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
)

// TestMustBlockKeywordIsRegistered pins the coverage half of the fix: the
// canonical head the printed CR 509.1a sentence is rewritten to
// (cards/hiddenkeyword.go "MustBlock") must itself be in the engine's
// supported set, or the coverage walk still reports every printed carrier
// as unsupported -- the ticket's headline symptom. The engine genuinely
// reads the head through parseHiddenKeyword/hasMustBeBlockedKeyword (proved
// by TestRealCorpusMustBeBlockedKeywordIsRead), so the registration is
// honest.
//
// The registry assertion is the exact gate a reviewer broke on:
// reg.Unsupported(raphael, effects.Supported()) must be empty.
func TestMustBlockKeywordIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["kw:MustBlock"] {
		t.Fatal(`effects.Supported() is missing "kw:MustBlock"; the printed sentence is renamed but still unsupported`)
	}
	reg := searchTestRegistry(t)
	raph := searchCorpusCard(t, reg, "Raphael, Ninja Destroyer")
	if d := raph.Link(); len(d) != 0 {
		t.Fatalf("link %s: %v", raph.Faces[0].Name, d)
	}
	// Precondition: the face really carries the canonical head, so the
	// registry assertion below is about MustBlock and not about a card that
	// parses to some other primitive.
	found := false
	for _, f := range raph.Faces {
		for _, k := range f.Keywords {
			if strings.EqualFold(cards.KeywordHead(k), "MustBlock") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("precondition: Raphael carries no canonical MustBlock head; keywords=%v",
			raph.Faces[0].Keywords)
	}
	if m := reg.Unsupported(raph, effects.Supported()); len(m) != 0 {
		t.Fatalf("Raphael still unsupported after registering kw:MustBlock: %v", m)
	}
}

// TestMustBlockCensusCarriersAreNotBlockedByTheKeyword is the census half of
// the ticket ("a census test or count names the other affected cards"): no
// printed carrier of the canonical MustBlock head may be unsupported
// BECAUSE of that keyword primitive. A carrier left with kw:MustBlock (or
// the old phantom sentence primitive) in its unsupported list would mean the
// canonicalisation moved the phantom primitive without registering the real
// one -- the reviewer's MAJOR, caught per card. A carrier may still be
// unsupported for an unrelated primitive (Gorm the Great also needs the
// two-or-more-creatures MinMaxBlocker shape), which is not this ticket's.
func TestMustBlockCensusCarriersAreNotBlockedByTheKeyword(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	supported := effects.Supported()
	var carriers, keywordBlocked []string
	for _, c := range reg.Cards {
		has := false
		for _, f := range c.Faces {
			for _, k := range f.Keywords {
				if strings.EqualFold(cards.KeywordHead(k), "MustBlock") {
					has = true
				}
			}
		}
		if !has {
			continue
		}
		name := c.Faces[0].Name
		carriers = append(carriers, name)
		for _, m := range reg.Unsupported(c, supported) {
			if m == "kw:MustBlock" ||
				strings.EqualFold(m, "kw:CARDNAME must be blocked if able.") {
				keywordBlocked = append(keywordBlocked, name+": "+m)
			}
		}
	}
	// Precondition: the census found the translated carriers, so a green
	// result is not a vacuous scan of an empty registry.
	if len(carriers) == 0 {
		t.Fatal("precondition: no canonical MustBlock carrier found in the corpus registry")
	}
	if len(keywordBlocked) != 0 {
		t.Fatalf("MustBlock carriers still blocked by the keyword primitive (%d of %d): %v",
			len(keywordBlocked), len(carriers), keywordBlocked)
	}
}
