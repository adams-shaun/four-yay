package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// corpusFull is retained for callers and scripts but is now inert: the
// subset loader is retired (S4 of the pointer-free corpus design made the
// imaged, lazy registry the one corpus), so both a deck-seated run and
// -corpus-full get the shared full registry. S8 deletes this flag.
var corpusFull = flag.Bool("corpus-full", false, "INERT since S4: the whole corpus is always the shared (imaged, lazy) registry, so a deck-seated run and this flag open the same one; retained for callers, deleted in S8 (pointer-free corpus design)")

// openCorpusForDecks opens dir's corpus for a run that seats only decks.
// cards.OpenCorpusFor used to decode just the decks' cards plus every token
// when a game could not then play differently; S4 made it a wrapper over
// SharedCorpus, so this now always returns the shared full registry and
// -corpus-full changes nothing. The names are still gathered so the call
// shape is unchanged until S8 deletes both. log, when non-nil, gets one line
// saying which registry the run got.
func openCorpusForDecks(dir string, decks []deck.File, log io.Writer) (*cards.Registry, error) {
	if *corpusFull {
		return testutil.OpenCorpusRegistry(dir)
	}
	var names []string
	for _, f := range decks {
		names = append(names, f.CardNames()...)
	}
	reg, err := cards.OpenCorpusFor(dir, names)
	if err != nil || log == nil {
		return reg, err
	}
	if reg.IsSubset() {
		fmt.Fprintf(log, "corpus: %d-card subset (+%d tokens) for %d decks\n", reg.Len(), len(reg.Tokens), len(decks))
	} else {
		fmt.Fprintf(log, "corpus: full, %d cards (the shared registry; the retired subset loader no longer answers here)\n", reg.Len())
	}
	return reg, nil
}
