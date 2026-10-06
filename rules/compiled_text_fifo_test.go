package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestEngineCompiledTextEvictsOldestEntryOnly(t *testing.T) {
	// This sequential test owns the cache while it constructs the exact bound.
	compiledTextCache.Lock()
	compiledTextCache.entries = make(map[uintptr][]*compiledTextCacheEntry)
	compiledTextCache.order = nil
	compiledTextCache.n = 0
	compiledTextCache.Unlock()

	const survivorIndex = 1 // Deliberately not the FIFO head.
	var survivorCard *cards.Card
	var survivorText *compiledText
	for i := 0; i < compiledTextCacheLimit; i++ {
		c := card(t, fmt.Sprintf("Name:FIFO %d\nTypes:Creature Test\nPT:1/1\nOracle:x\n", i))
		e := New(Config{Decks: [][]*cards.Card{{c}}})
		if i == survivorIndex {
			survivorCard, survivorText = c, e.compiledText
		}
	}
	if survivorCard == nil || survivorText == nil {
		t.Fatal("cache fill did not retain the selected non-oldest entry")
	}

	overflowCard := card(t, "Name:FIFO overflow\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
	overflow := New(Config{Decks: [][]*cards.Card{{overflowCard}}})
	if overflow.compiledText == survivorText {
		t.Fatal("different configurations unexpectedly share compiled text")
	}
	repeated := New(Config{Decks: [][]*cards.Card{{survivorCard}}})
	if repeated.compiledText != survivorText {
		t.Fatal("one overflow evicted a non-oldest live compiled-text entry")
	}

	compiledTextCache.Lock()
	defer compiledTextCache.Unlock()
	total := 0
	for _, entries := range compiledTextCache.entries {
		total += len(entries)
	}
	if compiledTextCache.n != compiledTextCacheLimit || total != compiledTextCache.n {
		t.Fatalf("after one overflow: count %d, entries %d (limit %d)", compiledTextCache.n, total, compiledTextCacheLimit)
	}
}
