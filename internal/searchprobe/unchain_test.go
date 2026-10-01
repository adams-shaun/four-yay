package searchprobe

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// An Unchain'ed collector records the same frames as a chained one -- board
// facts and digests, identities, events, decision -- without the history
// chain, and historyDigest (so Sample) refuses its history.
func TestUnchainedCaptureKeepsTheFrames(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names := []string{"mono-red-prowess", "mono-blue-tempo"}
	decks := make([][]*cards.Card, len(names))
	for i, n := range names {
		var err error
		if decks[i], err = testutil.LoadRepoDeck(reg, n); err != nil {
			t.Fatal(err)
		}
	}
	e := rules.New(rules.Config{Seed: 30_000_001, Names: names, Decks: decks, Tokens: reg.Tokens})
	e.Advance()
	chained, unchained := NewCollector(0), NewCollector(0)
	unchained.Unchain()
	hc, hu := History{Actor: 0}, History{Actor: 0}
	rngs := BotRandoms(3, len(names))
	board := botpolicy.NewBoard(len(names))
	pos := 0
	for i := 0; i < 300 && !e.G.Over && e.Pending() != nil; i++ {
		fc, err := chained.Capture(e, e.L.Events[pos:])
		if err != nil {
			t.Fatal(err)
		}
		fu, err := unchained.Capture(e, e.L.Events[pos:])
		if err != nil {
			t.Fatal(err)
		}
		if fu.link != nil || !fu.unchained || fc.link == nil {
			t.Fatalf("frame %d: unchained link %v, chained link %v", i, fu.link, fc.link)
		}
		fc.link, fu.unchained = nil, false
		if !reflect.DeepEqual(fc, fu) {
			t.Fatalf("frame %d differs from the chained capture", i)
		}
		hc.Frames, hu.Frames = append(hc.Frames, fc), append(hu.Frames, fu)
		d := e.Pending()
		pos = len(e.L.Events)
		if err := e.Submit(botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])); err != nil {
			t.Fatal(err)
		}
	}
	hu.Frames[0].unchained = true
	if _, err := historyDigest(hu); err == nil {
		t.Fatal("historyDigest accepted an unchained history")
	}
}
